package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"
)

type Config struct {
	StateDirectory     string        `json:"state_directory"`
	PollInterval       string        `json:"poll_interval,omitempty"`
	DestinationTimeout string        `json:"destination_timeout,omitempty"`
	StatusListen       string        `json:"status_listen,omitempty"`
	Destinations       []Destination `json:"destinations"`
}

type Destination struct {
	Name              string `json:"name"`
	Type              string `json:"type"`
	Path              string `json:"path,omitempty"`
	Endpoint          string `json:"endpoint,omitempty"`
	Region            string `json:"region,omitempty"`
	Bucket            string `json:"bucket,omitempty"`
	Prefix            string `json:"prefix,omitempty"`
	UsePathStyle      bool   `json:"use_path_style,omitempty"`
	UploadConcurrency int    `json:"upload_concurrency,omitempty"`
	AccessKeyEnv      string `json:"access_key_env,omitempty"`
	SecretKeyEnv      string `json:"secret_key_env,omitempty"`
	SessionTokenEnv   string `json:"session_token_env,omitempty"`
	ServerURL         string `json:"server_url,omitempty"`
	APIKeyEnv         string `json:"api_key_env,omitempty"`
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
	hasArchive, hasImmich := false, false
	for _, destination := range c.Destinations {
		if destination.Name == "" {
			return errors.New("destination name is required")
		}
		if _, duplicate := seen[destination.Name]; duplicate {
			return fmt.Errorf("duplicate destination name %q", destination.Name)
		}
		seen[destination.Name] = struct{}{}
		switch destination.Type {
		case "local":
			hasArchive = true
			if destination.Path == "" {
				return fmt.Errorf("local destination %q requires path", destination.Name)
			}
			if destination.Endpoint != "" || destination.Region != "" || destination.Bucket != "" || destination.Prefix != "" || destination.UsePathStyle || destination.UploadConcurrency != 0 || destination.AccessKeyEnv != "" || destination.SecretKeyEnv != "" || destination.SessionTokenEnv != "" || destination.ServerURL != "" || destination.APIKeyEnv != "" {
				return fmt.Errorf("local destination %q contains settings for another destination type", destination.Name)
			}
		case "s3":
			hasArchive = true
			if destination.Path != "" {
				return fmt.Errorf("S3 destination %q cannot contain path", destination.Name)
			}
			if destination.Region == "" || destination.Bucket == "" {
				return fmt.Errorf("S3 destination %q requires region and bucket", destination.Name)
			}
			if destination.UploadConcurrency < 0 || destination.UploadConcurrency > 32 {
				return fmt.Errorf("S3 destination %q upload_concurrency must be between 1 and 32 when set", destination.Name)
			}
			if (destination.AccessKeyEnv == "") != (destination.SecretKeyEnv == "") {
				return fmt.Errorf("S3 destination %q must configure both access_key_env and secret_key_env", destination.Name)
			}
			if destination.SessionTokenEnv != "" && destination.AccessKeyEnv == "" {
				return fmt.Errorf("S3 destination %q cannot configure session_token_env without key environment names", destination.Name)
			}
			if destination.ServerURL != "" || destination.APIKeyEnv != "" {
				return fmt.Errorf("S3 destination %q contains Immich-only settings", destination.Name)
			}
		case "immich":
			hasImmich = true
			if destination.Path != "" || destination.Endpoint != "" || destination.Region != "" || destination.Bucket != "" || destination.Prefix != "" || destination.UsePathStyle || destination.UploadConcurrency != 0 || destination.AccessKeyEnv != "" || destination.SecretKeyEnv != "" || destination.SessionTokenEnv != "" {
				return fmt.Errorf("Immich destination %q contains settings for another destination type", destination.Name)
			}
			if destination.ServerURL == "" || destination.APIKeyEnv == "" {
				return fmt.Errorf("Immich destination %q requires server_url and api_key_env", destination.Name)
			}
		default:
			return fmt.Errorf("destination %q has unsupported type %q", destination.Name, destination.Type)
		}
	}
	if hasImmich && !hasArchive {
		return errors.New("Immich cannot be the only destination; configure a local or S3-compatible archive")
	}
	if c.StatusListen != "" {
		_, port, err := net.SplitHostPort(c.StatusListen)
		if err != nil {
			return fmt.Errorf("invalid status_listen: %w", err)
		}
		portNumber, err := strconv.ParseUint(port, 10, 16)
		if err != nil || portNumber == 0 {
			return errors.New("status_listen requires a numeric TCP port between 1 and 65535")
		}
	}
	_, err := c.ParsedPollInterval()
	if err != nil {
		return err
	}
	_, err = c.ParsedDestinationTimeout()
	return err
}

func (c Config) ParsedDestinationTimeout() (time.Duration, error) {
	if c.DestinationTimeout == "" {
		return 6 * time.Hour, nil
	}
	timeout, err := time.ParseDuration(c.DestinationTimeout)
	if err != nil {
		return 0, fmt.Errorf("invalid destination_timeout: %w", err)
	}
	if timeout <= 0 {
		return 0, errors.New("destination_timeout must be positive")
	}
	return timeout, nil
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
