package cli

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/engine"
	"github.com/ostamand/castor/internal/sysinfo"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"diff"},
	Short:   "Show drift between local targets and cloud state, timer health, and orphans",
	Long:    "Inspects local target drift against cloud sync state, validates timer health, and checks for orphaned remote archives.",
	RunE:    runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w (run 'castor init' first)", err)
	}

	state, err := config.LoadState(config.DefaultStatePath())
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	// 1. Systemd timer inspection
	timerInfo := sysinfo.CheckSystemdTimer()
	timerStatusText := tui.BadgeStatus("OFFLINE")
	if timerInfo.Active {
		nextText := ""
		if timerInfo.NextRun != "" {
			nextText = fmt.Sprintf(" (Next: %s)", timerInfo.NextRun)
		}
		timerStatusText = tui.BadgeStatus("ACTIVE") + nextText
	}

	// 2. Namespace & Destination Overview
	headerCard := fmt.Sprintf("Namespace:    %s\nDestinations: %d active\nNightly Run:  %s",
		lipgloss.NewStyle().Bold(true).Foreground(tui.ColorAccent).Render(cfg.Namespace),
		len(cfg.Destinations),
		timerStatusText,
	)
	fmt.Println(tui.StyleCard.Render(headerCard))
	fmt.Println()

	// 3. Inspect targets drift
	var rows [][]string
	for _, t := range cfg.Targets {
		key := config.CanonicalCloudKey(cfg.Namespace, t.Path, t.Namespace)
		name := t.Name
		if name == "" {
			name = path.Base(key)
		}

		absPath := sysinfo.ExpandHome(t.Path)
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			rows = append(rows, []string{name, t.Type, "Missing on disk", tui.BadgeStatus("WARNING")})
			continue
		}

		var currentFingerprint string
		if t.Type == "git" {
			currentFingerprint, _, _ = engine.GitFingerprint(absPath)
		} else {
			currentFingerprint, _, _ = engine.DirectoryFingerprint(absPath, cfg.Rules.Generic.Excludes)
		}

		targetState, exists := state.GetTarget(key)
		statusBadge := tui.BadgeStatus("UNTRACKED")
		lastPushStr := "Never"

		if exists {
			lastPushStr = targetState.LastPush.Format("2006-01-02 15:04")
			if targetState.Fingerprint == currentFingerprint {
				statusBadge = tui.BadgeStatus("SYNCED")
			} else {
				statusBadge = tui.BadgeStatus("DRIFT")
			}
		}

		rows = append(rows, []string{name, t.Type, lastPushStr, statusBadge})
	}

	if len(rows) > 0 {
		fmt.Println(tui.RenderTable([]string{"Target", "Type", "Last Synced (UTC)", "Status"}, rows))
	} else {
		fmt.Println("No targets configured in config.toml. Run 'castor add <dir>' to register targets.")
	}

	// 4. Orphan detection (targets in state but removed from config)
	configKeys := make(map[string]bool)
	for _, t := range cfg.Targets {
		k := config.CanonicalCloudKey(cfg.Namespace, t.Path, t.Namespace)
		configKeys[k] = true
	}

	var orphans []string
	for k := range state.Targets {
		if !configKeys[k] && filepath.Dir(k) != "." {
			orphans = append(orphans, k)
		}
	}

	if len(orphans) > 0 {
		fmt.Printf("\n⚠️  Found %d orphaned archive(s) in cloud state (removed from config.toml):\n", len(orphans))
		for _, o := range orphans {
			fmt.Printf("  • %s\n", o)
		}
		fmt.Println("Run 'castor prune' to clean up orphaned remote archives.")
	}

	return nil
}
