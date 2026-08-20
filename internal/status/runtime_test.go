package status

import (
	"sync"
	"testing"
	"time"

	"git.kuhree.com/kuhree.com/djpi/internal/ingest"
	"git.kuhree.com/kuhree.com/djpi/internal/station"
)

func TestRuntimeTrackerMapsLifecycleAndCopiesRetryDeadline(t *testing.T) {
	started := time.Unix(1, 0)
	updated := time.Unix(2, 0)
	retry := time.Unix(12, 0)
	tracker := NewRuntimeTracker(started)
	if snapshot := tracker.Snapshot(); snapshot.State != "idle" || !snapshot.StartedAt.Equal(started) {
		t.Fatalf("initial snapshot = %#v", snapshot)
	}
	if err := tracker.Emit(station.Event{Time: updated, Type: "ingest_failed", Camera: "DJI Osmo Pocket 3", Files: 9, RetryAt: &retry, Message: "offline"}); err != nil {
		t.Fatal(err)
	}
	snapshot := tracker.Snapshot()
	if snapshot.State != "retrying" || snapshot.LastEvent != "ingest_failed" || snapshot.Camera != "DJI Osmo Pocket 3" || snapshot.RetryAt == nil || !snapshot.RetryAt.Equal(retry) {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	retry = time.Unix(99, 0)
	if snapshot.RetryAt.Equal(retry) {
		t.Fatal("tracker retained caller-owned retry pointer")
	}
}

func TestRuntimeTrackerAddsProgressWithoutReplacingLifecycleEvidence(t *testing.T) {
	tracker := NewRuntimeTracker(time.Unix(1, 0))
	if err := tracker.Emit(station.Event{Time: time.Unix(2, 0), Type: "ingest_started", EvidenceID: "metadata:test", Files: 2}); err != nil {
		t.Fatal(err)
	}
	tracker.TrackProgress(ingest.Progress{Camera: "DJI Osmo Action 6", ItemPath: "DCIM/clip.MP4", ItemIndex: 1, ItemTotal: 2, Destination: "archive", Phase: "delivering"})
	snapshot := tracker.Snapshot()
	if snapshot.State != "ingesting" || snapshot.LastEvent != "ingest_started" || snapshot.EvidenceID != "metadata:test" || snapshot.ItemPath != "DCIM/clip.MP4" || snapshot.ItemIndex != 1 || snapshot.ItemTotal != 2 || snapshot.Destination != "archive" || snapshot.Phase != "delivering" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestRuntimeTrackerSupportsConcurrentSnapshots(t *testing.T) {
	tracker := NewRuntimeTracker(time.Unix(1, 0))
	var group sync.WaitGroup
	for index := range 20 {
		group.Add(2)
		go func(value int) {
			defer group.Done()
			_ = tracker.Emit(station.Event{Time: time.Unix(int64(value+2), 0), Type: "ingest_started"})
		}(index)
		go func() {
			defer group.Done()
			_ = tracker.Snapshot()
		}()
	}
	group.Wait()
}
