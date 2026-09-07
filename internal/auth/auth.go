package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ostamand/castor/internal/config"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
)

// Scope constants for Google APIs
const (
	ScopeGCS    = "https://www.googleapis.com/auth/devstorage.read_write"
	ScopeGDrive = "https://www.googleapis.com/auth/drive.file"
)

// Default Google OAuth client credentials for Castor desktop CLI.
// Can be injected at compile time via -ldflags:
//   -X github.com/ostamand/castor/internal/auth.defaultClientID=...
//   -X github.com/ostamand/castor/internal/auth.defaultClientSecret=...
var (
	defaultClientID     string
	defaultClientSecret string
)

// OAuthConfig defines the default OAuth client for Castor desktop CLI
var OAuthConfig = &oauth2.Config{
	ClientID:     defaultClientID,
	ClientSecret: defaultClientSecret,
	Scopes: []string{
		ScopeGCS,
		ScopeGDrive,
	},
	Endpoint: google.Endpoint,
}

func init() {
	if cid := os.Getenv("CASTOR_GOOGLE_CLIENT_ID"); cid != "" {
		OAuthConfig.ClientID = cid
	}
	if csec := os.Getenv("CASTOR_GOOGLE_CLIENT_SECRET"); csec != "" {
		OAuthConfig.ClientSecret = csec
	}
}

// HasValidCredentials checks if stored Google credentials exist
func HasValidCredentials() bool {
	credsPath := config.DefaultCredentialsPath()
	data, err := os.ReadFile(credsPath)
	if err != nil {
		return false
	}
	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return false
	}
	return token.AccessToken != "" || token.RefreshToken != ""
}

// GetGoogleClientOptions returns ClientOptions using stored OAuth token if present
func GetGoogleClientOptions(ctx context.Context) ([]option.ClientOption, error) {
	credsPath := config.DefaultCredentialsPath()
	data, err := os.ReadFile(credsPath)
	if err != nil {
		return nil, nil
	}

	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("corrupt credentials file: %w", err)
	}

	tokenSource := OAuthConfig.TokenSource(ctx, &token)
	return []option.ClientOption{option.WithTokenSource(tokenSource)}, nil
}

// SaveToken persists an OAuth token to ~/.config/castor/credentials.json with mode 0600
func SaveToken(path string, token *oauth2.Token) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// OpenBrowser opens a URL in the user's default browser
func OpenBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}

	_ = exec.Command(cmd, args...).Start()
}

// Login runs the browser-based OAuth 2.0 loopback authentication flow
func Login(ctx context.Context, scopes ...string) (*oauth2.Token, error) {
	if OAuthConfig.ClientID == "" || OAuthConfig.ClientSecret == "" {
		return nil, fmt.Errorf("Google OAuth credentials not configured: set CASTOR_GOOGLE_CLIENT_ID and CASTOR_GOOGLE_CLIENT_SECRET environment variables or build with -ldflags")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start local loopback listener: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	conf := *OAuthConfig
	if len(scopes) > 0 {
		conf.Scopes = scopes
	}
	conf.RedirectURL = redirectURL

	authURL := conf.AuthCodeURL("castor-state", oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	codeChan := make(chan string)
	errChan := make(chan error)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Authorization code missing", http.StatusBadRequest)
			errChan <- fmt.Errorf("missing authorization code")
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body style="font-family: sans-serif; text-align: center; padding-top: 50px;">
			<h2>🦫 Castor Authorization Successful!</h2>
			<p>You may now close this browser tab and return to your terminal.</p>
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
		token, err := conf.Exchange(ctx, code)
		if err != nil {
			return nil, fmt.Errorf("token exchange failed: %w", err)
		}
		credsPath := config.DefaultCredentialsPath()
		if err := SaveToken(credsPath, token); err != nil {
			return nil, fmt.Errorf("failed to save token: %w", err)
		}
		return token, nil
	case err := <-errChan:
		return nil, err
	case <-time.After(3 * time.Minute):
		return nil, fmt.Errorf("authentication timed out after 3 minutes")
	}
}
