// Package status builds a read-only operational view from live probe evidence
// and the durable ingest journal.
package status

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"git.kuhree.com/kuhree.com/djpi/internal/journal"
	"git.kuhree.com/kuhree.com/djpi/internal/probe"
)

type Report struct {
	Time                   time.Time               `json:"time"`
	AttachedSources        []probe.SourceCandidate `json:"attached_sources"`
	ConfiguredDestinations []string                `json:"configured_destinations"`
	Journal                JournalSummary          `json:"journal"`
	Cleanup                CleanupStatus           `json:"cleanup"`
	Warnings               []string                `json:"warnings,omitempty"`
}

type JournalSummary struct {
	Sources            int             `json:"sources"`
	Items              int             `json:"items"`
	VerifiedDeliveries int             `json:"verified_deliveries"`
	FailedDeliveries   int             `json:"failed_deliveries"`
	SourceSummaries    []SourceSummary `json:"source_summaries,omitempty"`
}

type SourceSummary struct {
	SourceID             string `json:"source_id"`
	Camera               string `json:"camera"`
	Items                int    `json:"items"`
	VerifiedDeliveries   int    `json:"verified_deliveries"`
	FailedDeliveries     int    `json:"failed_deliveries"`
	AllDestinationsReady bool   `json:"all_destinations_ready"`
}

type CleanupStatus struct {
	Implemented bool   `json:"implemented"`
	Authorized  bool   `json:"authorized"`
	Reason      string `json:"reason"`
}

func Build(at time.Time, candidates []probe.SourceCandidate, state journal.State, destinationNames []string, warnings []string) Report {
	names := append([]string(nil), destinationNames...)
	sort.Strings(names)
	report := Report{
		Time:                   at.UTC(),
		AttachedSources:        append(make([]probe.SourceCandidate, 0, len(candidates)), candidates...),
		ConfiguredDestinations: names,
		Cleanup: CleanupStatus{
			Implemented: false,
			Authorized:  false,
			Reason:      "cleanup is not implemented; verified delivery is not deletion or format authorization",
		},
		Warnings: append([]string(nil), warnings...),
	}
	sort.Slice(report.AttachedSources, func(i, j int) bool {
		return report.AttachedSources[i].BlockPath < report.AttachedSources[j].BlockPath
	})
	sort.Strings(report.Warnings)
	sourceIDs := make([]string, 0, len(state.Sources))
	for sourceID := range state.Sources {
		sourceIDs = append(sourceIDs, sourceID)
	}
	sort.Strings(sourceIDs)
	for _, sourceID := range sourceIDs {
		record := state.Sources[sourceID]
		summary := SourceSummary{SourceID: sourceID, Camera: record.Camera, Items: len(record.Items), AllDestinationsReady: len(record.Items) != 0 && len(names) != 0}
		for _, item := range record.Items {
			for _, delivery := range item.Deliveries {
				switch delivery.State {
				case "verified":
					summary.VerifiedDeliveries++
				case "failed":
					summary.FailedDeliveries++
				}
			}
			for _, name := range names {
				delivery, ok := item.Deliveries[name]
				if !ok || delivery.State != "verified" || delivery.ContentDigest == "" || delivery.VerificationDigest == "" || delivery.Location == "" {
					summary.AllDestinationsReady = false
				}
			}
		}
		report.Journal.Items += summary.Items
		report.Journal.VerifiedDeliveries += summary.VerifiedDeliveries
		report.Journal.FailedDeliveries += summary.FailedDeliveries
		report.Journal.SourceSummaries = append(report.Journal.SourceSummaries, summary)
	}
	report.Journal.Sources = len(report.Journal.SourceSummaries)
	return report
}

func Write(writer io.Writer, report Report, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	_, err := fmt.Fprintf(writer, "djpi station status\nattached sources: %d\njournal: %d source snapshot(s), %d item(s), %d verified delivery record(s), %d failed delivery record(s)\n",
		len(report.AttachedSources), report.Journal.Sources, report.Journal.Items, report.Journal.VerifiedDeliveries, report.Journal.FailedDeliveries)
	if err != nil {
		return err
	}
	for _, candidate := range report.AttachedSources {
		if _, err := fmt.Fprintf(writer, "  %s block=%s mount=%s read-only=%t\n", candidate.CameraModel, candidate.BlockPath, candidate.Mountpoint, candidate.MountReadOnly); err != nil {
			return err
		}
	}
	for _, source := range report.Journal.SourceSummaries {
		if _, err := fmt.Fprintf(writer, "  %s items=%d verified=%d failed=%d all-configured-ready=%t\n", source.Camera, source.Items, source.VerifiedDeliveries, source.FailedDeliveries, source.AllDestinationsReady); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(writer, "cleanup: disabled (%s)\n", report.Cleanup.Reason)
	return err
}
