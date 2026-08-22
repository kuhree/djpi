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
	OpenSource   func(string) (*os.File, error)
	// CameraAliases maps a Camera identity to the operator-chosen Destination
	// namespace for that Camera. It only names where Media Items are written;
	// Camera identity in the Journal is unaffected.
	CameraAliases    map[string]string
	OperationTimeout time.Duration
	Progress         func(Progress)
	Now              func() time.Time
}

const defaultOperationTimeout = 6 * time.Hour

type Result struct {
	SourceID string
	Items    int
	Verified int
	Skipped  int
	Failed   int
	Failures []string
}

// Progress is an observational update for an in-flight ingest. It carries no
// authority over delivery, verification, journaling, or cleanup decisions.
type Progress struct {
	SourceID    string `json:"source_id"`
	Camera      string `json:"camera"`
	ItemPath    string `json:"item_path"`
	ItemIndex   int    `json:"item_index"`
	ItemTotal   int    `json:"item_total"`
	Destination string `json:"destination,omitempty"`
	Phase       string `json:"phase"`
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
	if e.OperationTimeout < 0 {
		return result, errors.New("destination operation timeout cannot be negative")
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
	for itemIndex, item := range observation.Items {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		fanout := make([]destination.FanoutDestination, len(destinations))
		allFanout := true
		for index, target := range destinations {
			fanout[index], allFanout = target.(destination.FanoutDestination)
			if !allFanout {
				break
			}
		}
		if allFanout {
			if err := e.ingestFanoutItem(ctx, &result, &record, result.SourceID, camera, sourceRoot, item, itemIndex, len(observation.Items), fanout); err != nil {
				return result, err
			}
			continue
		}
		itemRecord := record.Items[item.Path]
		if itemRecord.ContentDigest != "" {
			e.progress(result.SourceID, camera, item, itemIndex, len(observation.Items), "", "source-check")
			digest, hashErr := hashObservedItem(ctx, sourceRoot, item)
			if hashErr != nil {
				e.progress(result.SourceID, camera, item, itemIndex, len(observation.Items), "", "failed")
				result.addFailure(item.Path, "source", hashErr)
				continue
			}
			if digest != itemRecord.ContentDigest {
				e.progress(result.SourceID, camera, item, itemIndex, len(observation.Items), "", "failed")
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
				e.progress(result.SourceID, camera, item, itemIndex, len(observation.Items), target.Name(), "verifying")
				operationContext, cancel := e.operationContext(ctx)
				verified, verifyErr := target.Verify(operationContext, destination.Evidence{SizeBytes: previous.SizeBytes, ContentDigest: previous.ContentDigest, VerificationDigest: previous.VerificationDigest, Location: previous.Location})
				cancel()
				if verifyErr == nil && verified {
					result.Skipped++
					e.progress(result.SourceID, camera, item, itemIndex, len(observation.Items), target.Name(), "skipped")
					continue
				}
				if verifyErr != nil {
					e.progress(result.SourceID, camera, item, itemIndex, len(observation.Items), target.Name(), "failed")
					result.addFailure(item.Path, target.Name(), verifyErr)
					continue
				}
			}

			e.progress(result.SourceID, camera, item, itemIndex, len(observation.Items), target.Name(), "delivering")
			operationContext, cancel := e.operationContext(ctx)
			evidence, deliveryErr := target.Deliver(operationContext, sourceRoot, e.namespaceFor(camera), item)
			cancel()
			if deliveryErr != nil {
				e.progress(result.SourceID, camera, item, itemIndex, len(observation.Items), target.Name(), "failed")
				result.addFailure(item.Path, target.Name(), deliveryErr)
				if err := e.recordFailure(result.SourceID, item.Path, target.Name(), deliveryErr); err != nil {
					return result, err
				}
				continue
			}
			if evidenceErr := validateEvidence(item, evidence); evidenceErr != nil {
				e.progress(result.SourceID, camera, item, itemIndex, len(observation.Items), target.Name(), "failed")
				result.addFailure(item.Path, target.Name(), evidenceErr)
				if err := e.recordFailure(result.SourceID, item.Path, target.Name(), evidenceErr); err != nil {
					return result, err
				}
				continue
			}
			if itemRecord.ContentDigest != "" && itemRecord.ContentDigest != evidence.ContentDigest {
				collision := errors.New("destination evidence differs from prior source content identity")
				e.progress(result.SourceID, camera, item, itemIndex, len(observation.Items), target.Name(), "failed")
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
			e.progress(result.SourceID, camera, item, itemIndex, len(observation.Items), target.Name(), "verified")
		}
	}
	if result.Failed != 0 {
		return result, fmt.Errorf("%d delivery operation(s) failed", result.Failed)
	}
	return result, nil
}

type activeFanout struct {
	target destination.FanoutDestination
	stage  destination.StagedDelivery
	failed bool
}

func (e Engine) ingestFanoutItem(ctx context.Context, result *Result, record *journal.SourceRecord, sourceID, camera, sourceRoot string, item source.ObservedItem, itemIndex, itemTotal int, targets []destination.FanoutDestination) error {
	itemRecord := record.Items[item.Path]
	if itemRecord.ContentDigest != "" {
		e.progress(sourceID, camera, item, itemIndex, itemTotal, "", "source-check")
	}
	pending := make([]destination.FanoutDestination, 0, len(targets))
	verifiedTargets := make([]destination.FanoutDestination, 0, len(targets))
	markSkipped := func() {
		for _, target := range verifiedTargets {
			result.Skipped++
			e.progress(sourceID, camera, item, itemIndex, itemTotal, target.Name(), "skipped")
		}
	}
	for _, target := range targets {
		previous, hadPrevious := itemRecord.Deliveries[target.Name()]
		if hadPrevious && previous.State == "verified" && previous.ContentDigest == itemRecord.ContentDigest {
			e.progress(sourceID, camera, item, itemIndex, itemTotal, target.Name(), "verifying")
			operationContext, cancel := e.operationContext(ctx)
			verified, err := target.Verify(operationContext, destination.Evidence{SizeBytes: previous.SizeBytes, ContentDigest: previous.ContentDigest, VerificationDigest: previous.VerificationDigest, Location: previous.Location})
			cancel()
			if err == nil && verified {
				verifiedTargets = append(verifiedTargets, target)
				continue
			}
			if err != nil {
				result.addFailure(item.Path, target.Name(), err)
				e.progress(sourceID, camera, item, itemIndex, itemTotal, target.Name(), "failed")
				continue
			}
		}
		pending = append(pending, target)
	}
	if len(pending) == 0 {
		if itemRecord.ContentDigest != "" {
			digest, err := e.hashObservedItem(ctx, sourceRoot, item)
			if err != nil || digest != itemRecord.ContentDigest {
				if err == nil {
					err = errors.New("content changed without metadata evidence changing")
				}
				result.addFailure(item.Path, "source", err)
				e.progress(sourceID, camera, item, itemIndex, itemTotal, "", "failed")
				return nil
			}
		}
		markSkipped()
		return nil
	}

	active := make([]*activeFanout, 0, len(pending))
	for _, target := range pending {
		e.progress(sourceID, camera, item, itemIndex, itemTotal, target.Name(), "delivering")
		operationContext, cancel := e.operationContext(ctx)
		stage, err := target.Stage(operationContext, sourceRoot, e.namespaceFor(camera), item)
		cancel()
		if err != nil {
			result.addFailure(item.Path, target.Name(), err)
			e.progress(sourceID, camera, item, itemIndex, itemTotal, target.Name(), "failed")
			if recordErr := e.recordFailure(sourceID, item.Path, target.Name(), err); recordErr != nil {
				return recordErr
			}
			continue
		}
		active = append(active, &activeFanout{target: target, stage: stage})
	}
	defer func() {
		for _, delivery := range active {
			_ = delivery.stage.Abort()
		}
	}()
	if len(active) == 0 {
		if len(verifiedTargets) != 0 {
			digest, err := e.hashObservedItem(ctx, sourceRoot, item)
			if err != nil || digest != itemRecord.ContentDigest {
				if err == nil {
					err = errors.New("content changed without metadata evidence changing")
				}
				result.addFailure(item.Path, "source", err)
				e.progress(sourceID, camera, item, itemIndex, itemTotal, "", "failed")
				return nil
			}
			markSkipped()
		}
		return nil
	}

	file, err := e.openSource(sourceRoot, item)
	if err != nil {
		return e.failActiveFanout(result, sourceID, item, itemIndex, itemTotal, camera, active, err)
	}
	hash := sha256.New()
	buffer := make([]byte, 1024*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return err
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			chunk := buffer[:count]
			_, _ = hash.Write(chunk)
			total += int64(count)
			type stageWriteResult struct {
				delivery *activeFanout
				written  int
				err      error
			}
			writeResults := make(chan stageWriteResult, len(active))
			writes := 0
			for _, delivery := range active {
				if delivery.failed {
					continue
				}
				writes++
				go func(delivery *activeFanout) {
					written, writeErr := delivery.stage.Write(chunk)
					writeResults <- stageWriteResult{delivery: delivery, written: written, err: writeErr}
				}(delivery)
			}
			for range writes {
				writeResult := <-writeResults
				delivery, written, writeErr := writeResult.delivery, writeResult.written, writeResult.err
				if writeErr != nil || written != len(chunk) {
					if writeErr == nil {
						writeErr = io.ErrShortWrite
					}
					delivery.failed = true
					_ = delivery.stage.Abort()
					result.addFailure(item.Path, delivery.target.Name(), writeErr)
					e.progress(sourceID, camera, item, itemIndex, itemTotal, delivery.target.Name(), "failed")
					if err := e.recordFailure(sourceID, item.Path, delivery.target.Name(), writeErr); err != nil {
						_ = file.Close()
						return err
					}
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			_ = file.Close()
			return e.failActiveFanout(result, sourceID, item, itemIndex, itemTotal, camera, active, readErr)
		}
	}
	if err := file.Close(); err != nil {
		return e.failActiveFanout(result, sourceID, item, itemIndex, itemTotal, camera, active, err)
	}
	if total != item.SizeBytes {
		return e.failActiveFanout(result, sourceID, item, itemIndex, itemTotal, camera, active, fmt.Errorf("source size changed while delivering %s", item.Path))
	}
	digest := fmt.Sprintf("sha256:%x", hash.Sum(nil))
	if itemRecord.ContentDigest != "" && digest != itemRecord.ContentDigest {
		return e.failActiveFanout(result, sourceID, item, itemIndex, itemTotal, camera, active, errors.New("content changed without metadata evidence changing"))
	}
	stableDigest, stableErr := e.hashObservedItem(ctx, sourceRoot, item)
	if stableErr != nil || stableDigest != digest {
		if stableErr == nil {
			stableErr = fmt.Errorf("source content changed while delivering %s", item.Path)
		}
		return e.failActiveFanout(result, sourceID, item, itemIndex, itemTotal, camera, active, stableErr)
	}
	markSkipped()

	for _, delivery := range active {
		if delivery.failed {
			continue
		}
		operationContext, cancel := e.operationContext(ctx)
		evidence, err := delivery.stage.Commit(operationContext, digest)
		cancel()
		if err == nil {
			err = validateEvidence(item, evidence)
		}
		if err == nil && evidence.ContentDigest != digest {
			err = errors.New("destination evidence differs from fanned-out content identity")
		}
		if err != nil {
			result.addFailure(item.Path, delivery.target.Name(), err)
			e.progress(sourceID, camera, item, itemIndex, itemTotal, delivery.target.Name(), "failed")
			if recordErr := e.recordFailure(sourceID, item.Path, delivery.target.Name(), err); recordErr != nil {
				return recordErr
			}
			continue
		}
		if err := e.recordVerified(sourceID, item.Path, delivery.target.Name(), evidence); err != nil {
			return err
		}
		itemRecord.ContentDigest = evidence.ContentDigest
		itemRecord.Deliveries[delivery.target.Name()] = deliveryRecord(evidence, e.now())
		record.Items[item.Path] = itemRecord
		result.Verified++
		e.progress(sourceID, camera, item, itemIndex, itemTotal, delivery.target.Name(), "verified")
	}
	return nil
}

func (e Engine) failActiveFanout(result *Result, sourceID string, item source.ObservedItem, itemIndex, itemTotal int, camera string, active []*activeFanout, failure error) error {
	for _, delivery := range active {
		if delivery.failed {
			continue
		}
		delivery.failed = true
		_ = delivery.stage.Abort()
		result.addFailure(item.Path, delivery.target.Name(), failure)
		e.progress(sourceID, camera, item, itemIndex, itemTotal, delivery.target.Name(), "failed")
		if err := e.recordFailure(sourceID, item.Path, delivery.target.Name(), failure); err != nil {
			return err
		}
	}
	return nil
}

func (e Engine) progress(sourceID, camera string, item source.ObservedItem, index, total int, destinationName, phase string) {
	if e.Progress == nil {
		return
	}
	e.Progress(Progress{
		SourceID: sourceID, Camera: camera, ItemPath: item.Path, ItemIndex: index + 1,
		ItemTotal: total, Destination: destinationName, Phase: phase,
	})
}

func (e Engine) operationContext(parent context.Context) (context.Context, context.CancelFunc) {
	timeout := e.OperationTimeout
	if timeout == 0 {
		timeout = defaultOperationTimeout
	}
	return context.WithTimeout(parent, timeout)
}

// namespaceFor resolves the Destination namespace for a Camera. An operator
// alias wins; otherwise a hashed Camera identity is shortened so a Destination
// stays browsable. The full identity remains in the Journal either way, and no
// Camera serial is exposed.
func (e Engine) namespaceFor(camera string) string {
	if alias := strings.TrimSpace(e.CameraAliases[camera]); alias != "" {
		return alias
	}
	digest, hashed := strings.CutPrefix(camera, "camera-sha256:")
	if hashed && len(digest) >= 8 {
		return "camera-" + digest[:8]
	}
	return camera
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

func (e Engine) openSource(root string, item source.ObservedItem) (*os.File, error) {
	relative := filepath.Clean(filepath.FromSlash(item.Path))
	if item.Path == "" || strings.ContainsRune(item.Path, '\x00') || filepath.IsAbs(item.Path) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("unsafe source item path %q", item.Path)
	}
	path := filepath.Join(root, relative)
	open := e.OpenSource
	if open == nil {
		open = os.Open
	}
	file, err := open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != item.SizeBytes || info.ModTime().UnixNano() != item.ModifiedUnixNanos {
		_ = file.Close()
		return nil, errors.New("source item changed since inventory")
	}
	return file, nil
}

func (e Engine) hashObservedItem(ctx context.Context, root string, item source.ObservedItem) (string, error) {
	file, err := e.openSource(root, item)
	if err != nil {
		return "", err
	}
	defer file.Close()
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

func hashObservedItem(ctx context.Context, root string, item source.ObservedItem) (string, error) {
	return (Engine{}).hashObservedItem(ctx, root, item)
}
