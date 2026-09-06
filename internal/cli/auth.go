package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage Google Cloud and Google Drive credentials",
	Long:  "Authenticate with Google via interactive browser loopback, check token status, or log out.",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate via browser OAuth loopback",
	RunE:  runAuthLogin,
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Inspect credentials validity",
	RunE:  runAuthStatus,
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove stored credentials",
	RunE:  runAuthLogout,
}

func init() {
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authLogoutCmd)
}

// Default OAuth client configuration for Castor desktop CLI
var oauthConfig = &oauth2.Config{
	ClientID:     "32555940559.apps.googleusercontent.com", // Public Google standard desktop client
	ClientSecret: "GOCSPX-7fE0iH7Wl_Qf5Rz6e1M9V8g7",
	Scopes: []string{
		"https://www.googleapis.com/auth/devstorage.read_write",
		"https://www.googleapis.com/auth/drive.file",
	},
	Endpoint: google.Endpoint,
}

func runAuthLogin(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// 1. Start ephemeral loopback HTTP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to start local loopback listener: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	conf := *oauthConfig
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

	fmt.Println("🦫 Castor · Google OAuth 2.0 Loopback Authentication")
	fmt.Printf("Opening browser to authorize GCS and Google Drive scopes...\n\n")

	// Try to launch browser
	openBrowser(authURL)
	fmt.Printf("If your browser didn't open automatically, navigate to:\n%s\n\n",
		lipgloss.NewStyle().Foreground(tui.ColorAccent).Render(authURL))

	select {
	case code := <-codeChan:
		token, err := conf.Exchange(ctx, code)
		if err != nil {
			return fmt.Errorf("token exchange failed: %w", err)
		}

		credsPath := config.DefaultCredentialsPath()
		if err := saveToken(credsPath, token); err != nil {
			return fmt.Errorf("failed to save token: %w", err)
		}

		fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render(
			fmt.Sprintf("✔ Successfully authenticated! Token saved to %s (mode 0600)", credsPath),
		))
		return nil

	case err := <-errChan:
		return err

	case <-time.After(3 * time.Minute):
		return fmt.Errorf("authentication timed out after 3 minutes")
	}
}

func runAuthStatus(cmd *cobra.Command, args []string) error {
	credsPath := config.DefaultCredentialsPath()
	fi, err := os.Stat(credsPath)
	if os.IsNotExist(err) {
		fmt.Println(tui.BadgeStatus("OFFLINE") + " No credentials found. Run 'castor auth login'.")
		return nil
	}

	data, err := os.ReadFile(credsPath)
	if err != nil {
		return err
	}

	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return fmt.Errorf("corrupt credentials file: %w", err)
	}

	fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render("✔ Authenticated with Google"))
	fmt.Printf("  • Token File: %s\n", credsPath)
	fmt.Printf("  • Permissions: %v\n", fi.Mode().Perm())
	fmt.Printf("  • Expiration: %s\n", token.Expiry.Format(time.RFC822))
	fmt.Printf("  • Scopes: devstorage.read_write, drive.file\n")

	return nil
}

func runAuthLogout(cmd *cobra.Command, args []string) error {
	credsPath := config.DefaultCredentialsPath()
	if err := os.Remove(credsPath); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Already logged out.")
			return nil
		}
		return err
	}

	fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Render("✔ Successfully logged out. Credentials removed."))
	return nil
}

func saveToken(path string, token *oauth2.Token) error {
	if err := os.MkdirAll(filepathDir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func openBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default: // Linux
		cmd = "xdg-open"
		args = []string{url}
	}

	_ = exec.Command(cmd, args...).Start()
}

func filepathDir(p string) string {
	idx := len(p) - 1
	for idx >= 0 && !os.IsPathSeparator(p[idx]) {
		idx--
	}
	if idx <= 0 {
		return "."
	}
	return p[:idx]
}
