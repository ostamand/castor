package cli

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
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
		cfg.Security.AgePublicKeys = []string{formResult.ExistingPubKey}
	}

	if err := config.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	// Generate systemd user unit templates on Linux
	_ = sysinfo.GenerateSystemdUnits("")

	// Success feedback
	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render("✔ You're all set! Configuration saved to: ") + configPath)

	if secretKey != "" {
		alertContent := fmt.Sprintf(`🔐 Save Your Encryption Key

Public Key : %s
Secret Key : %s

⚠️  IMPORTANT: Save this secret key in your password manager (1Password, Bitwarden, Keychain).
Because Castor uses true end-to-end encryption, this key is the only way to restore your archives if this device is ever lost or replaced.`,
			cfg.Security.AgePublicKeys[0], secretKey)

		alertBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(tui.ColorAccent).
			Padding(1, 2).
			Render(alertContent)

		fmt.Println()
		fmt.Println(alertBox)
	}

	fmt.Println()
	fmt.Println(tui.StyleBold.Render("What to do next:"))
	fmt.Println("  1. Add projects to back up   : " + lipgloss.NewStyle().Foreground(tui.ColorAccent).Render("castor add ~/projects -r --git-only"))
	fmt.Println("  2. Preview your backup plan  : " + lipgloss.NewStyle().Foreground(tui.ColorAccent).Render("castor push -n"))
	fmt.Println("  3. Run your first backup     : " + lipgloss.NewStyle().Foreground(tui.ColorAccent).Render("castor push"))
	fmt.Println("  4. Turn on automated schedule: " + lipgloss.NewStyle().Foreground(tui.ColorAccent).Render("castor schedule on"))

	return nil
}
