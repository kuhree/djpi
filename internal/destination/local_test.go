package destination

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"git.kuhree.com/kuhree.com/djpi/internal/source"
)

func TestLocalArchiveDeliversVerifiesAndReusesMatchingFile(t *testing.T) {
	sourceRoot, archiveRoot := t.TempDir(), t.TempDir()
	item := fixtureItem(t, sourceRoot, "DCIM/DJI_001/clip.MP4", []byte("camera bytes"))
	archive := LocalArchive{DestinationName: "archive", Root: archiveRoot}
	evidence, err := archive.Deliver(context.Background(), sourceRoot, item)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.SizeBytes != item.SizeBytes || evidence.ContentDigest == "" {
		t.Fatalf("missing evidence: %#v", evidence)
	}
	verified, err := archive.Verify(context.Background(), item, evidence)
	if err != nil || !verified {
		t.Fatalf("verify = %t, %v", verified, err)
	}
	second, err := archive.Deliver(context.Background(), sourceRoot, item)
	if err != nil || second != evidence {
		t.Fatalf("idempotent delivery = %#v, %v", second, err)
	}
	content, err := os.ReadFile(filepath.Join(archiveRoot, filepath.FromSlash(item.Path)))
	if err != nil || string(content) != "camera bytes" {
		t.Fatalf("destination content = %q, %v", content, err)
	}
}

func TestLocalArchiveRefusesConflictAndChangedSource(t *testing.T) {
	sourceRoot, archiveRoot := t.TempDir(), t.TempDir()
	item := fixtureItem(t, sourceRoot, "DCIM/photo.JPG", []byte("source"))
	conflictPath := filepath.Join(archiveRoot, "DCIM", "photo.JPG")
	if err := os.MkdirAll(filepath.Dir(conflictPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflictPath, []byte("other!"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := LocalArchive{DestinationName: "archive", Root: archiveRoot}
	if _, err := archive.Deliver(context.Background(), sourceRoot, item); err == nil {
		t.Fatal("expected destination conflict")
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, filepath.FromSlash(item.Path)), []byte("changed size"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Deliver(context.Background(), sourceRoot, item); err == nil {
		t.Fatal("expected changed source refusal")
	}
}

func TestLocalArchiveRejectsEscapingPath(t *testing.T) {
	archive := LocalArchive{DestinationName: "archive", Root: t.TempDir()}
	if _, err := archive.Deliver(context.Background(), t.TempDir(), source.ObservedItem{Path: "../escape", SizeBytes: 1}); err == nil {
		t.Fatal("expected unsafe path refusal")
	}
}

func TestLocalArchiveRefusesDestinationInsideSource(t *testing.T) {
	sourceRoot := t.TempDir()
	item := fixtureItem(t, sourceRoot, "DCIM/photo.JPG", []byte("photo"))
	archive := LocalArchive{DestinationName: "bad", Root: filepath.Join(sourceRoot, "archive")}
	if _, err := archive.Deliver(context.Background(), sourceRoot, item); err == nil {
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
