package source

import (
	"encoding/json"
	"fmt"
	"io"
)

func WriteObservation(w io.Writer, observation Observation, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(observation)
	}

	if _, err := fmt.Fprintf(w, "djpi read-only source inventory\nSource: %s\nComplete: %t\n", observation.SourcePath, observation.Complete); err != nil {
		return err
	}
	if observation.EvidenceID != "" {
		if _, err := fmt.Fprintf(w, "Metadata evidence ID: %s\n", observation.EvidenceID); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "Identity basis: %s\n", observation.IdentityBasis); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "This inventory is not content verification and cannot authorize cleanup."); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Files: %d\nBytes: %d\n", observation.Summary.Files, observation.Summary.Bytes); err != nil {
		return err
	}
	for _, extension := range observation.Summary.Extensions {
		if _, err := fmt.Fprintf(w, "  extension=%s files=%d bytes=%d\n", extension.Extension, extension.Files, extension.Bytes); err != nil {
			return err
		}
	}
	if len(observation.Issues) != 0 {
		if _, err := fmt.Fprintln(w, "Issues:"); err != nil {
			return err
		}
		for _, issue := range observation.Issues {
			if _, err := fmt.Fprintf(w, "  %s\n", issue); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintln(w, "Items:"); err != nil {
		return err
	}
	for _, item := range observation.Items {
		if _, err := fmt.Fprintf(w, "  %s size=%d modified_unix_nanos=%d\n", item.Path, item.SizeBytes, item.ModifiedUnixNanos); err != nil {
			return err
		}
	}
	return nil
}
