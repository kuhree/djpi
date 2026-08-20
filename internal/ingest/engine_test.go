package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"git.kuhree.com/kuhree.com/djpi/internal/destination"
	"git.kuhree.com/kuhree.com/djpi/internal/journal"
	"git.kuhree.com/kuhree.com/djpi/internal/source"
)

func TestEngineDeliversVerifiesJournalsAndResumes(t *testing.T) {
	sourceRoot, archiveRoot, stateRoot := t.TempDir(), t.TempDir(), t.TempDir()
	observation := ingestFixture(t, sourceRoot, map[string]string{"DCIM/one.JPG": "photo", "DCIM/two.MP4": "video"})
	store := journal.NewStore(filepath.Join(stateRoot, "journal.json"))
	engine := Engine{Journal: store, Destinations: []destination.Destination{destination.LocalArchive{DestinationName: "archive", Root: archiveRoot}}, Now: func() time.Time { return time.Unix(2, 0) }}
	first, err := engine.Ingest(context.Background(), "DJI Osmo Pocket 3", sourceRoot, observation)
	if err != nil {
		t.Fatal(err)
	}
	if first.Verified != 2 || first.Skipped != 0 {
		t.Fatalf("unexpected first result: %#v", first)
	}
	second, err := engine.Ingest(context.Background(), "DJI Osmo Pocket 3", sourceRoot, observation)
	if err != nil {
		t.Fatal(err)
	}
	if second.Verified != 0 || second.Skipped != 2 {
		t.Fatalf("unexpected resumed result: %#v", second)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	record := state.Sources[first.SourceID]
	if len(record.Items) != 2 || record.Items["DCIM/one.JPG"].Deliveries["archive"].State != "verified" {
		t.Fatalf("unexpected journal: %#v", state)
	}
}

func TestEngineRefusesSameMetadataWithChangedContent(t *testing.T) {
	sourceRoot, archiveRoot := t.TempDir(), t.TempDir()
	observation := ingestFixture(t, sourceRoot, map[string]string{"DCIM/one.JPG": "first"})
	store := journal.NewStore(filepath.Join(t.TempDir(), "journal.json"))
	engine := Engine{Journal: store, Destinations: []destination.Destination{destination.LocalArchive{DestinationName: "archive", Root: archiveRoot}}}
	if _, err := engine.Ingest(context.Background(), "camera", sourceRoot, observation); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sourceRoot, "DCIM", "one.JPG")
	if err := os.WriteFile(path, []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	modified := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	result, err := engine.Ingest(context.Background(), "camera", sourceRoot, observation)
	if err == nil || result.Failed != 1 {
		t.Fatalf("expected metadata collision failure, got %#v, %v", result, err)
	}
}

func TestEngineRecordsDestinationConflict(t *testing.T) {
	sourceRoot, archiveRoot := t.TempDir(), t.TempDir()
	observation := ingestFixture(t, sourceRoot, map[string]string{"DCIM/one.JPG": "photo"})
	conflict := filepath.Join(archiveRoot, "DCIM", "one.JPG")
	if err := os.MkdirAll(filepath.Dir(conflict), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflict, []byte("wrong"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := journal.NewStore(filepath.Join(t.TempDir(), "journal.json"))
	engine := Engine{Journal: store, Destinations: []destination.Destination{destination.LocalArchive{DestinationName: "archive", Root: archiveRoot}}}
	result, err := engine.Ingest(context.Background(), "camera", sourceRoot, observation)
	if err == nil || result.Failed != 1 {
		t.Fatalf("expected conflict failure, got %#v, %v", result, err)
	}
	state, loadErr := store.Load()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if state.Sources[result.SourceID].Items["DCIM/one.JPG"].Deliveries["archive"].State != "failed" {
		t.Fatalf("failure not journaled: %#v", state)
	}
}

func TestEngineRefusesJournalInsideSourceBeforeWriting(t *testing.T) {
	sourceRoot := t.TempDir()
	observation := ingestFixture(t, sourceRoot, map[string]string{"DCIM/one.JPG": "photo"})
	journalPath := filepath.Join(sourceRoot, "state", "journal.json")
	engine := Engine{Journal: journal.NewStore(journalPath), Destinations: []destination.Destination{destination.LocalArchive{DestinationName: "archive", Root: t.TempDir()}}}
	if _, err := engine.Ingest(context.Background(), "camera", sourceRoot, observation); err == nil {
		t.Fatal("expected Source-contained journal refusal")
	}
	if _, err := os.Stat(filepath.Dir(journalPath)); !os.IsNotExist(err) {
		t.Fatalf("journal directory was created on Source: %v", err)
	}
}

func ingestFixture(t *testing.T, root string, files map[string]string) source.Observation {
	t.Helper()
	modified := time.Unix(1_700_000_000, 0)
	for relative, content := range files {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	observation, err := source.InventoryPath(root)
	if err != nil {
		t.Fatal(err)
	}
	return observation
}
