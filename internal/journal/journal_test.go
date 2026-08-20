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
			"DCIM/DJI_001/test.MP4": {SizeBytes: 42, ContentDigest: "sha256:abc", Deliveries: map[string]DeliveryRecord{"archive": {State: "verified", SizeBytes: 42, ContentDigest: "sha256:abc"}}},
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
