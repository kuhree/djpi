package station

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"git.kuhree.com/kuhree/djpi/internal/ingest"
	"git.kuhree.com/kuhree/djpi/internal/probe"
	sourceinventory "git.kuhree.com/kuhree/djpi/internal/source"
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
	observation  sourceinventory.Observation
	observations []sourceinventory.Observation
	err          error
	errors       []error
	calls        int
}

func (f *fakeInventor) Inventory(string) (sourceinventory.Observation, error) {
	f.calls++
	index := f.calls - 1
	err := f.err
	if len(f.errors) != 0 {
		errorIndex := index
		if errorIndex >= len(f.errors) {
			errorIndex = len(f.errors) - 1
		}
		err = f.errors[errorIndex]
	}
	if len(f.observations) != 0 {
		if index >= len(f.observations) {
			index = len(f.observations) - 1
		}
		return f.observations[index], err
	}
	return f.observation, err
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

type fakeIngester struct {
	result ingest.Result
	err    error
	calls  int
}

func (f *fakeIngester) Ingest(context.Context, string, string, sourceinventory.Observation) (ingest.Result, error) {
	f.calls++
	return f.result, f.err
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

func TestServiceIngestsAfterInventoryAndDoesNotRepeatCompletedAttachment(t *testing.T) {
	candidate := probe.SourceCandidate{CameraModel: "DJI Osmo Pocket 3", Mountpoint: "/camera", MountReadOnly: true}
	detector := &fakeDetector{values: []detection{{candidate: candidate, found: true}}}
	inventor := &fakeInventor{observation: sourceinventory.Observation{Complete: true, EvidenceID: "metadata-sha256:test", Summary: sourceinventory.ObservationSummary{Files: 2, Bytes: 42}}}
	ingester := &fakeIngester{result: ingest.Result{Verified: 2}}
	events := &memoryEvents{}
	service := NewService(detector, inventor, events).WithIngester(ingester)
	if err := service.Step(); err != nil {
		t.Fatal(err)
	}
	if err := service.Step(); err != nil {
		t.Fatal(err)
	}
	if inventor.calls != 2 || ingester.calls != 1 {
		t.Fatalf("calls inventor=%d ingester=%d", inventor.calls, ingester.calls)
	}
	if len(events.events) != 4 || events.events[2].Type != "ingest_started" || events.events[3].Type != "ingest_complete" || events.events[3].Verified != 2 {
		t.Fatalf("unexpected events: %#v", events.events)
	}
}

func TestServiceReconcilesSourceChangeBeforeCompletion(t *testing.T) {
	candidate := probe.SourceCandidate{CameraModel: "DJI Osmo Pocket 3", Mountpoint: "/camera", MountReadOnly: true}
	first := sourceinventory.Observation{Complete: true, EvidenceID: "metadata-sha256:first", Summary: sourceinventory.ObservationSummary{Files: 1, Bytes: 10}}
	second := sourceinventory.Observation{Complete: true, EvidenceID: "metadata-sha256:second", Summary: sourceinventory.ObservationSummary{Files: 2, Bytes: 20}}
	detector := &fakeDetector{values: []detection{{candidate: candidate, found: true}}}
	inventor := &fakeInventor{observations: []sourceinventory.Observation{first, second, second}}
	ingester := &fakeIngester{result: ingest.Result{Verified: 2}}
	events := &memoryEvents{}
	service := NewService(detector, inventor, events).WithIngester(ingester)
	if err := service.Step(); err != nil {
		t.Fatal(err)
	}
	if ingester.calls != 1 || events.events[len(events.events)-1].Type != "source_changed" || events.events[len(events.events)-1].EvidenceID != second.EvidenceID {
		t.Fatalf("first reconciliation: calls=%d events=%#v", ingester.calls, events.events)
	}
	if err := service.Step(); err != nil {
		t.Fatal(err)
	}
	if inventor.calls != 3 || ingester.calls != 2 || events.events[len(events.events)-1].Type != "ingest_complete" {
		t.Fatalf("completed reconciliation: inventor=%d ingester=%d events=%#v", inventor.calls, ingester.calls, events.events)
	}
}

func TestServiceRefusesCompletionWhenPostIngestInventoryIsIncomplete(t *testing.T) {
	candidate := probe.SourceCandidate{CameraModel: "DJI Osmo Pocket 3", Mountpoint: "/camera", MountReadOnly: true}
	complete := sourceinventory.Observation{Complete: true, EvidenceID: "metadata-sha256:complete"}
	incomplete := sourceinventory.Observation{Complete: false, Issues: []string{"DCIM: unavailable"}}
	detector := &fakeDetector{values: []detection{{candidate: candidate, found: true}}}
	inventor := &fakeInventor{observations: []sourceinventory.Observation{complete, incomplete}}
	events := &memoryEvents{}
	service := NewService(detector, inventor, events).WithIngester(&fakeIngester{})
	if err := service.Step(); err != nil {
		t.Fatal(err)
	}
	last := events.events[len(events.events)-1]
	if last.Type != "post_ingest_inventory_incomplete" || service.completedKey != "" || service.inventoriedKey != "" {
		t.Fatalf("unsafe completion state: event=%#v completed=%q inventoried=%q", last, service.completedKey, service.inventoriedKey)
	}
}

func TestServiceRefusesCompletionWhenPostIngestInventoryFails(t *testing.T) {
	candidate := probe.SourceCandidate{CameraModel: "DJI Osmo Pocket 3", Mountpoint: "/camera", MountReadOnly: true}
	complete := sourceinventory.Observation{Complete: true, EvidenceID: "metadata-sha256:complete"}
	detector := &fakeDetector{values: []detection{{candidate: candidate, found: true}}}
	inventor := &fakeInventor{observations: []sourceinventory.Observation{complete, {}}, errors: []error{nil, errors.New("camera disconnected")}}
	events := &memoryEvents{}
	service := NewService(detector, inventor, events).WithIngester(&fakeIngester{})
	if err := service.Step(); err != nil {
		t.Fatal(err)
	}
	last := events.events[len(events.events)-1]
	if last.Type != "post_ingest_inventory_failed" || !strings.Contains(last.Message, "camera disconnected") || service.completedKey != "" {
		t.Fatalf("unsafe completion state: event=%#v completed=%q", last, service.completedKey)
	}
}

func TestServiceBacksOffRepeatedIngestFailures(t *testing.T) {
	candidate := probe.SourceCandidate{CameraModel: "DJI Osmo Pocket 3", Mountpoint: "/camera", MountReadOnly: true}
	detector := &fakeDetector{values: []detection{{candidate: candidate, found: true}}}
	inventor := &fakeInventor{observation: sourceinventory.Observation{Complete: true, EvidenceID: "metadata-sha256:test"}}
	ingester := &fakeIngester{err: errors.New("destination unavailable")}
	events := &memoryEvents{}
	now := time.Unix(1_700_000_000, 0).UTC()
	service := NewService(detector, inventor, events).WithIngester(ingester)
	service.now = func() time.Time { return now }

	if err := service.Step(); err != nil {
		t.Fatal(err)
	}
	if ingester.calls != 1 || len(events.events) != 4 {
		t.Fatalf("first attempt: calls=%d events=%#v", ingester.calls, events.events)
	}
	firstRetry := now.Add(initialIngestRetry)
	if events.events[3].RetryAt == nil || !events.events[3].RetryAt.Equal(firstRetry) {
		t.Fatalf("first retry = %#v", events.events[3].RetryAt)
	}

	now = firstRetry.Add(-time.Nanosecond)
	if err := service.Step(); err != nil {
		t.Fatal(err)
	}
	if ingester.calls != 1 {
		t.Fatalf("ingest retried before deadline: %d", ingester.calls)
	}

	now = firstRetry
	if err := service.Step(); err != nil {
		t.Fatal(err)
	}
	if ingester.calls != 2 || events.events[len(events.events)-1].RetryAt == nil || !events.events[len(events.events)-1].RetryAt.Equal(now.Add(2*initialIngestRetry)) {
		t.Fatalf("second attempt: calls=%d events=%#v", ingester.calls, events.events)
	}
}

func TestIngestRetryDelayIsBounded(t *testing.T) {
	if got := ingestRetryDelay(0); got != 10*time.Second {
		t.Fatalf("first delay = %v", got)
	}
	if got := ingestRetryDelay(100); got != 5*time.Minute {
		t.Fatalf("maximum delay = %v", got)
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

func TestMultiEventSinkEmitsInOrder(t *testing.T) {
	first, second := &memoryEvents{}, &memoryEvents{}
	sinks := MultiEventSink{first, nil, second}
	event := Event{Type: "ingest_started"}
	if err := sinks.Emit(event); err != nil {
		t.Fatal(err)
	}
	if len(first.events) != 1 || len(second.events) != 1 || first.events[0].Type != event.Type || second.events[0].Type != event.Type {
		t.Fatalf("first=%#v second=%#v", first.events, second.events)
	}
}
