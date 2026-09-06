package auth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ostamand/castor/internal/config"
	"golang.org/x/oauth2"
)

func TestAuthCredentialsLifecycle(t *testing.T) {
	tmp := t.TempDir()
	credsFile := filepath.Join(tmp, "credentials.json")

	// Point default credentials path check to temp directory
	t.Setenv("HOME", tmp)

	// In empty directory, HasValidCredentials should return false
	if HasValidCredentials() {
		t.Errorf("expected HasValidCredentials to be false when no credentials exist")
	}

	opts, err := GetGoogleClientOptions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error from GetGoogleClientOptions: %v", err)
	}
	if len(opts) != 0 {
		t.Errorf("expected 0 options when no credentials exist, got %d", len(opts))
	}

	// Save token
	testToken := &oauth2.Token{
		AccessToken:  "mock-access-token",
		RefreshToken: "mock-refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(1 * time.Hour),
	}

	actualPath := config.DefaultCredentialsPath()
	if err := SaveToken(actualPath, testToken); err != nil {
		t.Fatalf("SaveToken failed: %v", err)
	}

	// Verify permissions
	fi, err := os.Stat(actualPath)
	if err != nil {
		t.Fatalf("failed to stat saved credentials: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected mode 0600, got %v", fi.Mode().Perm())
	}

	// HasValidCredentials should now return true
	if !HasValidCredentials() {
		t.Errorf("expected HasValidCredentials to be true after saving token")
	}

	// GetGoogleClientOptions should return option
	opts, err = GetGoogleClientOptions(context.Background())
	if err != nil {
		t.Fatalf("GetGoogleClientOptions failed: %v", err)
	}
	if len(opts) != 1 {
		t.Errorf("expected 1 client option, got %d", len(opts))
	}
	_ = credsFile
}
