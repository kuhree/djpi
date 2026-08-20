package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"git.kuhree.com/kuhree.com/djpi/internal/destination"
	"git.kuhree.com/kuhree.com/djpi/internal/journal"
	"git.kuhree.com/kuhree.com/djpi/internal/pathguard"
	"git.kuhree.com/kuhree.com/djpi/internal/source"
)

type Engine struct {
	Journal      *journal.Store
	Destinations []destination.Destination
	Now          func() time.Time
}

type Result struct {
	SourceID string
	Items    int
	Verified int
	Skipped  int
	Failed   int
	Failures []string
}

func (e Engine) Ingest(ctx context.Context, camera, sourceRoot string, observation source.Observation) (Result, error) {
	result := Result{Items: len(observation.Items)}
	if e.Journal == nil {
		return result, errors.New("ingest journal is required")
	}
	if err := pathguard.RefuseInside(sourceRoot, e.Journal.Path()); err != nil {
		return result, fmt.Errorf("unsafe journal configuration: %w", err)
	}
	if camera == "" {
		return result, errors.New("camera identity is required")
	}
	if !observation.Complete || observation.EvidenceID == "" {
		return result, errors.New("complete source metadata evidence is required")
	}
	if len(e.Destinations) == 0 {
		return result, errors.New("at least one destination is required")
	}
	destinationNames := make(map[string]struct{}, len(e.Destinations))
	for _, configured := range e.Destinations {
		if configured == nil || configured.Name() == "" {
			return result, errors.New("every destination must have a name")
		}
		if _, duplicate := destinationNames[configured.Name()]; duplicate {
			return result, fmt.Errorf("duplicate destination name %q", configured.Name())
		}
		destinationNames[configured.Name()] = struct{}{}
	}
	result.SourceID = sourceRecordID(camera, observation.EvidenceID)
	if err := e.initializeSource(result.SourceID, camera, observation); err != nil {
		return result, err
	}
	state, err := e.Journal.Load()
	if err != nil {
		return result, err
	}
	record := state.Sources[result.SourceID]

	destinations := append([]destination.Destination(nil), e.Destinations...)
	sort.Slice(destinations, func(i, j int) bool { return destinations[i].Name() < destinations[j].Name() })
	for _, item := range observation.Items {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		itemRecord := record.Items[item.Path]
		if itemRecord.ContentDigest != "" {
			digest, hashErr := hashObservedItem(ctx, sourceRoot, item)
			if hashErr != nil {
				result.addFailure(item.Path, "source", hashErr)
				continue
			}
			if digest != itemRecord.ContentDigest {
				result.addFailure(item.Path, "source", errors.New("content changed without metadata evidence changing"))
				continue
			}
		}
		for _, target := range destinations {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			itemRecord = record.Items[item.Path]
			previous, hadPrevious := itemRecord.Deliveries[target.Name()]
			if hadPrevious && previous.State == "verified" && previous.ContentDigest == itemRecord.ContentDigest {
				verified, verifyErr := target.Verify(ctx, destination.Evidence{SizeBytes: previous.SizeBytes, ContentDigest: previous.ContentDigest, VerificationDigest: previous.VerificationDigest, Location: previous.Location})
				if verifyErr == nil && verified {
					result.Skipped++
					continue
				}
				if verifyErr != nil {
					result.addFailure(item.Path, target.Name(), verifyErr)
					continue
				}
			}

			evidence, deliveryErr := target.Deliver(ctx, sourceRoot, camera, item)
			if deliveryErr != nil {
				result.addFailure(item.Path, target.Name(), deliveryErr)
				if err := e.recordFailure(result.SourceID, item.Path, target.Name(), deliveryErr); err != nil {
					return result, err
				}
				continue
			}
			if evidenceErr := validateEvidence(item, evidence); evidenceErr != nil {
				result.addFailure(item.Path, target.Name(), evidenceErr)
				if err := e.recordFailure(result.SourceID, item.Path, target.Name(), evidenceErr); err != nil {
					return result, err
				}
				continue
			}
			if itemRecord.ContentDigest != "" && itemRecord.ContentDigest != evidence.ContentDigest {
				collision := errors.New("destination evidence differs from prior source content identity")
				result.addFailure(item.Path, target.Name(), collision)
				_ = e.recordFailure(result.SourceID, item.Path, target.Name(), collision)
				continue
			}
			if err := e.recordVerified(result.SourceID, item.Path, target.Name(), evidence); err != nil {
				return result, err
			}
			itemRecord.ContentDigest = evidence.ContentDigest
			itemRecord.Deliveries[target.Name()] = deliveryRecord(evidence, e.now())
			record.Items[item.Path] = itemRecord
			result.Verified++
		}
	}
	if result.Failed != 0 {
		return result, fmt.Errorf("%d delivery operation(s) failed", result.Failed)
	}
	return result, nil
}

func validateEvidence(item source.ObservedItem, evidence destination.Evidence) error {
	if evidence.SizeBytes != item.SizeBytes {
		return errors.New("destination evidence size differs from inventoried source")
	}
	digest, ok := strings.CutPrefix(evidence.ContentDigest, "sha256:")
	decoded, decodeErr := hex.DecodeString(digest)
	if !ok || decodeErr != nil || len(decoded) != sha256.Size {
		return errors.New("destination did not return a full SHA-256 content digest")
	}
	if evidence.VerificationDigest == "" {
		return errors.New("destination did not return an independent verification digest")
	}
	if evidence.Location == "" {
		return errors.New("destination did not return a durable location")
	}
	return nil
}

func (e Engine) initializeSource(sourceID, camera string, observation source.Observation) error {
	return e.Journal.Update(func(state *journal.State) error {
		record, exists := state.Sources[sourceID]
		if exists && record.MetadataEvidenceID != observation.EvidenceID {
			return errors.New("journal source identity collision")
		}
		if record.Items == nil {
			record.Items = make(map[string]journal.ItemRecord)
		}
		for _, item := range observation.Items {
			existing, ok := record.Items[item.Path]
			if ok && (existing.SizeBytes != item.SizeBytes || existing.ModifiedUnixNanos != item.ModifiedUnixNanos) {
				return fmt.Errorf("journal item metadata collision: %s", item.Path)
			}
			if !ok {
				record.Items[item.Path] = journal.ItemRecord{SizeBytes: item.SizeBytes, ModifiedUnixNanos: item.ModifiedUnixNanos, Deliveries: make(map[string]journal.DeliveryRecord)}
			}
		}
		if len(record.Items) != len(observation.Items) {
			return errors.New("journal item set differs for identical metadata evidence")
		}
		record.Camera, record.MetadataEvidenceID = camera, observation.EvidenceID
		state.Sources[sourceID] = record
		return nil
	})
}

func (e Engine) recordVerified(sourceID, itemPath, destinationName string, evidence destination.Evidence) error {
	return e.Journal.Update(func(state *journal.State) error {
		record := state.Sources[sourceID]
		item := record.Items[itemPath]
		if item.Deliveries == nil {
			item.Deliveries = make(map[string]journal.DeliveryRecord)
		}
		if item.ContentDigest != "" && item.ContentDigest != evidence.ContentDigest {
			return errors.New("source content digest changed during ingest")
		}
		item.ContentDigest = evidence.ContentDigest
		item.Deliveries[destinationName] = deliveryRecord(evidence, e.now())
		record.Items[itemPath] = item
		state.Sources[sourceID] = record
		return nil
	})
}

func deliveryRecord(evidence destination.Evidence, verifiedAt time.Time) journal.DeliveryRecord {
	return journal.DeliveryRecord{
		State:              "verified",
		SizeBytes:          evidence.SizeBytes,
		ContentDigest:      evidence.ContentDigest,
		VerificationDigest: evidence.VerificationDigest,
		Location:           evidence.Location,
		VerifiedAt:         verifiedAt,
	}
}

func (e Engine) recordFailure(sourceID, itemPath, destinationName string, failure error) error {
	return e.Journal.Update(func(state *journal.State) error {
		record := state.Sources[sourceID]
		item := record.Items[itemPath]
		if item.Deliveries == nil {
			item.Deliveries = make(map[string]journal.DeliveryRecord)
		}
		item.Deliveries[destinationName] = journal.DeliveryRecord{State: "failed", Error: failure.Error()}
		record.Items[itemPath] = item
		state.Sources[sourceID] = record
		return nil
	})
}

func (e Engine) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *Result) addFailure(item, target string, err error) {
	r.Failed++
	r.Failures = append(r.Failures, fmt.Sprintf("%s at %s: %v", item, target, err))
}

func sourceRecordID(camera, evidenceID string) string {
	digest := sha256.Sum256([]byte(camera + "\x00" + evidenceID))
	return fmt.Sprintf("source-sha256:%x", digest[:])
}

func hashObservedItem(ctx context.Context, root string, item source.ObservedItem) (string, error) {
	relative := filepath.Clean(filepath.FromSlash(item.Path))
	if item.Path == "" || strings.ContainsRune(item.Path, '\x00') || filepath.IsAbs(item.Path) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe source item path %q", item.Path)
	}
	path := filepath.Join(root, relative)
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() != item.SizeBytes || info.ModTime().UnixNano() != item.ModifiedUnixNanos {
		return "", errors.New("source item changed since inventory")
	}
	hash := sha256.New()
	buffer := make([]byte, 1024*1024)
	var read int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			written, writeErr := hash.Write(buffer[:count])
			read += int64(written)
			if writeErr != nil {
				return "", writeErr
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	if read != item.SizeBytes {
		return "", errors.New("source size changed while hashing")
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil)), nil
}
