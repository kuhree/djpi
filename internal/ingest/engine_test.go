package ingest

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.kuhree.com/kuhree/djpi/internal/destination"
	"git.kuhree.com/kuhree/djpi/internal/journal"
	"git.kuhree.com/kuhree/djpi/internal/source"
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

func TestEngineFanoutReadsSourceTwiceForAnyDestinationCount(t *testing.T) {
	sourceRoot := t.TempDir()
	observation := ingestFixture(t, sourceRoot, map[string]string{"DCIM/one.JPG": strings.Repeat("camera", 1024)})
	reads := 0
	engine := Engine{
		Journal: journal.NewStore(filepath.Join(t.TempDir(), "journal.json")),
		Destinations: []destination.Destination{
			&fanoutTestDestination{name: "archive"},
			&fanoutTestDestination{name: "objects"},
			&fanoutTestDestination{name: "photos"},
		},
		OpenSource: func(path string) (*os.File, error) {
			reads++
			return os.Open(path)
		},
	}
	result, err := engine.Ingest(context.Background(), "camera", sourceRoot, observation)
	if err != nil || result.Verified != 3 || reads != 2 {
		t.Fatalf("result=%#v error=%v Source reads=%d, want 3 verified and 2 reads", result, err, reads)
	}
}

func TestEngineFanoutIsolatesDestinationWriteFailure(t *testing.T) {
	sourceRoot := t.TempDir()
	observation := ingestFixture(t, sourceRoot, map[string]string{"DCIM/one.JPG": strings.Repeat("camera", 1024)})
	healthy := &fanoutTestDestination{name: "archive"}
	failing := &fanoutTestDestination{name: "objects", failWrite: true}
	engine := Engine{Journal: journal.NewStore(filepath.Join(t.TempDir(), "journal.json")), Destinations: []destination.Destination{failing, healthy}}
	result, err := engine.Ingest(context.Background(), "camera", sourceRoot, observation)
	if err == nil || result.Verified != 1 || result.Failed != 1 || healthy.commits != 1 || failing.commits != 0 {
		t.Fatalf("result=%#v error=%v healthy commits=%d failed commits=%d", result, err, healthy.commits, failing.commits)
	}
}

func TestEngineFanoutTimesOutBlockedWriterAndCommitsHealthyDestination(t *testing.T) {
	sourceRoot := t.TempDir()
	observation := ingestFixture(t, sourceRoot, map[string]string{"DCIM/one.JPG": "camera"})
	healthy := &fanoutTestDestination{name: "archive"}
	blocked := &fanoutTestDestination{name: "objects", blockWrite: true}
	engine := Engine{Journal: journal.NewStore(filepath.Join(t.TempDir(), "journal.json")), Destinations: []destination.Destination{blocked, healthy}, OperationTimeout: 10 * time.Millisecond}
	result, err := engine.Ingest(context.Background(), "camera", sourceRoot, observation)
	if err == nil || result.Verified != 1 || result.Failed != 1 || healthy.commits != 1 || blocked.commits != 0 {
		t.Fatalf("result=%#v error=%v healthy commits=%d blocked commits=%d", result, err, healthy.commits, blocked.commits)
	}
}

func TestEngineFanoutAllowsDistinctLocalDestinationsSharingRoot(t *testing.T) {
	sourceRoot, archiveRoot := t.TempDir(), t.TempDir()
	observation := ingestFixture(t, sourceRoot, map[string]string{"DCIM/one.JPG": "camera"})
	engine := Engine{Journal: journal.NewStore(filepath.Join(t.TempDir(), "journal.json")), Destinations: []destination.Destination{
		destination.LocalArchive{DestinationName: "first", Root: archiveRoot},
		destination.LocalArchive{DestinationName: "second", Root: archiveRoot},
	}, OperationTimeout: 100 * time.Millisecond}
	result, err := engine.Ingest(context.Background(), "camera", sourceRoot, observation)
	if err != nil || result.Verified != 2 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestEngineFanoutFailsClosedWhenSourceChangesDuringDelivery(t *testing.T) {
	sourceRoot := t.TempDir()
	observation := ingestFixture(t, sourceRoot, map[string]string{"DCIM/one.JPG": "camera"})
	path := filepath.Join(sourceRoot, "DCIM", "one.JPG")
	target := &fanoutTestDestination{name: "archive"}
	reads := 0
	engine := Engine{
		Journal: journal.NewStore(filepath.Join(t.TempDir(), "journal.json")), Destinations: []destination.Destination{target},
		OpenSource: func(openPath string) (*os.File, error) {
			reads++
			if reads == 2 {
				if err := os.WriteFile(path, []byte("mutate"), 0o600); err != nil {
					t.Fatal(err)
				}
				modified := time.Unix(1_700_000_000, 0)
				if err := os.Chtimes(path, modified, modified); err != nil {
					t.Fatal(err)
				}
			}
			return os.Open(openPath)
		},
	}
	result, err := engine.Ingest(context.Background(), "camera", sourceRoot, observation)
	if err == nil || result.Failed != 1 || target.commits != 0 || !target.aborted {
		t.Fatalf("result=%#v error=%v commits=%d aborted=%t", result, err, target.commits, target.aborted)
	}
}

func TestEngineFanoutFailsClosedForShortSource(t *testing.T) {
	sourceRoot := t.TempDir()
	observation := ingestFixture(t, sourceRoot, map[string]string{"DCIM/one.JPG": "camera"})
	if err := os.Truncate(filepath.Join(sourceRoot, "DCIM", "one.JPG"), 3); err != nil {
		t.Fatal(err)
	}
	target := &fanoutTestDestination{name: "archive"}
	result, err := (Engine{Journal: journal.NewStore(filepath.Join(t.TempDir(), "journal.json")), Destinations: []destination.Destination{target}}).Ingest(context.Background(), "camera", sourceRoot, observation)
	if err == nil || result.Failed != 1 || target.commits != 0 || !target.aborted {
		t.Fatalf("result=%#v error=%v commits=%d aborted=%t", result, err, target.commits, target.aborted)
	}
}

func TestEngineFanoutResumeReadsSourceOnceAndDoesNotRestage(t *testing.T) {
	sourceRoot := t.TempDir()
	observation := ingestFixture(t, sourceRoot, map[string]string{"DCIM/one.JPG": "camera"})
	target := &fanoutTestDestination{name: "archive"}
	reads := 0
	engine := Engine{
		Journal: journal.NewStore(filepath.Join(t.TempDir(), "journal.json")), Destinations: []destination.Destination{target},
		OpenSource: func(path string) (*os.File, error) { reads++; return os.Open(path) },
	}
	if _, err := engine.Ingest(context.Background(), "camera", sourceRoot, observation); err != nil {
		t.Fatal(err)
	}
	reads, target.stages = 0, 0
	result, err := engine.Ingest(context.Background(), "camera", sourceRoot, observation)
	if err != nil || result.Skipped != 1 || reads != 1 || target.stages != 0 {
		t.Fatalf("result=%#v error=%v Source reads=%d stages=%d", result, err, reads, target.stages)
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

type fanoutTestDestination struct {
	name       string
	failWrite  bool
	blockWrite bool
	commits    int
	stages     int
	aborted    bool
}

func (d *fanoutTestDestination) Name() string { return d.name }
func (d *fanoutTestDestination) Deliver(context.Context, string, string, source.ObservedItem) (destination.Evidence, error) {
	return destination.Evidence{}, errors.New("legacy delivery must not be used")
}
func (d *fanoutTestDestination) Verify(context.Context, destination.Evidence) (bool, error) {
	return true, nil
}
func (d *fanoutTestDestination) Stage(context.Context, string, string, source.ObservedItem) (destination.StagedDelivery, error) {
	d.stages++
	return &fanoutTestStage{destination: d}, nil
}

type fanoutTestStage struct {
	destination *fanoutTestDestination
	bytes.Buffer
}

func (s *fanoutTestStage) Write(ctx context.Context, value []byte) (int, error) {
	if s.destination.blockWrite {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	if s.destination.failWrite {
		return 0, errors.New("injected writer failure")
	}
	return s.Buffer.Write(value)
}
func (s *fanoutTestStage) Commit(_ context.Context, digest string) (destination.Evidence, error) {
	s.destination.commits++
	return destination.Evidence{SizeBytes: int64(s.Len()), ContentDigest: digest, VerificationDigest: digest, Location: s.destination.name + "/item"}, nil
}
func (s *fanoutTestStage) Abort() error { s.destination.aborted = true; return nil }

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

func TestNamespaceForPrefersAliasAndShortensHashedIdentity(t *testing.T) {
	const identity = "camera-sha256:81fa9cdac0594f420f7b90ed004a4765e094cf45061eeca9cb9641a92db4fa0c"
	aliased := Engine{CameraAliases: map[string]string{identity: "action6"}}
	if got := aliased.namespaceFor(identity); got != "action6" {
		t.Fatalf("alias namespace = %q", got)
	}
	// Without an alias a hashed identity is shortened, never expanded to a
	// serial and never left as 64 hex characters.
	if got := (Engine{}).namespaceFor(identity); got != "camera-81fa9cda" {
		t.Fatalf("default namespace = %q", got)
	}
	// A non-hashed identity, such as the model fallback, passes through.
	if got := (Engine{}).namespaceFor("DJI Osmo Pocket 3"); got != "DJI Osmo Pocket 3" {
		t.Fatalf("passthrough namespace = %q", got)
	}
}
