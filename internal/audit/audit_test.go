package audit

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"git.kuhree.com/kuhree/djpi/internal/destination"
	"git.kuhree.com/kuhree/djpi/internal/journal"
	"git.kuhree.com/kuhree/djpi/internal/source"
)

func TestRunVerifiesAndReportsConfiguredDeliveryProblems(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	state := journal.EmptyState()
	state.Sources["source-b"] = journal.SourceRecord{Camera: "Pocket 3", Items: map[string]journal.ItemRecord{
		"DCIM/two.MP4": {SizeBytes: 5, ContentDigest: digest, Deliveries: map[string]journal.DeliveryRecord{
			"archive": {State: "verified", SizeBytes: 5, ContentDigest: digest, VerificationDigest: digest, Location: "two"},
		}},
		"DCIM/one.JPG": {SizeBytes: 5, ContentDigest: digest, Deliveries: map[string]journal.DeliveryRecord{}},
	}}
	archive := fakeDestination{name: "archive", results: map[string]bool{"two": true}}
	replica := fakeDestination{name: "replica", results: map[string]bool{}}
	report := Run(context.Background(), state, []destination.Destination{replica, archive}, time.Second, time.Unix(9, 0))
	if report.Expected != 4 || report.Verified != 1 || report.Pending != 3 || report.Failed != 0 || report.Clean() {
		t.Fatalf("report = %#v", report)
	}
	if len(report.Problems) != 3 || report.Problems[0].ItemPath != "DCIM/one.JPG" || report.Problems[0].Destination != "archive" || report.CheckedAt.Location() != time.UTC {
		t.Fatalf("non-deterministic report = %#v", report)
	}
}

func TestRunFailsClosedForMismatchErrorAndInvalidEvidence(t *testing.T) {
	digest := "sha256:" + strings.Repeat("b", 64)
	state := journal.EmptyState()
	state.Sources["source"] = journal.SourceRecord{Camera: "Action 6", Items: map[string]journal.ItemRecord{
		"bad":      {SizeBytes: 1, ContentDigest: digest, Deliveries: map[string]journal.DeliveryRecord{"archive": {State: "verified"}}},
		"error":    {SizeBytes: 1, ContentDigest: digest, Deliveries: map[string]journal.DeliveryRecord{"archive": verifiedRecord(1, digest, "error")}},
		"mismatch": {SizeBytes: 1, ContentDigest: digest, Deliveries: map[string]journal.DeliveryRecord{"archive": verifiedRecord(1, digest, "mismatch")}},
	}}
	target := fakeDestination{name: "archive", results: map[string]bool{"mismatch": false}, errors: map[string]error{"error": errors.New("offline")}}
	report := Run(context.Background(), state, []destination.Destination{target}, time.Second, time.Now())
	if report.Failed != 3 || report.Verified != 0 || report.Pending != 0 || len(report.Problems) != 3 {
		t.Fatalf("report = %#v", report)
	}
	wantStates := []string{"invalid-evidence", "error", "mismatch"}
	for index, want := range wantStates {
		if report.Problems[index].State != want {
			t.Fatalf("problem %d = %#v", index, report.Problems[index])
		}
	}
}

func TestRunBoundsEachVerification(t *testing.T) {
	digest := "sha256:" + strings.Repeat("c", 64)
	state := journal.EmptyState()
	state.Sources["source"] = journal.SourceRecord{Items: map[string]journal.ItemRecord{
		"video": {SizeBytes: 1, ContentDigest: digest, Deliveries: map[string]journal.DeliveryRecord{"archive": verifiedRecord(1, digest, "video")}},
	}}
	report := Run(context.Background(), state, []destination.Destination{blockingDestination{}}, 5*time.Millisecond, time.Now())
	if report.Failed != 1 || !strings.Contains(report.Problems[0].Message, context.DeadlineExceeded.Error()) {
		t.Fatalf("report = %#v", report)
	}
}

func TestWriteHumanAndJSON(t *testing.T) {
	report := Report{Expected: 1, Verified: 1}
	var human, machine bytes.Buffer
	if err := Write(&human, report, false); err != nil {
		t.Fatal(err)
	}
	if err := Write(&machine, report, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(human.String(), "expected=1 verified=1 pending=0 failed=0") || !strings.Contains(machine.String(), `"verified": 1`) {
		t.Fatalf("human=%q json=%q", human.String(), machine.String())
	}
}

func verifiedRecord(size int64, digest, location string) journal.DeliveryRecord {
	return journal.DeliveryRecord{State: "verified", SizeBytes: size, ContentDigest: digest, VerificationDigest: digest, Location: location}
}

type fakeDestination struct {
	name    string
	results map[string]bool
	errors  map[string]error
}

func (d fakeDestination) Name() string { return d.name }
func (d fakeDestination) Deliver(context.Context, string, string, source.ObservedItem) (destination.Evidence, error) {
	return destination.Evidence{}, errors.New("unexpected delivery")
}
func (d fakeDestination) Verify(_ context.Context, evidence destination.Evidence) (bool, error) {
	return d.results[evidence.Location], d.errors[evidence.Location]
}

type blockingDestination struct{}

func (blockingDestination) Name() string { return "archive" }
func (blockingDestination) Deliver(context.Context, string, string, source.ObservedItem) (destination.Evidence, error) {
	return destination.Evidence{}, errors.New("unexpected delivery")
}
func (blockingDestination) Verify(ctx context.Context, _ destination.Evidence) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}
