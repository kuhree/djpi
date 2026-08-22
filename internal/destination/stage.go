package destination

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const managedStageMaxAge = 24 * time.Hour

type fileStage struct {
	file      *os.File
	path      string
	commit    func(context.Context, string, string) (Evidence, error)
	closed    bool
	committed bool
}

func newFileStage(pattern string, commit func(context.Context, string, string) (Evidence, error)) (*fileStage, error) {
	return newFileStageIn("", pattern, commit)
}

func newManagedFileStage(size int64, commit func(context.Context, string, string) (Evidence, error)) (*fileStage, error) {
	directory := filepath.Join(os.TempDir(), "djpi-staging")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	if err := reclaimManagedStages(directory, time.Now()); err != nil {
		return nil, err
	}
	available, err := filesystemAvailableBytes(directory)
	if err != nil {
		return nil, fmt.Errorf("inspect remote staging capacity: %w", err)
	}
	required, err := requiredLocalCapacity(size)
	if err != nil {
		return nil, err
	}
	if available < required {
		return nil, fmt.Errorf("insufficient remote staging capacity: %d bytes available, %d required including headroom", available, required)
	}
	return newFileStageIn(directory, ".djpi-remote-*.tmp", commit)
}

func reclaimManagedStages(directory string, now time.Time) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".djpi-remote-") || !strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refuse non-regular remote staging entry %s", entry.Name())
		}
		if now.Sub(info.ModTime()) < managedStageMaxAge {
			continue
		}
		if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func newFileStageIn(directory, pattern string, commit func(context.Context, string, string) (Evidence, error)) (*fileStage, error) {
	file, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, err
	}
	return &fileStage{file: file, path: file.Name(), commit: commit}, nil
}

func (s *fileStage) Write(ctx context.Context, value []byte) (int, error) {
	if s.closed {
		return 0, errors.New("staged delivery is closed")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return s.file.Write(value)
}

func (s *fileStage) Commit(ctx context.Context, digest string) (Evidence, error) {
	if s.committed {
		return Evidence{}, errors.New("staged delivery was already committed")
	}
	if !s.closed {
		if err := s.file.Sync(); err != nil {
			return Evidence{}, err
		}
		if err := s.file.Close(); err != nil {
			return Evidence{}, err
		}
		s.closed = true
	}
	s.committed = true
	return s.commit(ctx, s.path, digest)
}

func (s *fileStage) Abort() error {
	var closeErr error
	if !s.closed {
		closeErr = s.file.Close()
		s.closed = true
	}
	removeErr := os.Remove(s.path)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	return errors.Join(closeErr, removeErr)
}
