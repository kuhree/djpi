package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

type Config struct {
	StateDirectory string        `json:"state_directory"`
	PollInterval   string        `json:"poll_interval,omitempty"`
	Destinations   []Destination `json:"destinations"`
}

type Destination struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Path string `json:"path,omitempty"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var value Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return Config{}, fmt.Errorf("decode configuration: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("decode configuration: trailing JSON value")
	}
	if err := value.Validate(); err != nil {
		return Config{}, err
	}
	return value, nil
}

func (c Config) Validate() error {
	if c.StateDirectory == "" {
		return errors.New("state_directory is required")
	}
	if len(c.Destinations) == 0 {
		return errors.New("at least one destination is required")
	}
	seen := make(map[string]struct{}, len(c.Destinations))
	for _, destination := range c.Destinations {
		if destination.Name == "" {
			return errors.New("destination name is required")
		}
		if _, duplicate := seen[destination.Name]; duplicate {
			return fmt.Errorf("duplicate destination name %q", destination.Name)
		}
		seen[destination.Name] = struct{}{}
		if destination.Type != "local" {
			return fmt.Errorf("destination %q has unsupported type %q", destination.Name, destination.Type)
		}
		if destination.Path == "" {
			return fmt.Errorf("local destination %q requires path", destination.Name)
		}
	}
	_, err := c.ParsedPollInterval()
	return err
}

func (c Config) ParsedPollInterval() (time.Duration, error) {
	if c.PollInterval == "" {
		return 2 * time.Second, nil
	}
	interval, err := time.ParseDuration(c.PollInterval)
	if err != nil {
		return 0, fmt.Errorf("invalid poll_interval: %w", err)
	}
	if interval <= 0 {
		return 0, errors.New("poll_interval must be positive")
	}
	return interval, nil
}
