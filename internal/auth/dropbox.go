package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ostamand/castor/internal/config"
	"golang.org/x/oauth2"
)

// dropboxEndpoint defines the OAuth 2.0 authorization and token URLs for Dropbox
var dropboxEndpoint = oauth2.Endpoint{
	AuthURL:  "https://www.dropbox.com/oauth2/authorize",
	TokenURL: "https://api.dropboxapi.com/oauth2/token",
}

// defaultDropboxAppKey can be injected at compile time via -ldflags:
//
//	-X github.com/ostamand/castor/internal/auth.defaultDropboxAppKey=...
var defaultDropboxAppKey string

// GetDropboxAppKey returns the active Dropbox App Key
func GetDropboxAppKey() string {
	if k := strings.TrimSpace(os.Getenv("CASTOR_DROPBOX_APP_KEY")); k != "" {
		return k
	}
	return strings.TrimSpace(defaultDropboxAppKey)
}

// DropboxCredentials holds OAuth tokens and app key on disk
type DropboxCredentials struct {
	oauth2.Token
	AppKey string `json:"app_key,omitempty"`
}

// HasValidDropboxCredentials checks if stored Dropbox credentials exist
func HasValidDropboxCredentials() bool {
	credsPath := config.DefaultDropboxCredentialsPath()
	data, err := os.ReadFile(credsPath)
	if err != nil {
		return false
	}
	var creds DropboxCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return false
	}
	return creds.AccessToken != "" || creds.RefreshToken != ""
}

// SaveDropboxCredentials persists credentials to ~/.config/castor/dropbox_credentials.json
func SaveDropboxCredentials(path string, token *oauth2.Token, appKey string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	creds := DropboxCredentials{
		Token:  *token,
		AppKey: appKey,
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// LoadDropboxCredentials reads stored Dropbox credentials
func LoadDropboxCredentials(path string) (*DropboxCredentials, error) {
	if path == "" {
		path = config.DefaultDropboxCredentialsPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var creds DropboxCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("corrupt dropbox credentials file: %w", err)
	}
	return &creds, nil
}

// GetDropboxClient returns an authenticated *http.Client that automatically refreshes the token
func GetDropboxClient(ctx context.Context) (*http.Client, error) {
	credsPath := config.DefaultDropboxCredentialsPath()
	creds, err := LoadDropboxCredentials(credsPath)
	if err != nil {
		return nil, fmt.Errorf("dropbox not authenticated: run 'castor auth login dropbox'")
	}

	appKey := creds.AppKey
	if appKey == "" {
		appKey = GetDropboxAppKey()
	}

	conf := &oauth2.Config{
		ClientID: appKey,
		Endpoint: dropboxEndpoint,
	}

	tokenSource := conf.TokenSource(ctx, &creds.Token)
	return oauth2.NewClient(ctx, tokenSource), nil
}

// generatePKCE creates a high-entropy code verifier and its S256 code challenge
func generatePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

// DropboxLogin executes the interactive browser-based OAuth 2.0 PKCE flow
func DropboxLogin(ctx context.Context, appKey string) (*oauth2.Token, error) {
	if appKey == "" {
		appKey = GetDropboxAppKey()
	}
	if appKey == "" {
		return nil, fmt.Errorf("Dropbox App Key not configured: set CASTOR_DROPBOX_APP_KEY environment variable or build with -ldflags")
	}

	verifier, challenge, err := generatePKCE()
	if err != nil {
		return nil, fmt.Errorf("failed to generate PKCE challenge: %w", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start local loopback listener: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	conf := &oauth2.Config{
		ClientID:    appKey,
		Endpoint:    dropboxEndpoint,
		RedirectURL: redirectURL,
		Scopes: []string{
			"files.content.write",
			"files.content.read",
			"account_info.read",
		},
	}

	authURL := conf.AuthCodeURL(
		"castor-dropbox-state",
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("token_access_type", "offline"),
	)

	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errStr := r.URL.Query().Get("error_description")
			if errStr == "" {
				errStr = "missing authorization code"
			}
			http.Error(w, errStr, http.StatusBadRequest)
			errChan <- fmt.Errorf("dropbox auth error: %s", errStr)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body style="font-family: sans-serif; text-align: center; padding-top: 50px;">
			<h2>🦫 Castor Authorization Successful!</h2>
			<p>Dropbox connected successfully. You may now close this browser tab and return to your terminal.</p>
		</body></html>`)
		codeChan <- code
	})

	srv := &http.Server{Handler: mux}
	go func() {
		_ = srv.Serve(listener)
	}()

	OpenBrowser(authURL)

	select {
	case code := <-codeChan:
		token, err := conf.Exchange(
			ctx,
			code,
			oauth2.SetAuthURLParam("code_verifier", verifier),
		)
		if err != nil {
			return nil, fmt.Errorf("dropbox token exchange failed: %w", err)
		}
		credsPath := config.DefaultDropboxCredentialsPath()
		if err := SaveDropboxCredentials(credsPath, token, appKey); err != nil {
			return nil, fmt.Errorf("failed to save dropbox credentials: %w", err)
		}
		return token, nil
	case err := <-errChan:
		return nil, err
	case <-time.After(3 * time.Minute):
		return nil, fmt.Errorf("dropbox authentication timed out after 3 minutes")
	}
}
