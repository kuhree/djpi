package audit

import (
	"encoding/json"
	"fmt"
	"io"
)

func Write(writer io.Writer, report Report, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	if _, err := fmt.Fprintf(writer, "djpi destination audit\n%s\n", report.Summary()); err != nil {
		return err
	}
	for _, problem := range report.Problems {
		if _, err := fmt.Fprintf(writer, "  %s %s at %s: %s\n", problem.State, problem.ItemPath, problem.Destination, problem.Message); err != nil {
			return err
		}
	}
	return nil
}
