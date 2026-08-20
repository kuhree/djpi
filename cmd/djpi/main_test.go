package main

import (
	"context"
	"testing"

	"git.kuhree.com/kuhree.com/djpi/internal/config"
)

func TestConfiguredDestinationsBuildsLocalAndS3(t *testing.T) {
	configuration := config.Config{Destinations: []config.Destination{
		{Name: "archive", Type: "local", Path: "/srv/archive"},
		{Name: "objects", Type: "s3", Endpoint: "https://objects.example", Region: "us-east-1", Bucket: "media", UsePathStyle: true, AccessKeyEnv: "ACCESS", SecretKeyEnv: "SECRET"},
		{Name: "photos", Type: "immich", ServerURL: "https://photos.example/api", APIKeyEnv: "IMMICH_KEY"},
	}}
	environment := map[string]string{"ACCESS": "access-value", "SECRET": "secret-value", "IMMICH_KEY": "immich-value"}
	destinations, err := configuredDestinations(context.Background(), configuration, func(name string) (string, bool) {
		value, ok := environment[name]
		return value, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(destinations) != 3 || destinations[0].Name() != "archive" || destinations[1].Name() != "objects" || destinations[2].Name() != "photos" {
		t.Fatalf("destinations = %#v", destinations)
	}
}

func TestConfiguredDestinationsRequiresNamedCredentialEnvironment(t *testing.T) {
	configuration := config.Config{Destinations: []config.Destination{{
		Name: "objects", Type: "s3", Region: "us-east-1", Bucket: "media", AccessKeyEnv: "ACCESS", SecretKeyEnv: "SECRET",
	}}}
	_, err := configuredDestinations(context.Background(), configuration, func(string) (string, bool) { return "", false })
	if err == nil || err.Error() != `destination "objects" requires non-empty environment variable ACCESS` {
		t.Fatalf("error = %v", err)
	}
}
