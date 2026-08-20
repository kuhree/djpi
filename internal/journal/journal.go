// Package journal persists ingest evidence independently from camera storage.
package journal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const CurrentVersion = 1

type State struct {
	Version int                     `json:"version"`
	Sources map[string]SourceRecord `json:"sources"`
}

type SourceRecord struct {
	Camera             string                `json:"camera"`
	MetadataEvidenceID string                `json:"metadata_evidence_id"`
	Items              map[string]ItemRecord `json:"items"`
}

type ItemRecord struct {
	SizeBytes         int64                     `json:"size_bytes"`
	ModifiedUnixNanos int64                     `json:"modified_unix_nanos"`
	ContentDigest     string                    `json:"content_digest,omitempty"`
	Deliveries        map[string]DeliveryRecord `json:"deliveries"`
}

type DeliveryRecord struct {
	State         string    `json:"state"`
	SizeBytes     int64     `json:"size_bytes"`
	ContentDigest string    `json:"content_digest,omitempty"`
	VerifiedAt    time.Time `json:"verified_at,omitempty"`
	Error         string    `json:"error,omitempty"`
}

func EmptyState() State {
	return State{Version: CurrentVersion, Sources: make(map[string]SourceRecord)}
}

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(path string) *Store { return &Store{path: path} }

func (s *Store) Load() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Update serializes read-modify-write operations and replaces the journal
// atomically only after the complete new state is synced.
func (s *Store) Update(change func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.load()
	if err != nil {
		return err
	}
	if err := change(&state); err != nil {
		return err
	}
	return s.save(state)
}

func (s *Store) load() (State, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return EmptyState(), nil
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("decode ingest journal: %w", err)
	}
	if state.Version != CurrentVersion {
		return State{}, fmt.Errorf("unsupported ingest journal version %d", state.Version)
	}
	if state.Sources == nil {
		state.Sources = make(map[string]SourceRecord)
	}
	return state, nil
}

func (s *Store) save(state State) error {
	state.Version = CurrentVersion
	if state.Sources == nil {
		state.Sources = make(map[string]SourceRecord)
	}
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".journal-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(state); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return err
	}
	cleanup = false
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
