package ingest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	archive := destination.LocalArchive{DestinationName: "archive", Root: archiveRoot}
	evidence, err := archive.Deliver(context.Background(), sourceRoot, "camera", observation.Items[0])
	if err != nil {
		t.Fatal(err)
	}
	conflict := filepath.Join(archiveRoot, filepath.FromSlash(evidence.Location))
	if err := os.WriteFile(conflict, []byte("wrong"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := journal.NewStore(filepath.Join(t.TempDir(), "journal.json"))
	engine := Engine{Journal: store, Destinations: []destination.Destination{archive}}
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

func TestEngineRejectsIncompleteDestinationEvidence(t *testing.T) {
	sourceRoot := t.TempDir()
	observation := ingestFixture(t, sourceRoot, map[string]string{"DCIM/one.JPG": "photo"})
	engine := Engine{
		Journal:      journal.NewStore(filepath.Join(t.TempDir(), "journal.json")),
		Destinations: []destination.Destination{incompleteEvidenceDestination{}},
	}
	result, err := engine.Ingest(context.Background(), "camera", sourceRoot, observation)
	if err == nil || result.Failed != 1 {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestEngineBoundsDestinationOperationTime(t *testing.T) {
	sourceRoot := t.TempDir()
	observation := ingestFixture(t, sourceRoot, map[string]string{"DCIM/one.JPG": "photo"})
	engine := Engine{
		Journal:          journal.NewStore(filepath.Join(t.TempDir(), "journal.json")),
		Destinations:     []destination.Destination{blockingDestination{}},
		OperationTimeout: 10 * time.Millisecond,
	}
	result, err := engine.Ingest(context.Background(), "camera", sourceRoot, observation)
	if err == nil || result.Failed != 1 || len(result.Failures) != 1 {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	if !strings.Contains(result.Failures[0], context.DeadlineExceeded.Error()) {
		t.Fatalf("failure = %q", result.Failures[0])
	}
}

func TestEngineReportsObservationalItemProgress(t *testing.T) {
	sourceRoot, archiveRoot := t.TempDir(), t.TempDir()
	observation := ingestFixture(t, sourceRoot, map[string]string{"DCIM/one.JPG": "photo"})
	var updates []Progress
	engine := Engine{
		Journal:      journal.NewStore(filepath.Join(t.TempDir(), "journal.json")),
		Destinations: []destination.Destination{destination.LocalArchive{DestinationName: "archive", Root: archiveRoot}},
		Progress:     func(update Progress) { updates = append(updates, update) },
	}
	if _, err := engine.Ingest(context.Background(), "camera", sourceRoot, observation); err != nil {
		t.Fatal(err)
	}
	if len(updates) != 2 || updates[0].Phase != "delivering" || updates[1].Phase != "verified" {
		t.Fatalf("first updates = %#v", updates)
	}
	for _, update := range updates {
		if update.ItemPath != "DCIM/one.JPG" || update.ItemIndex != 1 || update.ItemTotal != 1 || update.Destination != "archive" || update.SourceID == "" {
			t.Fatalf("incomplete progress = %#v", update)
		}
	}
	updates = nil
	if _, err := engine.Ingest(context.Background(), "camera", sourceRoot, observation); err != nil {
		t.Fatal(err)
	}
	wantPhases := []string{"source-check", "verifying", "skipped"}
	if len(updates) != len(wantPhases) {
		t.Fatalf("resume updates = %#v", updates)
	}
	for index, wanted := range wantPhases {
		if updates[index].Phase != wanted {
			t.Fatalf("resume update %d = %#v, want %s", index, updates[index], wanted)
		}
	}
}

type incompleteEvidenceDestination struct{}

func (incompleteEvidenceDestination) Name() string { return "incomplete" }
func (incompleteEvidenceDestination) Deliver(context.Context, string, string, source.ObservedItem) (destination.Evidence, error) {
	return destination.Evidence{SizeBytes: 5, ContentDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000"}, nil
}

type blockingDestination struct{}

func (blockingDestination) Name() string { return "blocking" }
func (blockingDestination) Deliver(ctx context.Context, _ string, _ string, _ source.ObservedItem) (destination.Evidence, error) {
	<-ctx.Done()
	return destination.Evidence{}, ctx.Err()
}
func (blockingDestination) Verify(context.Context, destination.Evidence) (bool, error) {
	return false, errors.New("unexpected verify")
}
func (incompleteEvidenceDestination) Verify(context.Context, destination.Evidence) (bool, error) {
	return false, nil
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
