package status

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"git.kuhree.com/kuhree.com/djpi/internal/journal"
	"git.kuhree.com/kuhree.com/djpi/internal/probe"
)

func TestBuildReportsReadinessOnlyForEveryConfiguredDestination(t *testing.T) {
	verifiedAt := time.Unix(10, 0).UTC()
	state := journal.EmptyState()
	state.Sources["source"] = journal.SourceRecord{Camera: "DJI Osmo Pocket 3", Items: map[string]journal.ItemRecord{
		"DCIM/clip.MP4": {Deliveries: map[string]journal.DeliveryRecord{
			"archive": {State: "verified", ContentDigest: "sha256:test", VerificationDigest: "sha256:test", Location: "stored", VerifiedAt: verifiedAt},
			"photos":  {State: "failed", Error: "offline"},
		}},
	}}
	report := Build(time.Unix(1, 0), []probe.SourceCandidate{{CameraModel: "DJI Osmo Pocket 3", BlockPath: "/dev/sdd"}}, state, []string{"photos", "archive"}, nil)
	if report.Journal.Sources != 1 || report.Journal.Items != 1 || report.Journal.VerifiedDeliveries != 1 || report.Journal.FailedDeliveries != 1 || report.Journal.PendingDeliveries != 1 {
		t.Fatalf("journal summary = %#v", report.Journal)
	}
	if report.Journal.LastVerifiedAt == nil || !report.Journal.LastVerifiedAt.Equal(verifiedAt) || len(report.Journal.Failures) != 1 || report.Journal.Failures[0].ItemPath != "DCIM/clip.MP4" {
		t.Fatalf("journal diagnostics = %#v", report.Journal)
	}
	if report.Journal.SourceSummaries[0].AllDestinationsReady || report.Cleanup.Authorized || report.Cleanup.Implemented {
		t.Fatalf("unsafe status = %#v", report)
	}
}

func TestBuildBoundsAndSortsFailureDetails(t *testing.T) {
	state := journal.EmptyState()
	items := make(map[string]journal.ItemRecord)
	for index := 24; index >= 0; index-- {
		path := fmt.Sprintf("DCIM/%02d.MP4", index)
		items[path] = journal.ItemRecord{Deliveries: map[string]journal.DeliveryRecord{"archive": {State: "failed", Error: "offline"}}}
	}
	state.Sources["source"] = journal.SourceRecord{Camera: "camera", Items: items}
	report := Build(time.Unix(1, 0), nil, state, []string{"archive"}, nil)
	if len(report.Journal.Failures) != maximumReportedFailures || report.Journal.PendingDeliveries != 25 || report.Journal.FailedDeliveries != 25 {
		t.Fatalf("journal summary = %#v", report.Journal)
	}
	if report.Journal.Failures[0].ItemPath != "DCIM/00.MP4" || report.Journal.Failures[19].ItemPath != "DCIM/19.MP4" {
		t.Fatalf("failure ordering = %#v", report.Journal.Failures)
	}
}

func TestWriteHumanAndJSONStatus(t *testing.T) {
	report := Build(time.Unix(1, 0), nil, journal.EmptyState(), []string{"archive"}, nil)
	var human bytes.Buffer
	if err := Write(&human, report, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(human.String(), "cleanup: disabled") || !strings.Contains(human.String(), "pending") {
		t.Fatalf("human output = %q", human.String())
	}
	var machine bytes.Buffer
	if err := Write(&machine, report, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(machine.String(), `"authorized": false`) {
		t.Fatalf("JSON output = %q", machine.String())
	}
}
