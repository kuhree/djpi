// Package audit independently re-verifies durable destination evidence without
// requiring or modifying camera storage.
package audit

import (
	"context"
	"fmt"
	"sort"
	"time"

	"git.kuhree.com/kuhree.com/djpi/internal/destination"
	"git.kuhree.com/kuhree.com/djpi/internal/journal"
)

type Report struct {
	CheckedAt time.Time `json:"checked_at"`
	Expected  int       `json:"expected"`
	Verified  int       `json:"verified"`
	Pending   int       `json:"pending"`
	Failed    int       `json:"failed"`
	Problems  []Problem `json:"problems,omitempty"`
}

type Problem struct {
	SourceID    string `json:"source_id"`
	Camera      string `json:"camera"`
	ItemPath    string `json:"item_path"`
	Destination string `json:"destination"`
	State       string `json:"state"`
	Message     string `json:"message"`
}

func (r Report) Clean() bool { return r.Pending == 0 && r.Failed == 0 }

// Run checks every item against every currently configured destination. It is
// deliberately read-only: the journal and all Source data remain untouched.
func Run(ctx context.Context, state journal.State, destinations []destination.Destination, timeout time.Duration, at time.Time) Report {
	report := Report{CheckedAt: at.UTC()}
	targets := append([]destination.Destination(nil), destinations...)
	sort.Slice(targets, func(i, j int) bool { return targets[i].Name() < targets[j].Name() })
	sourceIDs := sortedKeys(state.Sources)
	for _, sourceID := range sourceIDs {
		sourceRecord := state.Sources[sourceID]
		for _, itemPath := range sortedKeys(sourceRecord.Items) {
			itemRecord := sourceRecord.Items[itemPath]
			for _, target := range targets {
				report.Expected++
				delivery, ok := itemRecord.Deliveries[target.Name()]
				if !ok || delivery.State != "verified" {
					report.Pending++
					message := "no verified delivery evidence"
					if ok && delivery.Error != "" {
						message = delivery.Error
					}
					report.Problems = append(report.Problems, problem(sourceID, sourceRecord.Camera, itemPath, target.Name(), "pending", message))
					continue
				}
				if delivery.ContentDigest == "" || delivery.ContentDigest != itemRecord.ContentDigest || delivery.VerificationDigest == "" || delivery.Location == "" || delivery.SizeBytes != itemRecord.SizeBytes {
					report.Failed++
					report.Problems = append(report.Problems, problem(sourceID, sourceRecord.Camera, itemPath, target.Name(), "invalid-evidence", "journal delivery evidence is incomplete or inconsistent"))
					continue
				}
				operationContext, cancel := context.WithTimeout(ctx, timeout)
				verified, err := target.Verify(operationContext, destination.Evidence{
					SizeBytes: delivery.SizeBytes, ContentDigest: delivery.ContentDigest,
					VerificationDigest: delivery.VerificationDigest, Location: delivery.Location,
				})
				cancel()
				if err != nil {
					report.Failed++
					report.Problems = append(report.Problems, problem(sourceID, sourceRecord.Camera, itemPath, target.Name(), "error", err.Error()))
					continue
				}
				if !verified {
					report.Failed++
					report.Problems = append(report.Problems, problem(sourceID, sourceRecord.Camera, itemPath, target.Name(), "mismatch", "destination bytes no longer match journaled evidence"))
					continue
				}
				report.Verified++
			}
		}
	}
	return report
}

func problem(sourceID, camera, itemPath, destinationName, state, message string) Problem {
	return Problem{SourceID: sourceID, Camera: camera, ItemPath: itemPath, Destination: destinationName, State: state, Message: message}
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (r Report) Summary() string {
	return fmt.Sprintf("expected=%d verified=%d pending=%d failed=%d", r.Expected, r.Verified, r.Pending, r.Failed)
}
