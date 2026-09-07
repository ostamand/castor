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
	Short: "Manage cloud storage credentials (Google Drive, Dropbox, GCS)",
	Long:  "Authenticate with Google or Dropbox via interactive browser loopback, check token status, or log out.",
}

var authLoginCmd = &cobra.Command{
	Use:   "login [google|dropbox]",
	Short: "Authenticate via browser OAuth loopback",
	Long:  "Authenticate with Google or Dropbox. By default, authenticates with Google or the provider specified.",
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
	authLoginGCS     bool
	authLoginDropbox bool
)

func init() {
	authLoginCmd.Flags().BoolVar(&authLoginGDrive, "gdrive", false, "Authenticate for Google Drive only")
	authLoginCmd.Flags().BoolVar(&authLoginGCS, "gcs", false, "Authenticate for Google Cloud Storage only")
	authLoginCmd.Flags().BoolVar(&authLoginDropbox, "dropbox", false, "Authenticate for Dropbox")
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

	_, err := auth.DropboxLogin(ctx, appKey)
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

	if (len(args) > 0 && (args[0] == "dropbox" || args[0] == "dbx")) || authLoginDropbox {
		return runDropboxLogin(ctx)
	}

	var scopes []string
	if authLoginGDrive {
		scopes = append(scopes, auth.ScopeGDrive)
	}
	if authLoginGCS {
		scopes = append(scopes, auth.ScopeGCS)
	}

	// If no flags were passed, inspect config.toml
	if len(scopes) == 0 {
		cfgPath := config.DefaultConfigPath()
		if cfg, err := config.LoadConfig(cfgPath); err == nil {
			hasGCS := false
			hasGDrive := false
			hasDropbox := false
			for _, dest := range cfg.Destinations {
				if dest.Provider == "gcs" {
					hasGCS = true
				} else if dest.Provider == "gdrive" {
					hasGDrive = true
				} else if dest.Provider == "dropbox" || dest.Provider == "dbx" {
					hasDropbox = true
				}
			}
			if hasDropbox && !hasGDrive && !hasGCS {
				return runDropboxLogin(ctx)
			}
			if hasGDrive {
				scopes = append(scopes, auth.ScopeGDrive)
			}
			if hasGCS {
				scopes = append(scopes, auth.ScopeGCS)
			}
		}
	}

	// If still empty (e.g. no config or no Google destinations configured), default to Google Drive
	if len(scopes) == 0 {
		scopes = append(scopes, auth.ScopeGDrive)
	}

	fmt.Println("🦫 Castor · Google OAuth 2.0 Loopback Authentication")
	if len(scopes) == 1 && scopes[0] == auth.ScopeGDrive {
		fmt.Printf("Opening browser to authorize Google Drive (%s)...\n\n", auth.ScopeGDrive)
	} else if len(scopes) == 1 && scopes[0] == auth.ScopeGCS {
		fmt.Printf("Opening browser to authorize Google Cloud Storage (%s)...\n\n", auth.ScopeGCS)
	} else {
		fmt.Printf("Opening browser to authorize Google Cloud Storage and Google Drive...\n\n")
	}

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
