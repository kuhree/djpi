package ingest

import (
	"fmt"
	"sort"
)

// DeliveryState describes what djpi has proved about one destination copy.
type DeliveryState string

const (
	DeliveryPending  DeliveryState = "pending"
	DeliveryComplete DeliveryState = "complete"
	DeliveryVerified DeliveryState = "verified"
	DeliveryFailed   DeliveryState = "failed"
)

// Delivery records the evidence for one destination copy.
type Delivery struct {
	Destination string
	State       DeliveryState
	Size        int64
	Digest      string
}

// MediaItem is one immutable item observed in a source snapshot.
type MediaItem struct {
	SourcePath        string
	Size              int64
	Digest            string
	RemoteDeleteToken string
	Deliveries        map[string]Delivery
}

// Snapshot is a complete view of one camera storage area.
type Snapshot struct {
	ID    string
	Items []MediaItem
}

// CleanupMode names the destructive operation being considered.
type CleanupMode string

const (
	CleanupDeleteMedia  CleanupMode = "delete-media"
	CleanupFormatSource CleanupMode = "format-source"
)

// CleanupRequest contains the evidence required to consider cleanup.
type CleanupRequest struct {
	Snapshot             Snapshot
	CurrentSnapshotID    string
	RequiredDestinations []string
	Mode                 CleanupMode
	FormatCapability     bool
}

// CleanupAssessment is a deterministic explanation, not permission to bypass
// a camera integration's own checks.
type CleanupAssessment struct {
	Ready   bool
	Reasons []string
}

// AssessCleanup proves whether a destructive cleanup may be attempted.
func AssessCleanup(req CleanupRequest) CleanupAssessment {
	reasons := make([]string, 0)
	if req.Snapshot.ID == "" {
		reasons = append(reasons, "source snapshot has no identity")
	}
	if req.CurrentSnapshotID == "" || req.CurrentSnapshotID != req.Snapshot.ID {
		reasons = append(reasons, "source changed or could not be re-verified")
	}
	if len(req.Snapshot.Items) == 0 {
		reasons = append(reasons, "source snapshot is empty")
	}

	required := uniqueSorted(req.RequiredDestinations)
	if len(required) == 0 {
		reasons = append(reasons, "no required destinations are configured")
	}

	for _, item := range req.Snapshot.Items {
		label := item.SourcePath
		if label == "" {
			label = "<unnamed media item>"
		}
		if item.Size < 0 || item.Digest == "" {
			reasons = append(reasons, fmt.Sprintf("%s has no stable content identity", label))
		}
		for _, destination := range required {
			delivery, ok := item.Deliveries[destination]
			if !ok || delivery.State != DeliveryVerified {
				reasons = append(reasons, fmt.Sprintf("%s is not verified at %s", label, destination))
				continue
			}
			if delivery.Size != item.Size || delivery.Digest != item.Digest {
				reasons = append(reasons, fmt.Sprintf("%s verification evidence does not match at %s", label, destination))
			}
		}
		if req.Mode == CleanupDeleteMedia && item.RemoteDeleteToken == "" {
			reasons = append(reasons, fmt.Sprintf("%s has no unambiguous camera delete token", label))
		}
	}

	switch req.Mode {
	case CleanupDeleteMedia:
	case CleanupFormatSource:
		if !req.FormatCapability {
			reasons = append(reasons, "camera format capability has not been proven")
		}
	default:
		reasons = append(reasons, "cleanup mode is not supported")
	}

	sort.Strings(reasons)
	return CleanupAssessment{Ready: len(reasons) == 0, Reasons: reasons}
}

func uniqueSorted(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
