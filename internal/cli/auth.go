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

var (
	authLoginGDrive bool
	authLoginGCS    bool
)

func init() {
	authLoginCmd.Flags().BoolVar(&authLoginGDrive, "gdrive", false, "Authenticate for Google Drive only")
	authLoginCmd.Flags().BoolVar(&authLoginGCS, "gcs", false, "Authenticate for Google Cloud Storage only")
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authLogoutCmd)
}

func runAuthLogin(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

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
			for _, dest := range cfg.Destinations {
				if dest.Provider == "gcs" {
					hasGCS = true
				} else if dest.Provider == "gdrive" {
					hasGDrive = true
				}
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
