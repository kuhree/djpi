package pathguard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRefuseInsideHandlesFutureAndSiblingPaths(t *testing.T) {
	root := t.TempDir()
	if err := RefuseInside(root, filepath.Join(root, "future", "archive")); err == nil {
		t.Fatal("expected nested path refusal")
	}
	if err := RefuseInside(root, root); err == nil {
		t.Fatal("expected equal path refusal")
	}
	if err := RefuseInside(root, root+"-sibling"); err != nil {
		t.Fatalf("sibling should be allowed: %v", err)
	}
}

func TestRefuseInsideResolvesSymlinkedAncestor(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(outside, "camera-link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if err := RefuseInside(root, filepath.Join(link, "future")); err == nil {
		t.Fatal("expected symlinked nested path refusal")
	}
}
