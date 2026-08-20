package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInventoryPathCollectsStableSortedMetadata(t *testing.T) {
	root := t.TempDir()
	mustWriteFixture(t, root, "MISC/IDX/camera.db", []byte("db"))
	mustWriteFixture(t, root, "DCIM/DJI_001/DJI_0002.JPG", []byte("photo-two"))
	mustWriteFixture(t, root, "DCIM/DJI_001/DJI_0001.dng", []byte("raw-one"))

	first, err := InventoryPath(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := InventoryPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Complete || len(first.Issues) != 0 {
		t.Fatalf("expected complete inventory, got %#v", first)
	}
	if first.EvidenceID == "" || first.EvidenceID != second.EvidenceID {
		t.Fatalf("expected stable evidence ID, got %q and %q", first.EvidenceID, second.EvidenceID)
	}
	wantPaths := []string{"DCIM/DJI_001/DJI_0001.dng", "DCIM/DJI_001/DJI_0002.JPG", "MISC/IDX/camera.db"}
	for index, want := range wantPaths {
		if first.Items[index].Path != want {
			t.Fatalf("item %d path = %q, want %q", index, first.Items[index].Path, want)
		}
	}
	if first.Summary.Files != 3 || first.Summary.Bytes != 18 {
		t.Fatalf("unexpected summary: %#v", first.Summary)
	}
	if got := first.Summary.Extensions; len(got) != 3 || got[0].Extension != "db" || got[1].Extension != "dng" || got[2].Extension != "jpg" {
		t.Fatalf("unexpected extension summary: %#v", got)
	}
}

func TestInventoryChangesEvidenceWhenMetadataChanges(t *testing.T) {
	root := t.TempDir()
	path := mustWriteFixture(t, root, "DCIM/DJI_001/DJI_0001.JPG", []byte("one"))
	before, err := InventoryPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("longer"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := InventoryPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if before.EvidenceID == after.EvidenceID {
		t.Fatalf("expected metadata evidence ID to change")
	}
}

func TestInventoryRefusesToTreatSymlinkAsComplete(t *testing.T) {
	root := t.TempDir()
	mustWriteFixture(t, root, "outside.JPG", []byte("photo"))
	if err := os.Symlink(filepath.Join(root, "outside.JPG"), filepath.Join(root, "link.JPG")); err != nil {
		t.Fatal(err)
	}

	observation, err := InventoryPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Complete || observation.EvidenceID != "" {
		t.Fatalf("expected incomplete observation without evidence ID: %#v", observation)
	}
	if len(observation.Issues) != 1 || !strings.Contains(observation.Issues[0], "unsupported source entry type") {
		t.Fatalf("unexpected issues: %v", observation.Issues)
	}
}

func TestTextOutputStatesCleanupLimit(t *testing.T) {
	var output strings.Builder
	observation := Observation{SourcePath: "/camera", Complete: true, EvidenceID: "metadata-sha256:test", IdentityBasis: identityBasis}
	if err := WriteObservation(&output, observation, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"read-only source inventory", "not content verification", "cannot authorize cleanup"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q: %s", want, output.String())
		}
	}
}

func mustWriteFixture(t *testing.T, root, relative string, content []byte) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	fixed := time.Unix(1_700_000_000, 123)
	if err := os.Chtimes(path, fixed, fixed); err != nil {
		t.Fatal(err)
	}
	return path
}
