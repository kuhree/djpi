package main

import (
	"flag"
	"fmt"
	"os"

	"git.kuhree.com/kuhree.com/djpi/internal/probe"
	sourceinventory "git.kuhree.com/kuhree.com/djpi/internal/source"
)

const version = "0.0.0-dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 1 && args[0] == "version" {
		fmt.Println(version)
		return 0
	}
	if len(args) >= 1 && args[0] == "probe" {
		return runProbe(args[1:])
	}
	if len(args) >= 1 && args[0] == "inventory" {
		return runInventory(args[1:])
	}

	fmt.Fprintln(os.Stderr, "djpi is not configured yet")
	fmt.Fprintln(os.Stderr, "usage: djpi <version|probe [--json]|inventory [--source PATH] [--json]>")
	return 2
}

func runInventory(args []string) int {
	flags := flag.NewFlagSet("inventory", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	sourcePath := flags.String("source", "", "existing mounted source directory")
	jsonOutput := flags.Bool("json", false, "write machine-readable JSON")
	flags.Usage = func() { fmt.Fprintln(os.Stderr, "usage: djpi inventory [--source PATH] [--json]") }
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		flags.Usage()
		return 2
	}
	if *sourcePath == "" {
		report := probe.NewSystemCollector().Collect()
		candidate, err := probe.SingleMountedSource(report.SourceCandidates)
		if err != nil {
			fmt.Fprintf(os.Stderr, "auto-discover source: %v\n", err)
			return 1
		}
		*sourcePath = candidate.Mountpoint
		if !candidate.MountReadOnly {
			fmt.Fprintf(os.Stderr, "warning: auto-discovered source %s is mounted read-write; djpi will only read it\n", candidate.Mountpoint)
		}
	}

	observation, err := sourceinventory.InventoryPath(*sourcePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "inventory source: %v\n", err)
		return 1
	}
	if err := sourceinventory.WriteObservation(os.Stdout, observation, *jsonOutput); err != nil {
		fmt.Fprintf(os.Stderr, "write source inventory: %v\n", err)
		return 1
	}
	if !observation.Complete {
		return 1
	}
	return 0
}

func runProbe(args []string) int {
	jsonOutput := false
	if len(args) == 1 && args[0] == "--json" {
		jsonOutput = true
	} else if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: djpi probe [--json]")
		return 2
	}

	report := probe.NewSystemCollector().Collect()
	if err := probe.WriteReport(os.Stdout, report, jsonOutput); err != nil {
		fmt.Fprintf(os.Stderr, "write probe report: %v\n", err)
		return 1
	}
	return 0
}
