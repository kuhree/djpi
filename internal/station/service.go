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

	"git.kuhree.com/kuhree.com/djpi/internal/ingest"
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

// MultiEventSink sends each event to every sink in order.
type MultiEventSink []EventSink

func (sinks MultiEventSink) Emit(event Event) error {
	for _, sink := range sinks {
		if sink == nil {
			continue
		}
		if err := sink.Emit(event); err != nil {
			return err
		}
	}
	return nil
}

type Event struct {
	Time       time.Time  `json:"time"`
	Type       string     `json:"type"`
	Camera     string     `json:"camera,omitempty"`
	BlockPath  string     `json:"block_path,omitempty"`
	SourcePath string     `json:"source_path,omitempty"`
	EvidenceID string     `json:"metadata_evidence_id,omitempty"`
	Files      int        `json:"files,omitempty"`
	Bytes      int64      `json:"bytes,omitempty"`
	Verified   int        `json:"verified,omitempty"`
	Skipped    int        `json:"skipped,omitempty"`
	Failed     int        `json:"failed,omitempty"`
	RetryAt    *time.Time `json:"retry_at,omitempty"`
	Message    string     `json:"message,omitempty"`
}

type Ingester interface {
	Ingest(context.Context, string, string, sourceinventory.Observation) (ingest.Result, error)
}

type Service struct {
	detector Detector
	inventor Inventor
	events   EventSink
	ingester Ingester
	now      func() time.Time

	currentKey     string
	completedKey   string
	inventoriedKey string
	observation    sourceinventory.Observation
	lastProblem    string
	retryFailures  uint
	nextIngestTry  time.Time
}

const (
	initialIngestRetry = 10 * time.Second
	maximumIngestRetry = 5 * time.Minute
)

func (s *Service) WithIngester(ingester Ingester) *Service { s.ingester = ingester; return s }

func NewService(detector Detector, inventor Inventor, events EventSink) *Service {
	return &Service{detector: detector, inventor: inventor, events: events, now: time.Now}
}

// Step performs one observation cycle. Detection and inventory failures are
// emitted as state and retried later; only event-output failures are returned.
func (s *Service) Step() error { return s.StepContext(context.Background()) }

func (s *Service) StepContext(ctx context.Context) error {
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
		s.currentKey, s.completedKey, s.inventoriedKey = "", "", ""
		s.retryFailures, s.nextIngestTry = 0, time.Time{}
		s.observation = sourceinventory.Observation{}
		return s.events.Emit(event)
	}

	key := sourceKey(candidate)
	if key != s.currentKey {
		s.currentKey, s.completedKey, s.inventoriedKey, s.lastProblem = key, "", "", ""
		s.retryFailures, s.nextIngestTry = 0, time.Time{}
		s.observation = sourceinventory.Observation{}
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

	if s.inventoriedKey != key {
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
		s.inventoriedKey, s.observation = key, observation
		event := candidateEvent(s.now(), "inventory_complete", candidate)
		event.EvidenceID, event.Files, event.Bytes = observation.EvidenceID, observation.Summary.Files, observation.Summary.Bytes
		if err := s.events.Emit(event); err != nil {
			return err
		}
	}
	if s.ingester == nil {
		s.completedKey = key
		return nil
	}
	if !s.nextIngestTry.IsZero() && s.now().Before(s.nextIngestTry) {
		return nil
	}
	started := candidateEvent(s.now(), "ingest_started", candidate)
	started.EvidenceID = s.observation.EvidenceID
	started.Files, started.Bytes = s.observation.Summary.Files, s.observation.Summary.Bytes
	if err := s.events.Emit(started); err != nil {
		return err
	}
	cameraIdentity := candidate.CameraID
	if cameraIdentity == "" {
		cameraIdentity = candidate.CameraModel
	}
	result, err := s.ingester.Ingest(ctx, cameraIdentity, candidate.Mountpoint, s.observation)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return s.ingestFailed(fmt.Sprintf("%v: %v", err, result.Failures), candidate, result)
	}
	s.lastProblem, s.completedKey = "", key
	s.retryFailures, s.nextIngestTry = 0, time.Time{}
	event := candidateEvent(s.now(), "ingest_complete", candidate)
	event.EvidenceID, event.Files, event.Bytes = s.observation.EvidenceID, s.observation.Summary.Files, s.observation.Summary.Bytes
	event.Verified, event.Skipped, event.Failed = result.Verified, result.Skipped, result.Failed
	return s.events.Emit(event)
}

func (s *Service) ingestFailed(message string, candidate probe.SourceCandidate, result ingest.Result) error {
	delay := ingestRetryDelay(s.retryFailures)
	s.retryFailures++
	retryAt := s.now().UTC().Add(delay)
	s.nextIngestTry = retryAt
	event := candidateEvent(s.now(), "ingest_failed", candidate)
	event.EvidenceID, event.Files, event.Bytes = s.observation.EvidenceID, s.observation.Summary.Files, s.observation.Summary.Bytes
	event.Verified, event.Skipped, event.Failed = result.Verified, result.Skipped, result.Failed
	event.Message = message
	event.RetryAt = &retryAt
	return s.events.Emit(event)
}

func ingestRetryDelay(failures uint) time.Duration {
	delay := initialIngestRetry
	for count := uint(0); count < failures && delay < maximumIngestRetry; count++ {
		if delay > maximumIngestRetry/2 {
			return maximumIngestRetry
		}
		delay *= 2
	}
	if delay > maximumIngestRetry {
		return maximumIngestRetry
	}
	return delay
}

func (s *Service) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("poll interval must be positive")
	}
	if err := s.StepContext(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.StepContext(ctx); err != nil {
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
