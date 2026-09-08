package destination

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.kuhree.com/kuhree/djpi/internal/source"
)

func TestLocalArchiveRefusesInsufficientCapacityBeforeStagingBytes(t *testing.T) {
	sourceRoot, archiveRoot := t.TempDir(), t.TempDir()
	item := fixtureItem(t, sourceRoot, "DCIM/large.MP4", []byte("camera bytes"))
	archive := LocalArchive{
		DestinationName: "archive",
		Root:            archiveRoot,
		availableBytes:  func(string) (uint64, error) { return uint64(item.SizeBytes), nil },
	}
	if _, err := archive.Deliver(context.Background(), sourceRoot, "camera", item); err == nil || !strings.Contains(err.Error(), "insufficient local destination capacity") {
		t.Fatalf("error = %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(archiveRoot, ".staging"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("staging contains media data: %#v", entries)
	}
}

func TestLocalArchiveFailsClosedWhenCapacityCannotBeRead(t *testing.T) {
	sourceRoot, archiveRoot := t.TempDir(), t.TempDir()
	item := fixtureItem(t, sourceRoot, "DCIM/photo.JPG", []byte("photo"))
	archive := LocalArchive{
		DestinationName: "archive",
		Root:            archiveRoot,
		availableBytes:  func(string) (uint64, error) { return 0, errors.New("statfs unavailable") },
	}
	if _, err := archive.Deliver(context.Background(), sourceRoot, "camera", item); err == nil || !strings.Contains(err.Error(), "statfs unavailable") {
		t.Fatalf("error = %v", err)
	}
}

func TestRequiredLocalCapacityIncludesBoundedHeadroom(t *testing.T) {
	if got, err := requiredLocalCapacity(1); err != nil || got != minimumLocalHeadroom+1 {
		t.Fatalf("small capacity = %d, %v", got, err)
	}
	if got, err := requiredLocalCapacity(10_000_000_000); err != nil || got != 10_100_000_000 {
		t.Fatalf("large capacity = %d, %v", got, err)
	}
	if _, err := requiredLocalCapacity(-1); err == nil {
		t.Fatal("expected negative size refusal")
	}
}

func TestLocalArchiveDeliversVerifiesAndReusesMatchingFile(t *testing.T) {
	sourceRoot, archiveRoot := t.TempDir(), t.TempDir()
	item := fixtureItem(t, sourceRoot, "DCIM/DJI_001/clip.MP4", []byte("camera bytes"))
	archive := LocalArchive{DestinationName: "archive", Root: archiveRoot}
	evidence, err := archive.Deliver(context.Background(), sourceRoot, "camera-one", item)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.SizeBytes != item.SizeBytes || evidence.ContentDigest == "" {
		t.Fatalf("missing evidence: %#v", evidence)
	}
	verified, err := archive.Verify(context.Background(), evidence)
	if err != nil || !verified {
		t.Fatalf("verify = %t, %v", verified, err)
	}
	second, err := archive.Deliver(context.Background(), sourceRoot, "camera-one", item)
	if err != nil || second != evidence {
		t.Fatalf("idempotent delivery = %#v, %v", second, err)
	}
	content, err := os.ReadFile(filepath.Join(archiveRoot, filepath.FromSlash(evidence.Location)))
	if err != nil || string(content) != "camera bytes" {
		t.Fatalf("destination content = %q, %v", content, err)
	}
}

func TestLocalArchiveReclaimsOnlyRegularDjpiStagingFiles(t *testing.T) {
	sourceRoot, archiveRoot := t.TempDir(), t.TempDir()
	item := fixtureItem(t, sourceRoot, "DCIM/photo.JPG", []byte("photo"))
	staging := filepath.Join(archiveRoot, ".staging")
	if err := os.MkdirAll(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(staging, ".djpi-crashed.tmp")
	unrelated := filepath.Join(staging, "operator-note")
	if err := os.WriteFile(stale, []byte("partial media"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelated, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := LocalArchive{DestinationName: "archive", Root: archiveRoot}
	if _, err := archive.Deliver(context.Background(), sourceRoot, "camera", item); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale temporary file remains: %v", err)
	}
	if content, err := os.ReadFile(unrelated); err != nil || string(content) != "keep" {
		t.Fatalf("unrelated staging entry changed: %q, %v", content, err)
	}
}

func TestLocalArchiveRefusesNonRegularStaleEntry(t *testing.T) {
	sourceRoot, archiveRoot := t.TempDir(), t.TempDir()
	item := fixtureItem(t, sourceRoot, "DCIM/photo.JPG", []byte("photo"))
	staleDirectory := filepath.Join(archiveRoot, ".staging", ".djpi-unexpected.tmp")
	if err := os.MkdirAll(staleDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	archive := LocalArchive{DestinationName: "archive", Root: archiveRoot}
	if _, err := archive.Deliver(context.Background(), sourceRoot, "camera", item); err == nil || !strings.Contains(err.Error(), "refuse non-regular stale staging entry") {
		t.Fatalf("error = %v", err)
	}
	if info, err := os.Stat(staleDirectory); err != nil || !info.IsDir() {
		t.Fatalf("unexpected entry was changed: %v", err)
	}
}

func TestLocalArchiveLockWaitHonorsContext(t *testing.T) {
	sourceRoot, archiveRoot := t.TempDir(), t.TempDir()
	item := fixtureItem(t, sourceRoot, "DCIM/photo.JPG", []byte("photo"))
	if err := os.MkdirAll(filepath.Join(archiveRoot, ".staging"), 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireLocalArchiveLock(context.Background(), archiveRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseLocalArchiveLock(lock)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	archive := LocalArchive{DestinationName: "archive", Root: archiveRoot}
	if _, err := archive.Deliver(ctx, sourceRoot, "camera", item); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}

func TestLocalArchiveRefusesConflictAndChangedSource(t *testing.T) {
	sourceRoot, archiveRoot := t.TempDir(), t.TempDir()
	item := fixtureItem(t, sourceRoot, "DCIM/photo.JPG", []byte("source"))
	archive := LocalArchive{DestinationName: "archive", Root: archiveRoot}
	firstEvidence, err := archive.Deliver(context.Background(), sourceRoot, "camera", item)
	if err != nil {
		t.Fatal(err)
	}
	conflictPath := filepath.Join(archiveRoot, filepath.FromSlash(firstEvidence.Location))
	if err := os.MkdirAll(filepath.Dir(conflictPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflictPath, []byte("other!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Deliver(context.Background(), sourceRoot, "camera", item); err == nil {
		t.Fatal("expected destination conflict")
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, filepath.FromSlash(item.Path)), []byte("changed size"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Deliver(context.Background(), sourceRoot, "camera", item); err == nil {
		t.Fatal("expected changed source refusal")
	}
}

func TestLocalArchiveRejectsEscapingPath(t *testing.T) {
	archive := LocalArchive{DestinationName: "archive", Root: t.TempDir()}
	if _, err := archive.Deliver(context.Background(), t.TempDir(), "camera", source.ObservedItem{Path: "../escape", SizeBytes: 1}); err == nil {
		t.Fatal("expected unsafe path refusal")
	}
}

func TestLocalArchiveRefusesDestinationInsideSource(t *testing.T) {
	sourceRoot := t.TempDir()
	item := fixtureItem(t, sourceRoot, "DCIM/photo.JPG", []byte("photo"))
	archive := LocalArchive{DestinationName: "bad", Root: filepath.Join(sourceRoot, "archive")}
	if _, err := archive.Deliver(context.Background(), sourceRoot, "camera", item); err == nil {
		t.Fatal("expected camera-contained destination refusal")
	}
	if _, err := os.Stat(filepath.Join(sourceRoot, "archive")); !os.IsNotExist(err) {
		t.Fatalf("destination directory was created on Source: %v", err)
	}
}

func fixtureItem(t *testing.T, root, relative string, content []byte) source.ObservedItem {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	modified := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	return source.ObservedItem{Path: relative, SizeBytes: int64(len(content)), ModifiedUnixNanos: modified.UnixNano()}
}

func TestContentLocationMirrorsSourceAndDisambiguatesByContent(t *testing.T) {
	const digest = "sha256:fb527e791266b2a48bd89271e283cbfb200f654490a9848768f15aeff3a814bf"
	got := contentLocation("action6", digest, "DCIM/DJI_001/DJI_20260818190329_0020_D_006.JPG")
	want := "action6/DCIM/DJI_001/DJI_20260818190329_0020_D_006.fb527e791266.JPG"
	if got != want {
		t.Fatalf("location = %q, want %q", got, want)
	}

	// A Camera reuses item paths after a format, so identical paths holding
	// different content must never resolve to one object.
	other := contentLocation("action6", "sha256:0123456789abcdef"+strings.Repeat("0", 48), "DCIM/DJI_001/DJI_20260818190329_0020_D_006.JPG")
	if other == got {
		t.Fatalf("different content collided at %q", got)
	}

	// The same content at the same path must stay idempotent.
	if again := contentLocation("action6", digest, "DCIM/DJI_001/DJI_20260818190329_0020_D_006.JPG"); again != got {
		t.Fatalf("not idempotent: %q vs %q", again, got)
	}

	// An item without an extension keeps the digest as its final segment.
	if got := contentLocation("action6", digest, "MISC/IDX"); got != "action6/MISC/IDX.fb527e791266" {
		t.Fatalf("extensionless location = %q", got)
	}
}
