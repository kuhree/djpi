package destination

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.kuhree.com/kuhree.com/djpi/internal/source"
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
