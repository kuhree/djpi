package station

import (
	"errors"
	"strings"
	"testing"
	"time"

	"git.kuhree.com/kuhree.com/djpi/internal/probe"
	sourceinventory "git.kuhree.com/kuhree.com/djpi/internal/source"
)

type detection struct {
	candidate probe.SourceCandidate
	found     bool
	err       error
}
type fakeDetector struct {
	values []detection
	index  int
}

func (f *fakeDetector) Detect() (probe.SourceCandidate, bool, error) {
	value := f.values[f.index]
	if f.index < len(f.values)-1 {
		f.index++
	}
	return value.candidate, value.found, value.err
}

type fakeInventor struct {
	observation sourceinventory.Observation
	err         error
	calls       int
}

func (f *fakeInventor) Inventory(string) (sourceinventory.Observation, error) {
	f.calls++
	return f.observation, f.err
}

type memoryEvents struct{ events []Event }

func (m *memoryEvents) Emit(event Event) error { m.events = append(m.events, event); return nil }

func TestServiceInventoriesOncePerAttachmentAndResetsOnRemoval(t *testing.T) {
	candidate := probe.SourceCandidate{CameraModel: "DJI Osmo Pocket 3", USBDevice: "7-1.4", BlockPath: "/dev/sdd", Mountpoint: "/camera"}
	detector := &fakeDetector{values: []detection{{}, {candidate: candidate, found: true}, {candidate: candidate, found: true}, {}}}
	inventor := &fakeInventor{observation: sourceinventory.Observation{Complete: true, EvidenceID: "metadata-sha256:test", Summary: sourceinventory.ObservationSummary{Files: 9, Bytes: 42}}}
	events := &memoryEvents{}
	service := NewService(detector, inventor, events)
	service.now = func() time.Time { return time.Unix(1, 0).UTC() }
	for range 4 {
		if err := service.Step(); err != nil {
			t.Fatal(err)
		}
	}
	if inventor.calls != 1 {
		t.Fatalf("expected one inventory, got %d", inventor.calls)
	}
	want := []string{"source_detected", "inventory_complete", "source_removed"}
	if len(events.events) != len(want) {
		t.Fatalf("unexpected events: %#v", events.events)
	}
	for index, eventType := range want {
		if events.events[index].Type != eventType {
			t.Fatalf("event %d = %q, want %q", index, events.events[index].Type, eventType)
		}
	}
	if events.events[1].Files != 9 || events.events[1].EvidenceID == "" {
		t.Fatalf("missing inventory evidence: %#v", events.events[1])
	}
	if !strings.Contains(events.events[0].Message, "mounted read-write") {
		t.Fatalf("expected read-write mount warning: %#v", events.events[0])
	}
}

func TestServiceRetriesInventoryAndDeduplicatesProblemEvent(t *testing.T) {
	candidate := probe.SourceCandidate{CameraModel: "DJI Osmo Action 6", Mountpoint: "/camera"}
	detector := &fakeDetector{values: []detection{{candidate: candidate, found: true}}}
	inventor := &fakeInventor{err: errors.New("camera unavailable")}
	events := &memoryEvents{}
	service := NewService(detector, inventor, events)
	if err := service.Step(); err != nil {
		t.Fatal(err)
	}
	if err := service.Step(); err != nil {
		t.Fatal(err)
	}
	if inventor.calls != 2 {
		t.Fatalf("expected retry, got %d calls", inventor.calls)
	}
	if len(events.events) != 2 || events.events[0].Type != "source_detected" || events.events[1].Type != "inventory_failed" {
		t.Fatalf("unexpected deduplicated events: %#v", events.events)
	}
}

func TestServiceReportsBlockedDetectionWithoutInventoring(t *testing.T) {
	detector := &fakeDetector{values: []detection{{err: errors.New("multiple mounted camera sources are ambiguous")}}}
	inventor := &fakeInventor{}
	events := &memoryEvents{}
	service := NewService(detector, inventor, events)
	if err := service.Step(); err != nil {
		t.Fatal(err)
	}
	if inventor.calls != 0 || len(events.events) != 1 || events.events[0].Type != "source_blocked" {
		t.Fatalf("unexpected blocked state: calls=%d events=%#v", inventor.calls, events.events)
	}
}

func TestJSONEventSinkWritesOneJSONObjectPerLine(t *testing.T) {
	var output strings.Builder
	sink := NewJSONEventSink(&output)
	if err := sink.Emit(Event{Time: time.Unix(1, 0).UTC(), Type: "source_detected"}); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(output.String(), "\n"); lines != 1 {
		t.Fatalf("expected one JSON line, got %q", output.String())
	}
}
