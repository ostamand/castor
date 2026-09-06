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

func init() {
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authLogoutCmd)
}

func runAuthLogin(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	fmt.Println("🦫 Castor · Google OAuth 2.0 Loopback Authentication")
	fmt.Printf("Opening browser to authorize GCS and Google Drive scopes...\n\n")

	_, err := auth.Login(ctx)
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
