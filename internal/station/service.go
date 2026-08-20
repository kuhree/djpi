// Package station coordinates the always-running, non-destructive camera station.
package station

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"git.kuhree.com/kuhree.com/djpi/internal/probe"
	sourceinventory "git.kuhree.com/kuhree.com/djpi/internal/source"
)

type Detector interface {
	Detect() (probe.SourceCandidate, bool, error)
}

type Inventor interface {
	Inventory(string) (sourceinventory.Observation, error)
}

type EventSink interface {
	Emit(Event) error
}

type Event struct {
	Time       time.Time `json:"time"`
	Type       string    `json:"type"`
	Camera     string    `json:"camera,omitempty"`
	BlockPath  string    `json:"block_path,omitempty"`
	SourcePath string    `json:"source_path,omitempty"`
	EvidenceID string    `json:"metadata_evidence_id,omitempty"`
	Files      int       `json:"files,omitempty"`
	Bytes      int64     `json:"bytes,omitempty"`
	Message    string    `json:"message,omitempty"`
}

type Service struct {
	detector Detector
	inventor Inventor
	events   EventSink
	now      func() time.Time

	currentKey   string
	completedKey string
	lastProblem  string
}

func NewService(detector Detector, inventor Inventor, events EventSink) *Service {
	return &Service{detector: detector, inventor: inventor, events: events, now: time.Now}
}

// Step performs one observation cycle. Detection and inventory failures are
// emitted as state and retried later; only event-output failures are returned.
func (s *Service) Step() error {
	candidate, found, err := s.detector.Detect()
	if err != nil {
		return s.problem("source_blocked", err.Error(), candidate)
	}
	if !found {
		s.lastProblem = ""
		if s.currentKey == "" {
			return nil
		}
		event := Event{Time: s.now(), Type: "source_removed"}
		s.currentKey, s.completedKey = "", ""
		return s.events.Emit(event)
	}

	key := sourceKey(candidate)
	if key != s.currentKey {
		s.currentKey, s.completedKey, s.lastProblem = key, "", ""
		event := candidateEvent(s.now(), "source_detected", candidate)
		if !candidate.MountReadOnly {
			event.Message = "source is mounted read-write; djpi station operations remain read-only"
		}
		if err := s.events.Emit(event); err != nil {
			return err
		}
	}
	if s.completedKey == key {
		return nil
	}

	observation, err := s.inventor.Inventory(candidate.Mountpoint)
	if err != nil {
		return s.problem("inventory_failed", err.Error(), candidate)
	}
	if !observation.Complete {
		message := "source inventory is incomplete"
		if len(observation.Issues) != 0 {
			message = fmt.Sprintf("%s: %v", message, observation.Issues)
		}
		return s.problem("inventory_incomplete", message, candidate)
	}

	s.lastProblem = ""
	s.completedKey = key
	event := candidateEvent(s.now(), "inventory_complete", candidate)
	event.EvidenceID = observation.EvidenceID
	event.Files = observation.Summary.Files
	event.Bytes = observation.Summary.Bytes
	return s.events.Emit(event)
}

func (s *Service) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("poll interval must be positive")
	}
	if err := s.Step(); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.Step(); err != nil {
				return err
			}
		}
	}
}

func (s *Service) problem(eventType, message string, candidate probe.SourceCandidate) error {
	problemKey := eventType + "\x00" + message
	if problemKey == s.lastProblem {
		return nil
	}
	s.lastProblem = problemKey
	event := candidateEvent(s.now(), eventType, candidate)
	event.Message = message
	return s.events.Emit(event)
}

func sourceKey(candidate probe.SourceCandidate) string {
	return candidate.CameraModel + "\x00" + candidate.USBDevice + "\x00" + candidate.BlockPath + "\x00" + candidate.Mountpoint
}

func candidateEvent(at time.Time, eventType string, candidate probe.SourceCandidate) Event {
	return Event{Time: at, Type: eventType, Camera: candidate.CameraModel, BlockPath: candidate.BlockPath, SourcePath: candidate.Mountpoint}
}

type SystemDetector struct{}

func (SystemDetector) Detect() (probe.SourceCandidate, bool, error) {
	report := probe.NewSystemCollector().Collect()
	if len(report.SourceCandidates) == 0 {
		return probe.SourceCandidate{}, false, nil
	}
	candidate, err := probe.SingleMountedSource(report.SourceCandidates)
	return candidate, err == nil, err
}

type SystemInventor struct{}

func (SystemInventor) Inventory(path string) (sourceinventory.Observation, error) {
	return sourceinventory.InventoryPath(path)
}

type JSONEventSink struct {
	writer io.Writer
	mu     sync.Mutex
}

func NewJSONEventSink(writer io.Writer) *JSONEventSink { return &JSONEventSink{writer: writer} }

func (s *JSONEventSink) Emit(event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.NewEncoder(s.writer).Encode(event)
}
