package status

import (
	"sync"
	"time"

	"git.kuhree.com/kuhree.com/djpi/internal/ingest"
	"git.kuhree.com/kuhree.com/djpi/internal/station"
)

// RuntimeStatus is the daemon's current in-memory lifecycle state. Durable
// delivery evidence remains in Journal; this view is for live operations.
type RuntimeStatus struct {
	State       string     `json:"state"`
	StartedAt   time.Time  `json:"started_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastEvent   string     `json:"last_event,omitempty"`
	Camera      string     `json:"camera,omitempty"`
	SourcePath  string     `json:"source_path,omitempty"`
	EvidenceID  string     `json:"metadata_evidence_id,omitempty"`
	Files       int        `json:"files,omitempty"`
	Bytes       int64      `json:"bytes,omitempty"`
	Verified    int        `json:"verified,omitempty"`
	Skipped     int        `json:"skipped,omitempty"`
	Failed      int        `json:"failed,omitempty"`
	RetryAt     *time.Time `json:"retry_at,omitempty"`
	Message     string     `json:"message,omitempty"`
	ItemPath    string     `json:"item_path,omitempty"`
	ItemIndex   int        `json:"item_index,omitempty"`
	ItemTotal   int        `json:"item_total,omitempty"`
	Destination string     `json:"destination,omitempty"`
	Phase       string     `json:"phase,omitempty"`
}

// TrackProgress updates only the live operation detail. The last lifecycle
// event and all durable journal evidence remain unchanged.
func (tracker *RuntimeTracker) TrackProgress(progress ingest.Progress) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.state.State = "ingesting"
	tracker.state.Camera = progress.Camera
	tracker.state.ItemPath = progress.ItemPath
	tracker.state.ItemIndex = progress.ItemIndex
	tracker.state.ItemTotal = progress.ItemTotal
	tracker.state.Destination = progress.Destination
	tracker.state.Phase = progress.Phase
	tracker.state.UpdatedAt = time.Now().UTC()
}

// RuntimeTracker implements station.EventSink and is safe for concurrent HTTP
// snapshots while the station loop emits lifecycle events.
type RuntimeTracker struct {
	mu    sync.RWMutex
	state RuntimeStatus
}

func NewRuntimeTracker(started time.Time) *RuntimeTracker {
	started = started.UTC()
	return &RuntimeTracker{state: RuntimeStatus{State: "idle", StartedAt: started, UpdatedAt: started}}
}

func (tracker *RuntimeTracker) Emit(event station.Event) error {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	state := RuntimeStatus{
		State:      runtimeState(event.Type),
		StartedAt:  tracker.state.StartedAt,
		UpdatedAt:  event.Time.UTC(),
		LastEvent:  event.Type,
		Camera:     event.Camera,
		SourcePath: event.SourcePath,
		EvidenceID: event.EvidenceID,
		Files:      event.Files,
		Bytes:      event.Bytes,
		Verified:   event.Verified,
		Skipped:    event.Skipped,
		Failed:     event.Failed,
		Message:    event.Message,
	}
	if event.RetryAt != nil {
		retryAt := event.RetryAt.UTC()
		state.RetryAt = &retryAt
	}
	tracker.state = state
	return nil
}

func (tracker *RuntimeTracker) Snapshot() RuntimeStatus {
	tracker.mu.RLock()
	defer tracker.mu.RUnlock()
	snapshot := tracker.state
	if snapshot.RetryAt != nil {
		retryAt := *snapshot.RetryAt
		snapshot.RetryAt = &retryAt
	}
	return snapshot
}

func runtimeState(eventType string) string {
	switch eventType {
	case "source_detected":
		return "source-detected"
	case "inventory_complete":
		return "inventory-complete"
	case "ingest_started":
		return "ingesting"
	case "ingest_complete":
		return "complete"
	case "ingest_failed":
		return "retrying"
	case "source_changed":
		return "reconciling"
	case "source_removed":
		return "idle"
	case "source_blocked", "inventory_failed", "inventory_incomplete", "post_ingest_inventory_failed", "post_ingest_inventory_incomplete":
		return "blocked"
	default:
		return "unknown"
	}
}
