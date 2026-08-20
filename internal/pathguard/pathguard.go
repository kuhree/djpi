package pathguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// RefuseInside rejects a target equal to or contained by a protected root.
// Canonicalization follows symlinks through the nearest existing ancestor so
// future state/archive paths cannot be configured back onto camera storage.
func RefuseInside(protectedRoot, target string) error {
	root, err := canonicalFuturePath(protectedRoot)
	if err != nil {
		return err
	}
	resolvedTarget, err := canonicalFuturePath(target)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, resolvedTarget)
	if err != nil {
		return err
	}
	if relative == "." || (relative != ".." && !startsWithParent(relative)) {
		return fmt.Errorf("refusing path inside protected Source: %s", target)
	}
	return nil
}

func startsWithParent(path string) bool {
	return len(path) > 3 && path[:3] == ".."+string(filepath.Separator)
}

func canonicalFuturePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := absolute
	missing := make([]string, 0)
	for {
		_, statErr := os.Lstat(current)
		if statErr == nil {
			break
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", statErr
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
	resolved, err := filepath.EvalSymlinks(current)
	if err != nil {
		return "", err
	}
	for index := len(missing) - 1; index >= 0; index-- {
		resolved = filepath.Join(resolved, missing[index])
	}
	return filepath.Clean(resolved), nil
}
