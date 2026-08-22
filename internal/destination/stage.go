package destination

import (
	"context"
	"errors"
	"os"
)

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

func (s *fileStage) Write(value []byte) (int, error) {
	if s.closed {
		return 0, errors.New("staged delivery is closed")
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
