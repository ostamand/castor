package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/ostamand/castor/internal/auth"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage cloud storage credentials (Google Drive, Dropbox)",
	Long:  "Authenticate with Google Drive or Dropbox via interactive browser loopback, check token status, or log out. (Note: GCS uses dedicated Service Account key files configured per destination).",
}

var authLoginCmd = &cobra.Command{
	Use:   "login [google|dropbox]",
	Short: "Authenticate via browser OAuth loopback",
	Long:  "Authenticate with Google Drive or Dropbox via browser OAuth loopback.",
	Example: `  castor auth login
  castor auth login dropbox
  castor auth login --gdrive
  castor auth login --dropbox`,
	RunE: runAuthLogin,
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Inspect credentials validity",
	RunE:  runAuthStatus,
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout [google|dropbox]",
	Short: "Remove stored credentials",
	RunE:  runAuthLogout,
}

var (
	authLoginGDrive  bool
	authLoginDropbox bool
	authLoginManual  bool
)

func init() {
	authLoginCmd.Flags().BoolVar(&authLoginGDrive, "gdrive", false, "Authenticate for Google Drive")
	authLoginCmd.Flags().BoolVar(&authLoginDropbox, "dropbox", false, "Authenticate for Dropbox")
	authLoginCmd.Flags().BoolVarP(&authLoginManual, "manual", "m", false, "Manual mode: copy-paste authorization code from Dropbox (no redirect URL needed)")
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authLogoutCmd)
}

func runDropboxLogin(ctx context.Context) error {
	appKey := auth.GetDropboxAppKey()
	if appKey == "" {
		fmt.Print("Enter your Dropbox App Key: ")
		fmt.Scanln(&appKey)
	}
	if appKey == "" {
		return fmt.Errorf("Dropbox App Key required: set CASTOR_DROPBOX_APP_KEY environment variable")
	}

	fmt.Println("🦫 Castor · Dropbox OAuth 2.0 PKCE Authentication")
	fmt.Printf("Opening browser to authorize Dropbox (files.content.write, files.content.read)...\n\n")

	_, err := auth.DropboxLogin(ctx, appKey, authLoginManual)
	if err != nil {
		return err
	}

	credsPath := config.DefaultDropboxCredentialsPath()
	fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render(
		fmt.Sprintf("✔ Successfully authenticated with Dropbox! Token saved to %s (mode 0600)", credsPath),
	))
	return nil
}

func runAuthLogin(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if len(args) > 0 && args[0] == "gcs" {
		return fmt.Errorf("Google Cloud Storage (GCS) uses dedicated Service Account key files, not browser OAuth.\nRun: castor provider add gcs <bucket> --credentials /path/to/key.json")
	}

	if (len(args) > 0 && (args[0] == "dropbox" || args[0] == "dbx")) || authLoginDropbox {
		return runDropboxLogin(ctx)
	}

	scopes := []string{auth.ScopeGDrive}

	fmt.Println("🦫 Castor · Google Drive OAuth 2.0 Loopback Authentication")
	fmt.Printf("Opening browser to authorize Google Drive (%s)...\n\n", auth.ScopeGDrive)

	_, err := auth.Login(ctx, scopes...)
	if err != nil {
		return err
	}

	credsPath := config.DefaultCredentialsPath()
	fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render(
		fmt.Sprintf("✔ Successfully authenticated! Token saved to %s (mode 0600)", credsPath),
	))
	return nil
}

func runAuthStatus(cmd *cobra.Command, args []string) error {
	anyFound := false

	// 1. Google credentials
	googleCredsPath := config.DefaultCredentialsPath()
	if fi, err := os.Stat(googleCredsPath); err == nil {
		if data, err := os.ReadFile(googleCredsPath); err == nil {
			var token oauth2.Token
			if err := json.Unmarshal(data, &token); err == nil {
				anyFound = true
				fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render("✔ Authenticated with Google"))
				fmt.Printf("  • Token File: %s\n", googleCredsPath)
				fmt.Printf("  • Permissions: %v\n", fi.Mode().Perm())
				fmt.Printf("  • Expiration: %s\n", token.Expiry.Format(time.RFC822))
				fmt.Printf("  • Scopes: devstorage.read_write, drive.file\n\n")
			}
		}
	}
	if !anyFound {
		fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorMuted).Render("○ Google: Not authenticated (run 'castor auth login')\n"))
	}

	// 2. Dropbox credentials
	dbxFound := false
	dbxCredsPath := config.DefaultDropboxCredentialsPath()
	if fi, err := os.Stat(dbxCredsPath); err == nil {
		if creds, err := auth.LoadDropboxCredentials(dbxCredsPath); err == nil {
			dbxFound = true
			fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render("✔ Authenticated with Dropbox"))
			fmt.Printf("  • Token File: %s\n", dbxCredsPath)
			fmt.Printf("  • Permissions: %v\n", fi.Mode().Perm())
			if creds.AppKey != "" {
				fmt.Printf("  • App Key: %s\n", creds.AppKey)
			}
			if !creds.Expiry.IsZero() {
				fmt.Printf("  • Expiration: %s\n", creds.Expiry.Format(time.RFC822))
			}
			fmt.Printf("  • Scopes: files.content.write, files.content.read\n")
		}
	}
	if !dbxFound {
		fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorMuted).Render("○ Dropbox: Not authenticated (run 'castor auth login dropbox')"))
	}

	return nil
}

func runAuthLogout(cmd *cobra.Command, args []string) error {
	target := ""
	if len(args) > 0 {
		target = args[0]
	}

	removedAny := false

	if target == "" || target == "google" || target == "gdrive" || target == "gcs" {
		credsPath := config.DefaultCredentialsPath()
		if err := os.Remove(credsPath); err == nil {
			removedAny = true
			fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Render("✔ Google credentials removed."))
		}
	}

	if target == "" || target == "dropbox" || target == "dbx" {
		dbxCredsPath := config.DefaultDropboxCredentialsPath()
		if err := os.Remove(dbxCredsPath); err == nil {
			removedAny = true
			fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Render("✔ Dropbox credentials removed."))
		}
	}

	if !removedAny {
		fmt.Println("Already logged out of all providers.")
	}

	return nil
}
