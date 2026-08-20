package main

import (
	"fmt"
	"os"

	"git.kuhree.com/kuhree.com/djpi/internal/probe"
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

	fmt.Fprintln(os.Stderr, "djpi is not configured yet")
	fmt.Fprintln(os.Stderr, "usage: djpi <version|probe [--json]>")
	return 2
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
