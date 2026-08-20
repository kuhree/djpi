package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadValidConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "djpi.json")
	data := []byte(`{"state_directory":"/var/lib/djpi","poll_interval":"3s","destinations":[{"name":"archive","type":"local","path":"/srv/archive"}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	interval, err := config.ParsedPollInterval()
	if err != nil || interval != 3*time.Second {
		t.Fatalf("interval = %v, %v", interval, err)
	}
}

func TestConfigurationFailsClosed(t *testing.T) {
	tests := []Config{
		{},
		{StateDirectory: "/state"},
		{StateDirectory: "/state", Destinations: []Destination{{Name: "remote", Type: "s3"}}},
		{StateDirectory: "/state", PollInterval: "0s", Destinations: []Destination{{Name: "archive", Type: "local", Path: "/archive"}}},
		{StateDirectory: "/state", Destinations: []Destination{{Name: "same", Type: "local", Path: "/one"}, {Name: "same", Type: "local", Path: "/two"}}},
	}
	for _, value := range tests {
		if err := value.Validate(); err == nil {
			t.Fatalf("expected invalid configuration: %#v", value)
		}
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "djpi.json")
	data := []byte(`{"state_directory":"/state","destinations":[{"name":"archive","type":"local","path":"/archive"}],"cleanup":true}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown field rejection")
	}
}
