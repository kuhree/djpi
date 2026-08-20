package destination

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"git.kuhree.com/kuhree.com/djpi/internal/pathguard"
	"git.kuhree.com/kuhree.com/djpi/internal/source"
)

type LocalArchive struct {
	DestinationName string
	Root            string
}

func (d LocalArchive) Name() string { return d.DestinationName }

func (d LocalArchive) Deliver(ctx context.Context, sourceRoot string, item source.ObservedItem) (Evidence, error) {
	if d.DestinationName == "" {
		return Evidence{}, errors.New("local destination has no name")
	}
	if d.Root == "" {
		return Evidence{}, errors.New("local destination has no root")
	}
	if err := pathguard.RefuseInside(sourceRoot, d.Root); err != nil {
		return Evidence{}, err
	}
	relative, err := safeRelativePath(item.Path)
	if err != nil {
		return Evidence{}, err
	}
	sourcePath := filepath.Join(sourceRoot, relative)
	destinationPath := filepath.Join(d.Root, relative)

	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return Evidence{}, err
	}
	defer sourceFile.Close()
	info, err := sourceFile.Stat()
	if err != nil {
		return Evidence{}, err
	}
	if !info.Mode().IsRegular() || info.Size() != item.SizeBytes || info.ModTime().UnixNano() != item.ModifiedUnixNanos {
		return Evidence{}, fmt.Errorf("source item changed since inventory: %s", item.Path)
	}
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o700); err != nil {
		return Evidence{}, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destinationPath), ".djpi-*.tmp")
	if err != nil {
		return Evidence{}, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return Evidence{}, err
	}

	hash := sha256.New()
	written, copyErr := copyContext(ctx, io.MultiWriter(temporary, hash), sourceFile)
	if copyErr != nil {
		_ = temporary.Close()
		return Evidence{}, copyErr
	}
	if written != item.SizeBytes {
		_ = temporary.Close()
		return Evidence{}, fmt.Errorf("source size changed while reading %s", item.Path)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return Evidence{}, err
	}
	if err := temporary.Close(); err != nil {
		return Evidence{}, err
	}
	evidence := Evidence{SizeBytes: written, ContentDigest: fmt.Sprintf("sha256:%x", hash.Sum(nil))}
	sourceStable, err := verifyPath(ctx, sourcePath, evidence)
	if err != nil {
		return Evidence{}, err
	}
	if !sourceStable {
		return Evidence{}, fmt.Errorf("source content changed while delivering %s", item.Path)
	}

	if existing, verifyErr := verifyPath(ctx, destinationPath, evidence); verifyErr != nil {
		return Evidence{}, verifyErr
	} else if existing {
		return evidence, nil
	}
	if _, err := os.Stat(destinationPath); err == nil {
		return Evidence{}, fmt.Errorf("destination conflict at %s", destinationPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Evidence{}, err
	}
	if err := os.Rename(temporaryPath, destinationPath); err != nil {
		return Evidence{}, err
	}
	if err := syncDirectory(filepath.Dir(destinationPath)); err != nil {
		return Evidence{}, err
	}
	verified, err := verifyPath(ctx, destinationPath, evidence)
	if err != nil {
		return Evidence{}, err
	}
	if !verified {
		return Evidence{}, fmt.Errorf("destination verification failed after commit: %s", destinationPath)
	}
	return evidence, nil
}

func (d LocalArchive) Verify(ctx context.Context, item source.ObservedItem, evidence Evidence) (bool, error) {
	relative, err := safeRelativePath(item.Path)
	if err != nil {
		return false, err
	}
	return verifyPath(ctx, filepath.Join(d.Root, relative), evidence)
}

func safeRelativePath(path string) (string, error) {
	if path == "" || strings.ContainsRune(path, '\x00') || filepath.IsAbs(path) {
		return "", fmt.Errorf("unsafe source item path %q", path)
	}
	cleaned := filepath.Clean(filepath.FromSlash(path))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe source item path %q", path)
	}
	return cleaned, nil
}

func verifyPath(ctx context.Context, path string, evidence Evidence) (bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() != evidence.SizeBytes {
		return false, nil
	}
	hash := sha256.New()
	read, err := copyContext(ctx, hash, file)
	if err != nil {
		return false, err
	}
	return read == evidence.SizeBytes && fmt.Sprintf("sha256:%x", hash.Sum(nil)) == evidence.ContentDigest, nil
}

func copyContext(ctx context.Context, writer io.Writer, reader io.Reader) (int64, error) {
	buffer := make([]byte, 1024*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		count, readErr := reader.Read(buffer)
		if count > 0 {
			written, writeErr := writer.Write(buffer[:count])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != count {
				return total, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
