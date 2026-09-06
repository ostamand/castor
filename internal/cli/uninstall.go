package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/sysinfo"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
)

var (
	uninstallPurge bool
	uninstallYes   bool
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Completely uninstall Castor, background timers, and agent skills",
	Long: `Deactivates background systemd timers, deletes timer unit files, removes LLM agent skills,
and removes the Castor binary from your system.

By default, your configuration directory (~/.config/castor) and Age encryption keys are PRESERVED
to protect against accidental loss of vault access. Use --purge to remove them.`,
	RunE: runUninstall,
}

func init() {
	uninstallCmd.Flags().BoolVarP(&uninstallYes, "yes", "y", false, "Confirm uninstallation without interactive prompt")
	uninstallCmd.Flags().BoolVar(&uninstallPurge, "purge", false, "Also delete ~/.config/castor (configuration and encryption keys)")
}

func runUninstall(cmd *cobra.Command, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to locate home directory: %w", err)
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to locate executable: %w", err)
	}
	execPath, _ = filepath.EvalSymlinks(execPath)

	configDir := filepath.Dir(config.DefaultConfigPath())

	// Interactive confirmation if not -y
	if !uninstallYes {
		fmt.Println(lipgloss.NewStyle().Bold(true).Foreground(tui.ColorWarning).Render("🦫 Castor Uninstaller"))
		fmt.Printf("Binary to remove : %s\n", execPath)
		fmt.Printf("Timers to remove : ~/.config/systemd/user/castor.{service,timer}\n")
		fmt.Printf("Skills to remove : ~/.gemini/config/skills/castor-*\n")

		if uninstallPurge {
			fmt.Println(lipgloss.NewStyle().Bold(true).Foreground(tui.ColorDanger).Render(
				fmt.Sprintf("⚠️  CAUTION: --purge will permanently delete %s, including your Age private keys!", configDir),
			))
		} else {
			fmt.Printf("Config to keep   : %s (use --purge to delete)\n", configDir)
		}
		fmt.Println()

		fmt.Print("Proceed with uninstallation? [y/N]: ")
		reader := bufio.NewReader(os.Stdin)
		ans, _ := reader.ReadString('\n')
		ans = strings.TrimSpace(strings.ToLower(ans))
		if ans != "y" && ans != "yes" {
			fmt.Println("Uninstallation aborted.")
			return nil
		}
	}

	fmt.Println()

	// 1. Deactivate and remove systemd timer units
	_ = sysinfo.DisableSystemdTimer()
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	_ = os.Remove(filepath.Join(unitDir, "castor.service"))
	_ = os.Remove(filepath.Join(unitDir, "castor.timer"))
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	fmt.Println("✔ Background systemd timer disabled and unit files removed.")

	// 2. Remove LLM agent skills
	skillsDir := filepath.Join(home, ".gemini", "config", "skills")
	_ = os.RemoveAll(filepath.Join(skillsDir, "castor-cli"))
	_ = os.RemoveAll(filepath.Join(skillsDir, "castor-customizer"))
	fmt.Println("✔ LLM agent skills removed from ~/.gemini/config/skills/.")

	// 3. Purge config if requested
	if uninstallPurge {
		if err := os.RemoveAll(configDir); err != nil {
			fmt.Printf("⚠️  Failed to purge %s: %v\n", configDir, err)
		} else {
			fmt.Printf("✔ Purged configuration and encryption keys (%s).\n", configDir)
		}
	} else {
		fmt.Printf("ℹ Preserved configuration and encryption keys in %s.\n", configDir)
	}

	// 4. Remove binary
	removedBin := false
	if err := os.Remove(execPath); err == nil {
		removedBin = true
	} else {
		// Attempt sudo if permitted
		if _, err := exec.LookPath("sudo"); err == nil {
			cmd := exec.Command("sudo", "rm", "-f", execPath)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err == nil {
				removedBin = true
			}
		}
	}

	if removedBin {
		fmt.Printf("✔ Removed binary (%s).\n", execPath)
	} else {
		fmt.Printf("⚠️  Could not remove %s (permission denied). Please run:\n", execPath)
		fmt.Printf("     sudo rm -f %s\n", execPath)
	}

	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Bold(true).Foreground(tui.ColorSuccess).Render("✔ Castor uninstalled successfully."))
	return nil
}
