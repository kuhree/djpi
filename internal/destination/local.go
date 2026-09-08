package destination

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"git.kuhree.com/kuhree/djpi/internal/pathguard"
	"git.kuhree.com/kuhree/djpi/internal/source"
)

type LocalArchive struct {
	DestinationName string
	Root            string
	availableBytes  func(string) (uint64, error)
}

const minimumLocalHeadroom = 16 * 1024 * 1024

func (d LocalArchive) Name() string { return d.DestinationName }

func (d LocalArchive) Stage(ctx context.Context, sourceRoot, namespace string, item source.ObservedItem) (StagedDelivery, error) {
	if d.DestinationName == "" || d.Root == "" {
		return nil, errors.New("local destination name and root are required")
	}
	if _, err := safeNamespace(namespace); err != nil {
		return nil, err
	}
	if err := pathguard.RefuseInside(sourceRoot, d.Root); err != nil {
		return nil, err
	}
	if _, err := safeRelativePath(item.Path); err != nil {
		return nil, err
	}
	stagingDirectory := filepath.Join(d.Root, ".staging")
	if err := os.MkdirAll(stagingDirectory, 0o700); err != nil {
		return nil, err
	}
	lock, err := acquireLocalArchiveLock(ctx, d.Root)
	if err != nil {
		return nil, err
	}
	defer releaseLocalArchiveLock(lock)
	if err := reclaimStaleLocalStaging(stagingDirectory, time.Now()); err != nil {
		return nil, err
	}
	availableBytes := d.availableBytes
	if availableBytes == nil {
		availableBytes = filesystemAvailableBytes
	}
	required, err := requiredLocalCapacity(item.SizeBytes)
	if err != nil {
		return nil, err
	}
	release, err := reserveStageCapacity(stagingDirectory, required, availableBytes)
	if err != nil {
		return nil, fmt.Errorf("reserve local destination capacity for %s: %w", item.Path, err)
	}
	stage, err := newFileStageIn(stagingDirectory, ".djpi-*.tmp", func(ctx context.Context, temporaryPath, digest string) (Evidence, error) {
		lock, err := acquireLocalArchiveLock(ctx, d.Root)
		if err != nil {
			return Evidence{}, err
		}
		defer releaseLocalArchiveLock(lock)
		evidence := Evidence{SizeBytes: item.SizeBytes, ContentDigest: digest, VerificationDigest: digest}
		evidence.Location = contentLocation(namespace, digest, item.Path)
		destinationPath := filepath.Join(d.Root, filepath.FromSlash(evidence.Location))
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0o700); err != nil {
			return Evidence{}, err
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
	})
	if err != nil {
		release()
		return nil, err
	}
	stage.releaseReservation = release
	return stage, nil
}

func reclaimStaleLocalStaging(directory string, now time.Time) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("inspect local destination staging: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, ".djpi-") || !strings.HasSuffix(name, ".tmp") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refuse non-regular stale staging entry %s", name)
		}
		if now.Sub(info.ModTime()) < managedStageMaxAge {
			continue
		}
		if err := os.Remove(filepath.Join(directory, name)); err != nil {
			return err
		}
	}
	return nil
}

func (d LocalArchive) Deliver(ctx context.Context, sourceRoot, namespace string, item source.ObservedItem) (Evidence, error) {
	if d.DestinationName == "" {
		return Evidence{}, errors.New("local destination has no name")
	}
	if d.Root == "" {
		return Evidence{}, errors.New("local destination has no root")
	}
	if _, err := safeNamespace(namespace); err != nil {
		return Evidence{}, err
	}
	if err := pathguard.RefuseInside(sourceRoot, d.Root); err != nil {
		return Evidence{}, err
	}
	relative, err := safeRelativePath(item.Path)
	if err != nil {
		return Evidence{}, err
	}
	sourcePath := filepath.Join(sourceRoot, relative)

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
	stagingDirectory := filepath.Join(d.Root, ".staging")
	if err := os.MkdirAll(stagingDirectory, 0o700); err != nil {
		return Evidence{}, err
	}
	archiveLock, err := acquireLocalArchiveLock(ctx, d.Root)
	if err != nil {
		return Evidence{}, err
	}
	defer releaseLocalArchiveLock(archiveLock)
	if err := reclaimLocalStaging(stagingDirectory); err != nil {
		return Evidence{}, err
	}
	availableBytes := d.availableBytes
	if availableBytes == nil {
		availableBytes = filesystemAvailableBytes
	}
	available, err := availableBytes(stagingDirectory)
	if err != nil {
		return Evidence{}, fmt.Errorf("inspect local destination capacity: %w", err)
	}
	required, err := requiredLocalCapacity(item.SizeBytes)
	if err != nil {
		return Evidence{}, err
	}
	if available < required {
		return Evidence{}, fmt.Errorf("insufficient local destination capacity for %s: %d bytes available, %d required including headroom", item.Path, available, required)
	}
	temporary, err := os.CreateTemp(stagingDirectory, ".djpi-*.tmp")
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
	evidence.VerificationDigest = evidence.ContentDigest
	evidence.Location = contentLocation(namespace, evidence.ContentDigest, item.Path)
	destinationPath := filepath.Join(d.Root, filepath.FromSlash(evidence.Location))
	sourceStable, err := verifyPath(ctx, sourcePath, evidence)
	if err != nil {
		return Evidence{}, err
	}
	if !sourceStable {
		return Evidence{}, fmt.Errorf("source content changed while delivering %s", item.Path)
	}
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o700); err != nil {
		return Evidence{}, err
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

func filesystemAvailableBytes(path string) (uint64, error) {
	var statistics syscall.Statfs_t
	if err := syscall.Statfs(path, &statistics); err != nil {
		return 0, err
	}
	blocks, blockSize := uint64(statistics.Bavail), uint64(statistics.Bsize)
	if blockSize != 0 && blocks > math.MaxUint64/blockSize {
		return math.MaxUint64, nil
	}
	return blocks * blockSize, nil
}

func acquireLocalArchiveLock(ctx context.Context, root string) (*os.File, error) {
	lock, err := os.OpenFile(filepath.Join(root, ".djpi.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open local destination lock: %w", err)
	}
	retry := time.NewTicker(100 * time.Millisecond)
	defer retry.Stop()
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = lock.Close()
			return nil, fmt.Errorf("lock local destination: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = lock.Close()
			return nil, ctx.Err()
		case <-retry.C:
		}
	}
}

func releaseLocalArchiveLock(lock *os.File) {
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	_ = lock.Close()
}

func reclaimLocalStaging(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("inspect local destination staging: %w", err)
	}
	removed := false
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, ".djpi-") || !strings.HasSuffix(name, ".tmp") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect stale staging entry %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refuse non-regular stale staging entry %s", name)
		}
		if err := os.Remove(filepath.Join(directory, name)); err != nil {
			return fmt.Errorf("remove stale staging entry %s: %w", name, err)
		}
		removed = true
	}
	if removed {
		return syncDirectory(directory)
	}
	return nil
}

func requiredLocalCapacity(size int64) (uint64, error) {
	if size < 0 {
		return 0, errors.New("source item has negative size")
	}
	mediaBytes := uint64(size)
	headroom := mediaBytes / 100
	if headroom < minimumLocalHeadroom {
		headroom = minimumLocalHeadroom
	}
	if mediaBytes > math.MaxUint64-headroom {
		return 0, errors.New("source item size overflows local capacity calculation")
	}
	return mediaBytes + headroom, nil
}

func (d LocalArchive) Verify(ctx context.Context, evidence Evidence) (bool, error) {
	relative, err := safeRelativePath(evidence.Location)
	if err != nil {
		return false, err
	}
	return verifyPath(ctx, filepath.Join(d.Root, relative), evidence)
}

func safeNamespace(namespace string) (string, error) {
	if namespace == "" || namespace == "." || namespace == ".." || strings.ContainsAny(namespace, "/\\\x00") {
		return "", fmt.Errorf("unsafe destination namespace %q", namespace)
	}
	return namespace, nil
}

// locationDigestLength is how much of the SHA-256 hex is carried in the
// object name. A DJI Camera reuses names such as DJI_0001.MP4 after a format,
// so the name alone cannot be a key without risking a silent overwrite of
// different content. 12 hex characters keep that collision negligible while
// leaving the layout readable; the full digest stays in the Journal and in
// destination metadata, and verification always uses the full digest.
const locationDigestLength = 12

// contentLocation mirrors the Source layout so a Destination can be browsed
// like the Camera itself, and disambiguates same-named items by content.
//
//	<namespace>/DCIM/DJI_001/DJI_20260818190329_0020_D_006.fb527e791266.JPG
func contentLocation(namespace, digest, itemPath string) string {
	short := strings.TrimPrefix(digest, "sha256:")
	if len(short) > locationDigestLength {
		short = short[:locationDigestLength]
	}
	itemPath = filepath.ToSlash(itemPath)
	directory, name := path.Split(itemPath)
	extension := path.Ext(name)
	stem := strings.TrimSuffix(name, extension)
	return path.Join(namespace, directory, stem+"."+short+extension)
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
