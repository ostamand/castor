package cli

import (
	"context"
	"fmt"
	"os/exec"

	"filippo.io/age"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/storage"
	"github.com/ostamand/castor/internal/sysinfo"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose system tools, Age keys, and cloud reachability",
	Long:  "Runs a comprehensive diagnostic check across system tools, Age keys, network connectivity, and storage destinations.",
	RunE:  runDoctor,
}

func runDoctor(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	fmt.Println("🦫 Castor · Running system diagnostics...")
	fmt.Println()

	var rows [][]string

	// 1. Check Git
	if gitPath, err := exec.LookPath("git"); err == nil {
		rows = append(rows, []string{"Git CLI", gitPath, tui.BadgeStatus("ONLINE")})
	} else {
		rows = append(rows, []string{"Git CLI", "Not found in PATH", tui.BadgeStatus("FAIL")})
	}

	// 2. Check systemd
	timerInfo := sysinfo.CheckSystemdTimer()
	if timerInfo.Available {
		status := "Available"
		if timerInfo.Active {
			status = "Active & scheduled"
		}
		rows = append(rows, []string{"Systemd Timer", status, tui.BadgeStatus("ONLINE")})
	} else {
		rows = append(rows, []string{"Systemd Timer", "Not available (non-systemd)", tui.BadgeStatus("WARNING")})
	}

	// 3. Check Config
	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.LoadConfig(configPath)
	if err == nil {
		rows = append(rows, []string{"Configuration", fmt.Sprintf("%s (Namespace: %s)", configPath, cfg.Namespace), tui.BadgeStatus("ONLINE")})
	} else {
		rows = append(rows, []string{"Configuration", fmt.Sprintf("Error loading %s: %v", configPath, err), tui.BadgeStatus("FAIL")})
	}

	// 4. Check Age keys
	if cfg != nil && cfg.Security.Encrypt {
		validKeys := 0
		for _, k := range cfg.Security.AgePublicKeys {
			if _, err := age.ParseX25519Recipient(k); err == nil {
				validKeys++
			}
		}
		if validKeys > 0 {
			rows = append(rows, []string{"Age Encryption", fmt.Sprintf("%d valid public recipient(s)", validKeys), tui.BadgeStatus("ONLINE")})
		} else {
			rows = append(rows, []string{"Age Encryption", "No valid Age public keys found", tui.BadgeStatus("FAIL")})
		}
	}

	// 5. Check Cloud Destinations
	if cfg != nil {
		for _, dest := range cfg.Destinations {
			prov, provErr := storage.NewProviderFromConfig(ctx, dest)

			if provErr != nil {
				rows = append(rows, []string{fmt.Sprintf("Provider: %s", dest.Name), provErr.Error(), tui.BadgeStatus("FAIL")})
			} else {
				prov.Close()
				rows = append(rows, []string{fmt.Sprintf("Provider: %s", dest.Name), fmt.Sprintf("%s reachability confirmed", dest.Provider), tui.BadgeStatus("ONLINE")})
			}
		}
	}

	fmt.Println(tui.RenderTable([]string{"Component", "Details", "Health"}, rows))
	return nil
}
