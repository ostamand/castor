package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/sysinfo"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:     "config",
	Short:   "View or edit Castor configuration",
	Example: `  castor config
  castor config edit`,
	RunE:    runConfigShow,
}

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Open config.toml in your editor with post-save validation",
	RunE:  runConfigEdit,
}

func init() {
	configCmd.AddCommand(configEditCmd)
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	cfg, cfgFilePath, err := loadConfig()
	if err != nil {
		return err
	}

	titleStyle := tui.StyleBold.Copy().Foreground(tui.ColorAccent)

	fmt.Printf("%s\n\n", titleStyle.Render(fmt.Sprintf("🦫 Castor · Configuration (%s)", sysinfo.ExpandHome(cfgFilePath))))

	fmt.Printf("%-15s%s\n", tui.StyleDim.Render("Namespace:"), cfg.Namespace)

	encryptionStr := "disabled"
	if cfg.Security.Encrypt {
		encryptionStr = fmt.Sprintf("age (%d public key)", len(cfg.Security.AgePublicKeys))
	}
	fmt.Printf("%-15s%s\n", tui.StyleDim.Render("Encryption:"), encryptionStr)

	fmt.Printf("%-15s%s level %d\n", tui.StyleDim.Render("Compression:"), "zstd", cfg.Performance.CompressionLevel)
	fmt.Printf("%-15s%d\n", tui.StyleDim.Render("Workers:"), cfg.Performance.MaxWorkers)

	tipsStr := "enabled"
	if !cfg.AreTipsEnabled() {
		tipsStr = "disabled"
	}
	fmt.Printf("%-15s%s\n\n", tui.StyleDim.Render("Tips:"), tipsStr)

	fmt.Printf("Destinations (%d):\n", len(cfg.Destinations))
	for _, dest := range cfg.Destinations {
		fmt.Printf("  • %s (%s)", dest.Name, dest.Provider)
		if dest.Disabled {
			fmt.Printf(" %s", lipgloss.NewStyle().Foreground(tui.ColorMuted).Render("[disabled]"))
		}
		if dest.Folder != "" {
			fmt.Printf(" → folder: %s", dest.Folder)
		} else if dest.Provider == "dropbox" || dest.Provider == "dbx" {
			fmt.Printf(" → root (app folder)")
		}
		if dest.Bucket != "" {
			fmt.Printf(" → bucket: %s", dest.Bucket)
		}
		if dest.Path != "" {
			fmt.Printf(" → %s", dest.Path)
		}
		fmt.Println()
	}
	fmt.Println()

	fmt.Printf("Targets (%d):\n", len(cfg.Targets))
	for _, target := range cfg.Targets {
		fmt.Printf("  %-20s %-7s %s\n", target.Name, target.Type, sysinfo.ExpandHome(target.Path))
	}

	return nil
}

func runConfigEdit(cmd *cobra.Command, args []string) error {
	oldCfg, cfgFilePath, err := loadConfig()
	if err != nil {
		return err
	}

	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "nano"
		if _, err := exec.LookPath("nano"); err != nil {
			editor = "vi"
		}
	}

	parts := strings.Fields(editor)
	if len(parts) == 0 {
		parts = []string{"nano"}
	}
	execArgs := append(parts[1:], cfgFilePath)
	c := exec.Command(parts[0], execArgs...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	if err := c.Run(); err != nil {
		return fmt.Errorf("editor failed: %w", err)
	}

	newCfg, err := config.LoadConfig(cfgFilePath)
	if err != nil {
		fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorDanger).Render(fmt.Sprintf("❌ Configuration validation failed:\n%v", err)))
		fmt.Println("Note: the file was saved, but castor may not function correctly until you fix the errors.")
		return nil
	}

	compareConfig(oldCfg, newCfg)

	return nil
}

func loadConfig() (*config.Config, string, error) {
	path := cfgPath
	if path == "" {
		path = config.DefaultConfigPath()
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		return nil, path, err
	}
	return cfg, path, nil
}

func compareConfig(oldCfg, newCfg *config.Config) {
	warningStyle := lipgloss.NewStyle().Foreground(tui.ColorWarning)
	successStyle := lipgloss.NewStyle().Foreground(tui.ColorSuccess)

	// Dangerous changes
	if oldCfg.Namespace != newCfg.Namespace {
		fmt.Println(warningStyle.Render(fmt.Sprintf("⚠️  Namespace changed: '%s' → '%s'. Existing archives under '%s/' will become orphaned. Use 'castor pull --namespace %s' to access old archives.", oldCfg.Namespace, newCfg.Namespace, oldCfg.Namespace, oldCfg.Namespace)))
	}

	oldDestMap := make(map[string]config.DestinationConfig)
	for _, d := range oldCfg.Destinations {
		oldDestMap[d.Name] = d
	}
	newDestMap := make(map[string]config.DestinationConfig)
	for _, d := range newCfg.Destinations {
		newDestMap[d.Name] = d
	}

	for name := range oldDestMap {
		if _, exists := newDestMap[name]; !exists {
			fmt.Println(warningStyle.Render(fmt.Sprintf("⚠️  Destination '%s' was removed. Archives stored there are no longer managed.", name)))
		}
	}

	if oldCfg.Security.Encrypt && !newCfg.Security.Encrypt {
		fmt.Println(warningStyle.Render("⚠️  Encryption disabled. New archives will NOT be encrypted."))
	}

	if oldCfg.Security.Encrypt && newCfg.Security.Encrypt {
		if !stringSlicesEqual(oldCfg.Security.AgePublicKeys, newCfg.Security.AgePublicKeys) {
			fmt.Println(warningStyle.Render("⚠️  Age public key changed. You won't be able to decrypt archives encrypted with the previous key."))
		}
	}

	// Non-dangerous changes
	if oldCfg.Performance.CompressionLevel != newCfg.Performance.CompressionLevel {
		fmt.Println(successStyle.Render(fmt.Sprintf("✔ Compression level changed: %d → %d", oldCfg.Performance.CompressionLevel, newCfg.Performance.CompressionLevel)))
	}

	if oldCfg.Performance.MaxWorkers != newCfg.Performance.MaxWorkers {
		fmt.Println(successStyle.Render(fmt.Sprintf("✔ Workers changed: %d → %d", oldCfg.Performance.MaxWorkers, newCfg.Performance.MaxWorkers)))
	}

	for name, dest := range newDestMap {
		if _, exists := oldDestMap[name]; !exists {
			fmt.Println(successStyle.Render(fmt.Sprintf("✔ Added destination '%s' (%s)", name, dest.Provider)))
		}
	}

	oldTargetMap := make(map[string]config.TargetConfig)
	for _, t := range oldCfg.Targets {
		oldTargetMap[t.Name] = t
	}
	newTargetMap := make(map[string]config.TargetConfig)
	for _, t := range newCfg.Targets {
		newTargetMap[t.Name] = t
	}

	addedTargets := 0
	for name := range newTargetMap {
		if _, exists := oldTargetMap[name]; !exists {
			addedTargets++
		}
	}
	if addedTargets > 0 {
		fmt.Println(successStyle.Render(fmt.Sprintf("✔ Added %d new target(s)", addedTargets)))
	}

	removedTargets := 0
	for name := range oldTargetMap {
		if _, exists := newTargetMap[name]; !exists {
			removedTargets++
		}
	}
	if removedTargets > 0 {
		fmt.Println(successStyle.Render(fmt.Sprintf("✔ Removed %d target(s)", removedTargets)))
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
