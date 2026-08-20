package status

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"git.kuhree.com/kuhree.com/djpi/internal/journal"
	"git.kuhree.com/kuhree.com/djpi/internal/probe"
)

func TestBuildReportsReadinessOnlyForEveryConfiguredDestination(t *testing.T) {
	state := journal.EmptyState()
	state.Sources["source"] = journal.SourceRecord{Camera: "DJI Osmo Pocket 3", Items: map[string]journal.ItemRecord{
		"DCIM/clip.MP4": {Deliveries: map[string]journal.DeliveryRecord{
			"archive": {State: "verified", ContentDigest: "sha256:test", VerificationDigest: "sha256:test", Location: "stored"},
			"photos":  {State: "failed", Error: "offline"},
		}},
	}}
	report := Build(time.Unix(1, 0), []probe.SourceCandidate{{CameraModel: "DJI Osmo Pocket 3", BlockPath: "/dev/sdd"}}, state, []string{"photos", "archive"}, nil)
	if report.Journal.Sources != 1 || report.Journal.Items != 1 || report.Journal.VerifiedDeliveries != 1 || report.Journal.FailedDeliveries != 1 {
		t.Fatalf("journal summary = %#v", report.Journal)
	}
	if report.Journal.SourceSummaries[0].AllDestinationsReady || report.Cleanup.Authorized || report.Cleanup.Implemented {
		t.Fatalf("unsafe status = %#v", report)
	}
}

func TestWriteHumanAndJSONStatus(t *testing.T) {
	report := Build(time.Unix(1, 0), nil, journal.EmptyState(), []string{"archive"}, nil)
	var human bytes.Buffer
	if err := Write(&human, report, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(human.String(), "cleanup: disabled") {
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
