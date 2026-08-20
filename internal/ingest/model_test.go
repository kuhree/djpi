package ingest

import "testing"

func TestCleanupReadyAfterMatchingVerifiedDelivery(t *testing.T) {
	snapshot := Snapshot{
		ID: "snapshot-1",
		Items: []MediaItem{{
			SourcePath:        "DCIM/100MEDIA/DJI_0001.MP4",
			Size:              42,
			Digest:            "sha256:abc",
			RemoteDeleteToken: "handle:123",
			Deliveries: map[string]Delivery{
				"archive": {Destination: "archive", State: DeliveryVerified, Size: 42, Digest: "sha256:abc"},
			},
		}},
	}

	got := AssessCleanup(CleanupRequest{
		Snapshot:             snapshot,
		CurrentSnapshotID:    snapshot.ID,
		RequiredDestinations: []string{"archive"},
		Mode:                 CleanupDeleteMedia,
	})

	if !got.Ready {
		t.Fatalf("expected cleanup to be ready, got reasons: %v", got.Reasons)
	}
}

func TestCleanupRefusesChangedSource(t *testing.T) {
	got := AssessCleanup(CleanupRequest{
		Snapshot: Snapshot{
			ID: "before",
			Items: []MediaItem{{
				SourcePath:        "DCIM/100MEDIA/DJI_0001.MP4",
				Size:              42,
				Digest:            "sha256:abc",
				RemoteDeleteToken: "handle:123",
				Deliveries: map[string]Delivery{
					"archive": {Destination: "archive", State: DeliveryVerified, Size: 42, Digest: "sha256:abc"},
				},
			}},
		},
		CurrentSnapshotID:    "after",
		RequiredDestinations: []string{"archive"},
		Mode:                 CleanupDeleteMedia,
	})

	if got.Ready {
		t.Fatalf("expected changed source to refuse cleanup")
	}
}

func TestCleanupRefusesUnverifiedOrAmbiguousMedia(t *testing.T) {
	got := AssessCleanup(CleanupRequest{
		Snapshot: Snapshot{
			ID: "snapshot-1",
			Items: []MediaItem{{
				SourcePath: "DCIM/100MEDIA/DJI_0001.JPG",
				Size:       42,
				Digest:     "sha256:abc",
				Deliveries: map[string]Delivery{
					"archive": {Destination: "archive", State: DeliveryComplete, Size: 42, Digest: "sha256:abc"},
				},
			}},
		},
		CurrentSnapshotID:    "snapshot-1",
		RequiredDestinations: []string{"archive"},
		Mode:                 CleanupDeleteMedia,
	})

	if got.Ready {
		t.Fatalf("expected incomplete verification and missing delete token to refuse cleanup")
	}
	if len(got.Reasons) != 2 {
		t.Fatalf("expected two refusal reasons, got %v", got.Reasons)
	}
}

func TestFormatRequiresProvenCameraCapability(t *testing.T) {
	got := AssessCleanup(CleanupRequest{
		Snapshot: Snapshot{
			ID: "snapshot-1",
			Items: []MediaItem{{
				SourcePath: "DCIM/100MEDIA/DJI_0001.JPG",
				Size:       42,
				Digest:     "sha256:abc",
				Deliveries: map[string]Delivery{
					"archive": {Destination: "archive", State: DeliveryVerified, Size: 42, Digest: "sha256:abc"},
				},
			}},
		},
		CurrentSnapshotID:    "snapshot-1",
		RequiredDestinations: []string{"archive"},
		Mode:                 CleanupFormatSource,
		FormatCapability:     false,
	})

	if got.Ready {
		t.Fatalf("expected unproven format capability to refuse cleanup")
	}
}
