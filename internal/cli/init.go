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

	host := sysinfo.GetHostInfo()
	formResult, err := tui.RunInitForm(host.DefaultNamespace)
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
				Name:     "gcp-coldline",
				Provider: "gcs",
				Bucket:   formResult.GCSBucket,
				Location: formResult.GCSLocation,
				Prefix:   "archives",
			})
		case "gdrive":
			cfg.Destinations = append(cfg.Destinations, config.DestinationConfig{
				Name:     "gdrive-mirror",
				Provider: "gdrive",
				Folder:   formResult.GDriveFolder,
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
	fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render("✔ Setup complete! Configuration written to: ") + configPath)

	if secretKey != "" {
		alertContent := fmt.Sprintf(`Public Recipient Key:
  %s

Secret Private Key:
  %s

⚠️  CRITICAL: Store this secret key in 1Password, Bitwarden, or print it.
This private key is NOT retained on this computer for write-only host security!`,
			cfg.Security.AgePublicKeys[0], secretKey)

		alertBox := lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(tui.ColorDanger).
			Padding(1, 2).
			Render(alertContent)

		fmt.Println()
		fmt.Println(alertBox)
	}

	fmt.Println()
	fmt.Println(tui.StyleBold.Render("Next steps:"))
	fmt.Println("  1. Discover & add projects:  " + lipgloss.NewStyle().Foreground(tui.ColorAccent).Render("castor add ~/projects -r --git-only"))
	fmt.Println("  2. Test execution plan:      " + lipgloss.NewStyle().Foreground(tui.ColorAccent).Render("castor push -n"))
	fmt.Println("  3. Stream first backup:      " + lipgloss.NewStyle().Foreground(tui.ColorAccent).Render("castor push"))

	return nil
}
