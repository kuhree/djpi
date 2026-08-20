package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"git.kuhree.com/kuhree.com/djpi/internal/config"
	"git.kuhree.com/kuhree.com/djpi/internal/destination"
	"git.kuhree.com/kuhree.com/djpi/internal/ingest"
	"git.kuhree.com/kuhree.com/djpi/internal/journal"
	"git.kuhree.com/kuhree.com/djpi/internal/operatorhttp"
	"git.kuhree.com/kuhree.com/djpi/internal/probe"
	sourceinventory "git.kuhree.com/kuhree.com/djpi/internal/source"
	"git.kuhree.com/kuhree.com/djpi/internal/station"
	stationstatus "git.kuhree.com/kuhree.com/djpi/internal/status"
)

var version = "0.0.0-dev"

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
	if len(args) >= 1 && args[0] == "serve" {
		return runServe(args[1:])
	}
	if len(args) >= 1 && args[0] == "status" {
		return runStatus(args[1:])
	}

	fmt.Fprintln(os.Stderr, "djpi is not configured yet")
	fmt.Fprintln(os.Stderr, "usage: djpi <version|probe|inventory|serve|status>")
	return 2
}

func runStatus(args []string) int {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPath := flags.String("config", "", "JSON station configuration")
	jsonOutput := flags.Bool("json", false, "write machine-readable JSON")
	flags.Usage = func() { fmt.Fprintln(os.Stderr, "usage: djpi status --config PATH [--json]") }
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *configPath == "" {
		flags.Usage()
		return 2
	}
	configuration, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load configuration: %v\n", err)
		return 1
	}
	report, err := buildStatusReport(configuration, journal.NewStore(filepath.Join(configuration.StateDirectory, "journal.json")))
	if err != nil {
		fmt.Fprintf(os.Stderr, "build status: %v\n", err)
		return 1
	}
	if err := stationstatus.Write(os.Stdout, report, *jsonOutput); err != nil {
		fmt.Fprintf(os.Stderr, "write status: %v\n", err)
		return 1
	}
	return 0
}

func runServe(args []string) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	pollInterval := flags.Duration("poll-interval", 0, "override camera attachment polling interval")
	configPath := flags.String("config", "", "JSON station configuration")
	once := flags.Bool("once", false, "perform one observation cycle and exit")
	flags.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: djpi serve [--config PATH] [--poll-interval DURATION] [--once]")
	}
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		flags.Usage()
		return 2
	}
	interval := 2 * time.Second
	if *pollInterval < 0 {
		fmt.Fprintln(os.Stderr, "poll interval must be positive")
		return 2
	}
	if *pollInterval > 0 {
		interval = *pollInterval
	}
	service := station.NewService(station.SystemDetector{}, station.SystemInventor{}, station.NewJSONEventSink(os.Stdout))
	statusListen := ""
	var statusHandler operatorhttp.ReportProvider
	if *configPath != "" {
		configuration, err := config.Load(*configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load configuration: %v\n", err)
			return 1
		}
		configuredInterval, err := configuration.ParsedPollInterval()
		if err != nil {
			fmt.Fprintf(os.Stderr, "load configuration: %v\n", err)
			return 1
		}
		if *pollInterval == 0 {
			interval = configuredInterval
		}
		destinations, err := configuredDestinations(context.Background(), configuration, os.LookupEnv)
		if err != nil {
			fmt.Fprintf(os.Stderr, "configure destinations: %v\n", err)
			return 1
		}
		store := journal.NewStore(filepath.Join(configuration.StateDirectory, "journal.json"))
		engine := &ingest.Engine{Journal: store, Destinations: destinations}
		service.WithIngester(engine)
		statusListen = configuration.StatusListen
		statusHandler = func() (stationstatus.Report, error) { return buildStatusReport(configuration, store) }
	}
	if *once {
		if err := service.Step(); err != nil {
			fmt.Fprintf(os.Stderr, "station cycle: %v\n", err)
			return 1
		}
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runners := []func(context.Context) error{
		func(runContext context.Context) error { return service.Run(runContext, interval) },
	}
	if statusListen != "" {
		handler := operatorhttp.Handler(statusHandler)
		runners = append(runners, func(runContext context.Context) error {
			return operatorhttp.ListenAndServe(runContext, statusListen, handler)
		})
	}
	if err := runConcurrent(ctx, runners...); err != nil {
		fmt.Fprintf(os.Stderr, "station service: %v\n", err)
		return 1
	}
	return 0
}

func buildStatusReport(configuration config.Config, store *journal.Store) (stationstatus.Report, error) {
	state, err := store.Load()
	if err != nil {
		return stationstatus.Report{}, fmt.Errorf("load journal: %w", err)
	}
	deviceReport := probe.NewSystemCollector().Collect()
	destinationNames := make([]string, 0, len(configuration.Destinations))
	for _, configured := range configuration.Destinations {
		destinationNames = append(destinationNames, configured.Name)
	}
	return stationstatus.Build(time.Now(), deviceReport.SourceCandidates, state, destinationNames, deviceReport.Warnings), nil
}

func runConcurrent(ctx context.Context, runners ...func(context.Context) error) error {
	if len(runners) == 0 {
		return nil
	}
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, len(runners))
	for _, runner := range runners {
		go func() { results <- runner(runContext) }()
	}
	var firstError error
	for range runners {
		if err := <-results; err != nil && firstError == nil {
			firstError = err
		}
		cancel()
	}
	return firstError
}

func configuredDestinations(ctx context.Context, configuration config.Config, lookupEnv func(string) (string, bool)) ([]destination.Destination, error) {
	result := make([]destination.Destination, 0, len(configuration.Destinations))
	for _, configured := range configuration.Destinations {
		switch configured.Type {
		case "local":
			result = append(result, destination.LocalArchive{DestinationName: configured.Name, Root: configured.Path})
		case "s3":
			accessKey, secretKey, sessionToken := "", "", ""
			if configured.AccessKeyEnv != "" {
				var ok bool
				accessKey, ok = lookupEnv(configured.AccessKeyEnv)
				if !ok || accessKey == "" {
					return nil, fmt.Errorf("destination %q requires non-empty environment variable %s", configured.Name, configured.AccessKeyEnv)
				}
				secretKey, ok = lookupEnv(configured.SecretKeyEnv)
				if !ok || secretKey == "" {
					return nil, fmt.Errorf("destination %q requires non-empty environment variable %s", configured.Name, configured.SecretKeyEnv)
				}
				if configured.SessionTokenEnv != "" {
					sessionToken, ok = lookupEnv(configured.SessionTokenEnv)
					if !ok || sessionToken == "" {
						return nil, fmt.Errorf("destination %q requires non-empty environment variable %s", configured.Name, configured.SessionTokenEnv)
					}
				}
			}
			archive, err := destination.NewS3Archive(ctx, destination.S3Options{
				DestinationName: configured.Name,
				Endpoint:        configured.Endpoint,
				Region:          configured.Region,
				Bucket:          configured.Bucket,
				Prefix:          configured.Prefix,
				UsePathStyle:    configured.UsePathStyle,
				AccessKeyID:     accessKey,
				SecretAccessKey: secretKey,
				SessionToken:    sessionToken,
			})
			if err != nil {
				return nil, fmt.Errorf("destination %q: %w", configured.Name, err)
			}
			result = append(result, archive)
		case "immich":
			apiKey, ok := lookupEnv(configured.APIKeyEnv)
			if !ok || apiKey == "" {
				return nil, fmt.Errorf("destination %q requires non-empty environment variable %s", configured.Name, configured.APIKeyEnv)
			}
			archive, err := destination.NewImmichArchive(destination.ImmichOptions{
				DestinationName: configured.Name,
				ServerURL:       configured.ServerURL,
				APIKey:          apiKey,
			})
			if err != nil {
				return nil, fmt.Errorf("destination %q: %w", configured.Name, err)
			}
			result = append(result, archive)
		default:
			return nil, fmt.Errorf("destination %q has unsupported type %q", configured.Name, configured.Type)
		}
	}
	return result, nil
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
