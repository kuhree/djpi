package destination

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReclaimManagedStagesRemovesOnlyStaleDjpiFiles(t *testing.T) {
	directory := t.TempDir()
	stale := filepath.Join(directory, ".djpi-remote-stale.tmp")
	fresh := filepath.Join(directory, ".djpi-remote-fresh.tmp")
	unrelated := filepath.Join(directory, "operator-file")
	for _, path := range []string{stale, fresh, unrelated} {
		if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	old := now.Add(-managedStageMaxAge - time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := reclaimManagedStages(directory, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale stage remains: %v", err)
	}
	for _, path := range []string{fresh, unrelated} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("preserved file %s: %v", path, err)
		}
	}
}
