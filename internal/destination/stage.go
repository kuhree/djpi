package destination

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const managedStageMaxAge = 24 * time.Hour

var stageReservations = struct {
	sync.Mutex
	bytes map[string]uint64
}{bytes: make(map[string]uint64)}

type fileStage struct {
	file               *os.File
	path               string
	commit             func(context.Context, string, string) (Evidence, error)
	closed             bool
	committed          bool
	releaseReservation func()
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
	required, err := requiredLocalCapacity(size)
	if err != nil {
		return nil, err
	}
	release, err := reserveStageCapacity(directory, required, filesystemAvailableBytes)
	if err != nil {
		return nil, fmt.Errorf("reserve remote staging capacity: %w", err)
	}
	stage, err := newFileStageIn(directory, ".djpi-remote-*.tmp", commit)
	if err != nil {
		release()
		return nil, err
	}
	stage.releaseReservation = release
	return stage, nil
}

func reserveStageCapacity(directory string, required uint64, availableBytes func(string) (uint64, error)) (func(), error) {
	directory = filepath.Clean(directory)
	stageReservations.Lock()
	defer stageReservations.Unlock()
	available, err := availableBytes(directory)
	if err != nil {
		return nil, err
	}
	reserved := stageReservations.bytes[directory]
	if required > available || reserved > available-required {
		return nil, fmt.Errorf("insufficient staging capacity: %d bytes available, %d already reserved, %d required", available, reserved, required)
	}
	stageReservations.bytes[directory] = reserved + required
	released := false
	return func() {
		stageReservations.Lock()
		defer stageReservations.Unlock()
		if released {
			return
		}
		released = true
		remaining := stageReservations.bytes[directory] - required
		if remaining == 0 {
			delete(stageReservations.bytes, directory)
		} else {
			stageReservations.bytes[directory] = remaining
		}
	}, nil
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
	if s.releaseReservation != nil {
		defer s.releaseReservation()
	}
	return s.commit(ctx, s.path, digest)
}

func (s *fileStage) Abort() error {
	if s.releaseReservation != nil {
		defer s.releaseReservation()
	}
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
