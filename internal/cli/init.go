package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/lipgloss"
	"github.com/ostamand/castor/internal/auth"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/crypto"
	"github.com/ostamand/castor/internal/sysinfo"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Guided setup wizard for providers, namespace, and Age keys",
	Long:  "Step-by-step interactive onboarding wizard to configure Castor, provision buckets/folders, and generate Age keys.",
	RunE:  runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	if _, err := os.Stat(configPath); err == nil {
		overwrite, err := tui.ConfirmPrompt(
			"Existing Configuration Found",
			fmt.Sprintf("A config file already exists at %s. Overwrite?", configPath),
			false,
		)
		if err != nil || !overwrite {
			fmt.Println("Init cancelled. Existing configuration preserved.")
			return nil
		}
	}

	defaultNS := "workstation"
	formResult, err := tui.RunInitForm(defaultNS)
	if err != nil {
		return err
	}

	cfg := config.DefaultConfig()
	cfg.Namespace = formResult.Namespace

	// Configure destinations
	for _, p := range formResult.Providers {
		switch p {
		case "gcs":
			cfg.Destinations = append(cfg.Destinations, config.DestinationConfig{
				Name:     "google-cloud-storage",
				Provider: "gcs",
				Bucket:   formResult.GCSBucket,
				Location: formResult.GCSLocation,
				Prefix:   "archives",
			})
		case "gdrive":
			cfg.Destinations = append(cfg.Destinations, config.DestinationConfig{
				Name:     "google-drive",
				Provider: "gdrive",
				Folder:   formResult.GDriveFolder,
			})
		case "dropbox":
			folder := formResult.DropboxFolder
			if folder == "" {
				folder = "CastorLodge"
			}
			cfg.Destinations = append(cfg.Destinations, config.DestinationConfig{
				Name:     "dropbox",
				Provider: "dropbox",
				Folder:   folder,
			})
		case "local":
			cfg.Destinations = append(cfg.Destinations, config.DestinationConfig{
				Name:     "local-backup",
				Provider: "local",
				Path:     formResult.LocalPath,
			})
		}
	}

	// Handle Age encryption keypair
	var secretKey string
	if formResult.GenerateAgeKey {
		kp, err := crypto.GenerateKeypair()
		if err != nil {
			return fmt.Errorf("failed to generate Age keypair: %w", err)
		}
		cfg.Security.AgePublicKeys = []string{kp.PublicKey}
		secretKey = kp.SecretKey
	} else if formResult.ExistingPubKey != "" {
		pubKey, err := crypto.ParseRecipientOrIdentity(formResult.ExistingPubKey)
		if err != nil {
			return fmt.Errorf("invalid encryption key: %w", err)
		}
		cfg.Security.AgePublicKeys = []string{pubKey}
	}

	if err := config.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	// Generate systemd user unit templates on Linux
	_ = sysinfo.GenerateSystemdUnits("")

	if secretKey != "" {
		copiedToClipboard := clipboard.WriteAll(secretKey) == nil

		var b strings.Builder
		titleStyle := lipgloss.NewStyle().Bold(true).Foreground(tui.ColorWarning)
		labelStyle := lipgloss.NewStyle().Bold(true).Foreground(tui.ColorMuted)
		valStyle := lipgloss.NewStyle().Foreground(tui.ColorHighlight)
		secretStyle := lipgloss.NewStyle().Bold(true).Foreground(tui.ColorAccent)
		warnStyle := lipgloss.NewStyle().Bold(true).Foreground(tui.ColorDanger)

		b.WriteString(titleStyle.Render("🔐 ACTION REQUIRED: Save Your Secret Encryption Key") + "\n\n")

		b.WriteString(labelStyle.Render("Public Key (saved automatically in config.toml):") + "\n")
		b.WriteString("  " + valStyle.Render(cfg.Security.AgePublicKeys[0]) + "\n\n")

		b.WriteString(secretStyle.Render("Secret Key (CONFIDENTIAL — REQUIRED TO RESTORE BACKUPS):") + "\n")
		b.WriteString("  " + secretStyle.Render(secretKey) + "\n")
		if copiedToClipboard {
			b.WriteString("  " + lipgloss.NewStyle().Foreground(tui.ColorSuccess).Italic(true).Render("📋 Automatically copied to your clipboard!") + "\n")
		}
		b.WriteString("\n")

		b.WriteString(tui.StyleBold.Render("👉 What to do right now:") + "\n")
		b.WriteString("  1. Copy the Secret Key above (or paste from clipboard).\n")
		b.WriteString("  2. Save it securely in your password manager (1Password, Bitwarden, Apple Keychain).\n")
		b.WriteString("  3. Keep it safe — you will need it whenever restoring files on any device.\n\n")

		b.WriteString(warnStyle.Render("⚠️  Castor NEVER saves your Secret Key to disk.") + "\n")
		b.WriteString("Because Castor uses true zero-knowledge end-to-end encryption,\n")
		b.WriteString("if you lose this key, your backups CANNOT be recovered under any circumstances.")

		alertBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(tui.ColorWarning).
			Padding(1, 2).
			Render(b.String())

		fmt.Println()
		fmt.Println(alertBox)
		fmt.Println()

		reader := bufio.NewReader(os.Stdin)
		for {
			confirmed, err := tui.ConfirmSecretKeySavedPrompt()
			if err != nil {
				// User cancelled prompt (Ctrl+C / Esc)
				break
			}
			if confirmed {
				break
			}
			fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorWarning).Render("\n⏳ Please copy and save your Secret Key now. Press [Enter] when ready to confirm..."))
			_, _ = reader.ReadString('\n')
		}
	}

	// Success feedback
	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render("✔ You're all set! Configuration saved to: ") + configPath)

	// Connect Google account if Google storage was selected and not yet authorized
	hasGoogleDest := false
	for _, p := range formResult.Providers {
		if p == "gcs" || p == "gdrive" {
			hasGoogleDest = true
			break
		}
	}

	if hasGoogleDest && !auth.HasValidCredentials() {
		var scopes []string
		hasGCS := false
		hasGDrive := false
		for _, p := range formResult.Providers {
			if p == "gcs" {
				hasGCS = true
			} else if p == "gdrive" {
				hasGDrive = true
			}
		}

		if hasGDrive {
			scopes = append(scopes, auth.ScopeGDrive)
		}
		if hasGCS {
			scopes = append(scopes, auth.ScopeGCS)
		}

		authTarget := "Google"
		if hasGDrive && !hasGCS {
			authTarget = "Google Drive"
		} else if hasGCS && !hasGDrive {
			authTarget = "Google Cloud Storage"
		}

		fmt.Println()
		loginNow, _ := tui.ConfirmPrompt(
			"Connect "+authTarget,
			fmt.Sprintf("Castor needs permission to store archives in %s. Authenticate via browser now?", authTarget),
			true,
		)
		if loginNow {
			fmt.Printf("\n🦫 Opening browser to authenticate with %s...\n", authTarget)
			authCtx, authCancel := context.WithTimeout(context.Background(), 3*time.Minute)
			if _, err := auth.Login(authCtx, scopes...); err != nil {
				fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorWarning).Render(
					fmt.Sprintf("⚠️  Authentication deferred (%v). You can log in later with 'castor auth login'.", err),
				))
			} else {
				fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render(
					fmt.Sprintf("✔ Successfully authenticated with %s!", authTarget),
				))
			}
			authCancel()
		} else {
			fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorWarning).Render(
				fmt.Sprintf("⚠️  Remember to run 'castor auth login' before backing up to %s.", authTarget),
			))
		}
	}

	// Connect Dropbox account if Dropbox was selected and not yet authorized
	hasDropboxDest := false
	for _, p := range formResult.Providers {
		if p == "dropbox" {
			hasDropboxDest = true
			break
		}
	}

	if hasDropboxDest && !auth.HasValidDropboxCredentials() {
		fmt.Println()
		loginNow, _ := tui.ConfirmPrompt(
			"Connect Dropbox",
			"Castor needs permission to store archives in Dropbox. Authenticate via browser now?",
			true,
		)
		if loginNow {
			appKey := auth.GetDropboxAppKey()
			if appKey == "" {
				fmt.Print("Enter your Dropbox App Key: ")
				fmt.Scanln(&appKey)
				appKey = strings.TrimSpace(appKey)
			}
			if appKey != "" {
				fmt.Println("\n🦫 Opening browser to authenticate with Dropbox...")
				authCtx, authCancel := context.WithTimeout(context.Background(), 3*time.Minute)
				if _, err := auth.DropboxLogin(authCtx, appKey, false); err != nil {
					fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorWarning).Render(
						fmt.Sprintf("⚠️  Dropbox authentication deferred (%v). You can log in later with 'castor auth login dropbox'.", err),
					))
				} else {
					fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render(
						"✔ Successfully authenticated with Dropbox!",
					))
				}
				authCancel()
			}
		} else {
			fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorWarning).Render(
				"⚠️  Remember to run 'castor auth login dropbox' before backing up to Dropbox.",
			))
		}
	}

	fmt.Println()
	fmt.Println(tui.StyleBold.Render("What to do next:"))
	step := 1
	if hasGoogleDest && !auth.HasValidCredentials() {
		fmt.Printf("  %d. Authenticate with Google   : %s\n", step, lipgloss.NewStyle().Foreground(tui.ColorAccent).Render("castor auth login"))
		step++
	}
	if hasDropboxDest && !auth.HasValidDropboxCredentials() {
		fmt.Printf("  %d. Authenticate with Dropbox  : %s\n", step, lipgloss.NewStyle().Foreground(tui.ColorAccent).Render("castor auth login dropbox"))
		step++
	}
	fmt.Printf("  %d. Add a folder to back up    : %s\n", step, lipgloss.NewStyle().Foreground(tui.ColorAccent).Render("castor add ~/projects"))
	step++
	fmt.Printf("  %d. Preview your backup plan   : %s\n", step, lipgloss.NewStyle().Foreground(tui.ColorAccent).Render("castor push -n"))
	step++
	fmt.Printf("  %d. Run your first backup      : %s\n", step, lipgloss.NewStyle().Foreground(tui.ColorAccent).Render("castor push"))
	step++
	fmt.Printf("  %d. Turn on automated schedule : %s\n", step, lipgloss.NewStyle().Foreground(tui.ColorAccent).Render("castor schedule on"))

	return nil
}
