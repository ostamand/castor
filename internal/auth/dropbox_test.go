package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestDropboxPKCEGeneration(t *testing.T) {
	verifier, challenge, err := generatePKCE()
	if err != nil {
		t.Fatalf("generatePKCE failed: %v", err)
	}

	if len(verifier) < 43 {
		t.Errorf("expected verifier length >= 43, got %d", len(verifier))
	}
	if len(challenge) < 43 {
		t.Errorf("expected challenge length >= 43, got %d", len(challenge))
	}

	// Generating a second one should produce unique values
	verifier2, challenge2, _ := generatePKCE()
	if verifier == verifier2 || challenge == challenge2 {
		t.Errorf("expected unique PKCE pairs, got duplicate")
	}
}

func TestDropboxCredentialsSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	credsPath := filepath.Join(tmpDir, "dropbox_creds.json")

	token := &oauth2.Token{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(4 * time.Hour).Truncate(time.Second),
	}

	err := SaveDropboxCredentials(credsPath, token, "custom-app-key")
	if err != nil {
		t.Fatalf("SaveDropboxCredentials failed: %v", err)
	}

	fi, err := os.Stat(credsPath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected permissions 0600, got %v", fi.Mode().Perm())
	}

	loaded, err := LoadDropboxCredentials(credsPath)
	if err != nil {
		t.Fatalf("LoadDropboxCredentials failed: %v", err)
	}

	if loaded.AccessToken != token.AccessToken {
		t.Errorf("expected access token %s, got %s", token.AccessToken, loaded.AccessToken)
	}
	if loaded.RefreshToken != token.RefreshToken {
		t.Errorf("expected refresh token %s, got %s", token.RefreshToken, loaded.RefreshToken)
	}
	if loaded.AppKey != "custom-app-key" {
		t.Errorf("expected app key 'custom-app-key', got '%s'", loaded.AppKey)
	}
}

func TestDropboxAppKeyResolution(t *testing.T) {
	t.Setenv("CASTOR_DROPBOX_APP_KEY", "env-app-key")
	if k := GetDropboxAppKey(); k != "env-app-key" {
		t.Errorf("expected 'env-app-key', got '%s'", k)
	}
}
