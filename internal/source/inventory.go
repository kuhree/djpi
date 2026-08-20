// Package source inventories camera storage without modifying or opening media.
package source

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Observation is a read-only metadata inventory. Its EvidenceID is not a
// content digest and must never be used as cleanup verification.
type Observation struct {
	SourcePath    string             `json:"source_path"`
	Complete      bool               `json:"complete"`
	EvidenceID    string             `json:"metadata_evidence_id,omitempty"`
	IdentityBasis string             `json:"identity_basis"`
	Items         []ObservedItem     `json:"items"`
	Summary       ObservationSummary `json:"summary"`
	Issues        []string           `json:"issues,omitempty"`
}

type ObservedItem struct {
	Path              string `json:"path"`
	SizeBytes         int64  `json:"size_bytes"`
	ModifiedUnixNanos int64  `json:"modified_unix_nanos"`
	Extension         string `json:"extension,omitempty"`
}

type ObservationSummary struct {
	Files      int                `json:"files"`
	Bytes      int64              `json:"bytes"`
	Extensions []ExtensionSummary `json:"extensions"`
}

type ExtensionSummary struct {
	Extension string `json:"extension"`
	Files     int    `json:"files"`
	Bytes     int64  `json:"bytes"`
}

const identityBasis = "relative path, size, and modification time; file content is not read or verified"

// InventoryPath observes an existing directory. It never creates or mounts a
// filesystem and delegates traversal to InventoryFS for testability.
func InventoryPath(path string) (Observation, error) {
	cleaned := filepath.Clean(path)
	info, err := os.Stat(cleaned)
	if err != nil {
		return Observation{}, err
	}
	if !info.IsDir() {
		return Observation{}, fmt.Errorf("source path is not a directory: %s", cleaned)
	}
	return InventoryFS(cleaned, os.DirFS(cleaned)), nil
}

// InventoryFS observes regular-file metadata without opening file contents.
// Symlinks and other special entries make the observation incomplete.
func InventoryFS(sourcePath string, sourceFS fs.FS) Observation {
	observation := Observation{
		SourcePath:    sourcePath,
		Complete:      true,
		IdentityBasis: identityBasis,
		Items:         make([]ObservedItem, 0),
	}

	err := fs.WalkDir(sourceFS, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			observation.Complete = false
			observation.Issues = append(observation.Issues, fmt.Sprintf("%s: %v", displayPath(path), walkErr))
			return nil
		}
		if path == "." || entry.IsDir() {
			return nil
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			observation.Complete = false
			observation.Issues = append(observation.Issues, fmt.Sprintf("%s: read metadata: %v", displayPath(path), infoErr))
			return nil
		}
		if !info.Mode().IsRegular() {
			observation.Complete = false
			observation.Issues = append(observation.Issues, fmt.Sprintf("%s: unsupported source entry type %s", displayPath(path), info.Mode().Type()))
			return nil
		}

		relativePath := filepath.ToSlash(path)
		observation.Items = append(observation.Items, ObservedItem{
			Path:              relativePath,
			SizeBytes:         info.Size(),
			ModifiedUnixNanos: info.ModTime().UnixNano(),
			Extension:         normalizedExtension(relativePath),
		})
		return nil
	})
	if err != nil {
		observation.Complete = false
		observation.Issues = append(observation.Issues, err.Error())
	}

	sort.Slice(observation.Items, func(i, j int) bool { return observation.Items[i].Path < observation.Items[j].Path })
	sort.Strings(observation.Issues)
	observation.Summary = summarize(observation.Items)
	if observation.Complete {
		observation.EvidenceID = metadataEvidenceID(observation.Items)
	}
	return observation
}

func summarize(items []ObservedItem) ObservationSummary {
	type aggregate struct {
		files int
		bytes int64
	}
	byExtension := make(map[string]aggregate)
	summary := ObservationSummary{Files: len(items), Extensions: make([]ExtensionSummary, 0)}
	for _, item := range items {
		summary.Bytes += item.SizeBytes
		extension := item.Extension
		if extension == "" {
			extension = "<none>"
		}
		value := byExtension[extension]
		value.files++
		value.bytes += item.SizeBytes
		byExtension[extension] = value
	}
	for extension, value := range byExtension {
		summary.Extensions = append(summary.Extensions, ExtensionSummary{Extension: extension, Files: value.files, Bytes: value.bytes})
	}
	sort.Slice(summary.Extensions, func(i, j int) bool { return summary.Extensions[i].Extension < summary.Extensions[j].Extension })
	return summary
}

func metadataEvidenceID(items []ObservedItem) string {
	digest := sha256.New()
	for _, item := range items {
		_, _ = fmt.Fprintf(digest, "%d:%s\x00%d\x00%d\n", len(item.Path), item.Path, item.SizeBytes, item.ModifiedUnixNanos)
	}
	return fmt.Sprintf("metadata-sha256:%x", digest.Sum(nil))
}

func normalizedExtension(path string) string {
	extension := strings.TrimPrefix(filepath.Ext(path), ".")
	return strings.ToLower(extension)
}

func displayPath(path string) string {
	if path == "." {
		return "<source root>"
	}
	return filepath.ToSlash(path)
}
