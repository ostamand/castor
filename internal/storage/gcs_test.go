package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostamand/castor/internal/config"
)

func TestGCSProviderExplicitCredentialsEnforcement(t *testing.T) {
	ctx := context.Background()

	// Case 1: Missing credentials_file must error out explicitly
	destWithoutCreds := config.DestinationConfig{
		Name:     "my-gcs",
		Provider: "gcs",
		Bucket:   "my-bucket",
	}
	_, err := NewProviderFromConfig(ctx, destWithoutCreds)
	if err == nil {
		t.Fatalf("expected error when credentials_file is empty, got nil")
	}
	if !strings.Contains(err.Error(), "requires an explicit service account key file") {
		t.Errorf("unexpected error message: %v", err)
	}

	// Case 2: Non-existent credentials file must error out explicitly
	destWithBadCreds := config.DestinationConfig{
		Name:            "my-gcs",
		Provider:        "gcs",
		Bucket:          "my-bucket",
		CredentialsFile: "/non/existent/key.json",
	}
	_, err = NewProviderFromConfig(ctx, destWithBadCreds)
	if err == nil {
		t.Fatalf("expected error when credentials file does not exist, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error message: %v", err)
	}

	// Case 3: Empty/invalid JSON key file fails during client initialization
	tmpKey := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(tmpKey, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	destWithEmptyJSON := config.DestinationConfig{
		Name:            "my-gcs",
		Provider:        "gcs",
		Bucket:          "my-bucket",
		CredentialsFile: tmpKey,
	}
	// Note: storage.NewClient will fail because {} is not a valid GCP service account JSON key
	_, err = NewProviderFromConfig(ctx, destWithEmptyJSON)
	if err == nil {
		t.Fatalf("expected error parsing invalid service account key JSON, got nil")
	}
}
