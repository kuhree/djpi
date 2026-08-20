package journal

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStorePersistsAtomicJournalUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "journal.json")
	store := NewStore(path)
	err := store.Update(func(state *State) error {
		state.Sources["camera/source"] = SourceRecord{Camera: "DJI Osmo Pocket 3", MetadataEvidenceID: "metadata-sha256:test", Items: map[string]ItemRecord{
			"DCIM/DJI_001/test.MP4": {SizeBytes: 42, ContentDigest: "sha256:abc", Deliveries: map[string]DeliveryRecord{"archive": {State: "verified", SizeBytes: 42, ContentDigest: "sha256:abc", Location: "camera/sha256/abc/DCIM/DJI_001/test.MP4"}}},
		}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	item := loaded.Sources["camera/source"].Items["DCIM/DJI_001/test.MP4"]
	if loaded.Version != CurrentVersion || item.Deliveries["archive"].State != "verified" {
		t.Fatalf("unexpected persisted state: %#v", loaded)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("journal permissions = %o, want 600", info.Mode().Perm())
	}
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".journal-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("temporary journals remain: %v", leftovers)
	}
}

func TestStoreMigratesVersionOneLocalDeliveryLocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	legacy := `{"version":1,"sources":{"source":{"camera":"camera","metadata_evidence_id":"metadata","items":{"DCIM/clip.MP4":{"size_bytes":4,"content_digest":"sha256:test","deliveries":{"archive":{"state":"verified","size_bytes":4,"content_digest":"sha256:test"}}}}}}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	delivery := state.Sources["source"].Items["DCIM/clip.MP4"].Deliveries["archive"]
	if state.Version != CurrentVersion || delivery.Location != "DCIM/clip.MP4" || delivery.VerificationDigest != "sha256:test" {
		t.Fatalf("migrated state = %#v", state)
	}
}

func TestStoreMigratesVersionTwoVerificationDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	legacy := `{"version":2,"sources":{"source":{"camera":"camera","metadata_evidence_id":"metadata","items":{"DCIM/clip.MP4":{"size_bytes":4,"content_digest":"sha256:test","deliveries":{"archive":{"state":"verified","size_bytes":4,"content_digest":"sha256:test","location":"camera/sha256/test/DCIM/clip.MP4"}}}}}}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	delivery := state.Sources["source"].Items["DCIM/clip.MP4"].Deliveries["archive"]
	if state.Version != CurrentVersion || delivery.VerificationDigest != "sha256:test" {
		t.Fatalf("migrated state = %#v", state)
	}
}

func TestFailedUpdatePreservesPriorState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	store := NewStore(path)
	if err := store.Update(func(state *State) error { state.Sources["before"] = SourceRecord{}; return nil }); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("delivery failed")
	if err := store.Update(func(state *State) error { state.Sources["after"] = SourceRecord{}; return wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("got %v, want %v", err, wantErr)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Sources["before"]; !ok {
		t.Fatal("prior state was lost")
	}
	if _, ok := loaded.Sources["after"]; ok {
		t.Fatal("failed update was persisted")
	}
}

func TestStoreRejectsCorruptAndFutureJournal(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "journal.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(path).Load(); err == nil {
		t.Fatal("expected corrupt journal error")
	}
	if err := os.WriteFile(path, []byte(`{"version":999,"sources":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(path).Load(); err == nil {
		t.Fatal("expected future version error")
	}
}
