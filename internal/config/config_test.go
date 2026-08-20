package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadValidConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "djpi.json")
	data := []byte(`{"state_directory":"/var/lib/djpi","poll_interval":"3s","destination_timeout":"1h","status_listen":"127.0.0.1:8787","destinations":[{"name":"archive","type":"local","path":"/srv/archive"}]}`)
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
	if config.StatusListen != "127.0.0.1:8787" {
		t.Fatalf("status listen = %q", config.StatusListen)
	}
	if timeout, err := config.ParsedDestinationTimeout(); err != nil || timeout != time.Hour {
		t.Fatalf("destination timeout = %v, %v", timeout, err)
	}
}

func TestLoadValidS3Configuration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "djpi.json")
	data := []byte(`{"state_directory":"/var/lib/djpi","destinations":[{"name":"object-store","type":"s3","endpoint":"https://objects.example","region":"us-east-1","bucket":"camera","prefix":"incoming","use_path_style":true,"access_key_env":"DJPI_S3_ACCESS_KEY","secret_key_env":"DJPI_S3_SECRET_KEY"}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := value.Destinations[0]; got.Bucket != "camera" || !got.UsePathStyle {
		t.Fatalf("destination = %#v", got)
	}
}

func TestLoadValidImmichConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "djpi.json")
	data := []byte(`{"state_directory":"/var/lib/djpi","destinations":[{"name":"archive","type":"local","path":"/srv/archive"},{"name":"photos","type":"immich","server_url":"https://photos.example/api","api_key_env":"DJPI_IMMICH_API_KEY"}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := value.Destinations[1]; got.ServerURL != "https://photos.example/api" || got.APIKeyEnv != "DJPI_IMMICH_API_KEY" {
		t.Fatalf("destination = %#v", got)
	}
}

func TestConfigurationFailsClosed(t *testing.T) {
	tests := []Config{
		{},
		{StateDirectory: "/state"},
		{StateDirectory: "/state", Destinations: []Destination{{Name: "remote", Type: "s3"}}},
		{StateDirectory: "/state", Destinations: []Destination{{Name: "remote", Type: "s3", Region: "r", Bucket: "b", AccessKeyEnv: "ACCESS"}}},
		{StateDirectory: "/state", Destinations: []Destination{{Name: "remote", Type: "s3", Region: "r", Bucket: "b", Path: "/wrong"}}},
		{StateDirectory: "/state", Destinations: []Destination{{Name: "archive", Type: "local", Path: "/archive", Bucket: "wrong"}}},
		{StateDirectory: "/state", Destinations: []Destination{{Name: "photos", Type: "immich"}}},
		{StateDirectory: "/state", Destinations: []Destination{{Name: "photos", Type: "immich", ServerURL: "https://photos/api", APIKeyEnv: "KEY", Path: "/wrong"}}},
		{StateDirectory: "/state", PollInterval: "0s", Destinations: []Destination{{Name: "archive", Type: "local", Path: "/archive"}}},
		{StateDirectory: "/state", DestinationTimeout: "0s", Destinations: []Destination{{Name: "archive", Type: "local", Path: "/archive"}}},
		{StateDirectory: "/state", StatusListen: "localhost", Destinations: []Destination{{Name: "archive", Type: "local", Path: "/archive"}}},
		{StateDirectory: "/state", StatusListen: "127.0.0.1:0", Destinations: []Destination{{Name: "archive", Type: "local", Path: "/archive"}}},
		{StateDirectory: "/state", StatusListen: "127.0.0.1:http", Destinations: []Destination{{Name: "archive", Type: "local", Path: "/archive"}}},
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
