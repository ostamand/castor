package cli

import (
	"encoding/json"
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

var (
	statusDriftOnly bool
)

var statusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"stat", "st"},
	Short:   "Show drift between local targets and cloud state, timer health, and orphans",
	Long:    "Inspects local target drift against cloud sync state, validates timer health, and checks for orphaned remote archives.",
	Example: `  castor status
  castor status --drift`,
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().BoolVarP(&statusDriftOnly, "drift", "d", false, "Only show targets with drift or untracked changes")
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

	statePath := config.StatePathForConfig(configPath)
	state, err := config.LoadState(statePath)
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

	// 2. Inspect targets and gather archive sizes
	var rows [][]string
	var totalStorageBytes int64
	var jsonTargets []TargetStatusItem
	var driftCount, syncedCount, untrackedCount int

	for _, t := range cfg.Targets {
		key := config.CanonicalCloudKey(cfg.Namespace, t.Name)
		name := t.Name
		if name == "" {
			name = path.Base(key)
		}

		targetState, exists := state.GetTarget(key)
		var cipherBytes int64
		if exists {
			for _, dState := range targetState.Destinations {
				if dState.CipherBytes > 0 {
					cipherBytes = dState.CipherBytes
					break
				}
			}
		}

		sizeStr := "—"
		if cipherBytes > 0 {
			sizeStr = tui.FormatBytes(cipherBytes)
			totalStorageBytes += cipherBytes
		}

		absPath := sysinfo.ExpandHome(t.Path)
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			rows = append(rows, []string{name, t.Type, sizeStr, "Missing on disk", tui.BadgeStatus("WARNING")})
			jsonTargets = append(jsonTargets, TargetStatusItem{
				Name:         name,
				Type:         t.Type,
				Path:         t.Path,
				Status:       "MISSING",
				ArchiveBytes: cipherBytes,
				ArchiveSize:  sizeStr,
				LastPush:     "Never",
			})
			continue
		}

		var currentFingerprint string
		if t.Type == "git" {
			currentFingerprint, _, _ = engine.GitFingerprint(absPath)
		} else {
			currentFingerprint, _, _ = engine.DirectoryFingerprint(absPath, cfg.Rules.Generic.Excludes)
		}

		statusBadge := tui.BadgeStatus("UNTRACKED")
		rawStatus := "UNTRACKED"
		lastPushStr := "Never"

		if exists {
			lastPushStr = targetState.LastPush.Format("2006-01-02 15:04")
			if targetState.Fingerprint == currentFingerprint {
				activeDests := targetActiveDestinationNames(t, cfg)
				var missingDests []string
				for _, d := range activeDests {
					dState, dExists := targetState.Destinations[d]
					if !dExists || !dState.Synced {
						missingDests = append(missingDests, d)
					}
				}

				if len(missingDests) == 0 {
					statusBadge = tui.BadgeStatus("SYNCED")
					rawStatus = "SYNCED"
					syncedCount++
				} else {
					statusBadge = tui.BadgeStatus("DESYNC")
					rawStatus = "DESYNC"
					driftCount++
				}
			} else {
				statusBadge = tui.BadgeStatus("DRIFT")
				rawStatus = "DRIFT"
				driftCount++
			}
		} else {
			untrackedCount++
		}

		if !statusDriftOnly || rawStatus != "SYNCED" {
			rows = append(rows, []string{name, t.Type, sizeStr, lastPushStr, statusBadge})
		}
		jsonTargets = append(jsonTargets, TargetStatusItem{
			Name:         name,
			Type:         t.Type,
			Path:         t.Path,
			Status:       rawStatus,
			ArchiveBytes: cipherBytes,
			ArchiveSize:  sizeStr,
			LastPush:     lastPushStr,
		})
	}

	// 3. Namespace & Storage Overview Header Card
	storageText := tui.FormatBytes(totalStorageBytes)
	if totalStorageBytes == 0 {
		storageText = "0 B"
	}

	historyPath := config.HistoryPathForConfig(configPath)
	hist, _ := config.LoadHistory(historyPath)
	lastRunText := "Never"
	lastRunStatus := ""
	lastRunTimeStr := ""
	if hist != nil && len(hist.Runs) > 0 {
		last := hist.Runs[len(hist.Runs)-1]
		lastRunStatus = last.Status
		lastRunTimeStr = last.StartedAt.Format("2006-01-02 15:04 UTC")
		var badge string
		switch last.Status {
		case "success":
			badge = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorSuccess).Render("✔ SUCCESS")
		case "partial":
			badge = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorWarning).Render("⚠ PARTIAL")
		case "failed":
			badge = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorDanger).Render("✖ FAILED")
		default:
			badge = last.Status
		}
		lastRunText = fmt.Sprintf("%s (%s)", badge, lastRunTimeStr)
	}

	// 4. Orphan detection (targets in state but removed from config)
	configKeys := make(map[string]bool)
	for _, t := range cfg.Targets {
		k := config.CanonicalCloudKey(cfg.Namespace, t.Name)
		configKeys[k] = true
	}

	var orphans []string
	for k := range state.Targets {
		if !configKeys[k] && filepath.Dir(k) != "." {
			orphans = append(orphans, k)
		}
	}

	activeDests := cfg.ActiveDestinations()
	disabledCount := len(cfg.Destinations) - len(activeDests)
	destinationsText := fmt.Sprintf("%d active", len(activeDests))
	if disabledCount > 0 {
		destinationsText += fmt.Sprintf(" (%d disabled)", disabledCount)
	}

	// Emit JSON if requested
	if jsonOut {
		payload := StatusPayload{
			Namespace:         cfg.Namespace,
			DestinationsCount: len(activeDests),
			StorageUsedBytes:  totalStorageBytes,
			StorageUsedHuman:  storageText,
			TimerActive:       timerInfo.Active,
			TimerNextRun:      timerInfo.NextRun,
			LastRunStatus:     lastRunStatus,
			LastRunTime:       lastRunTimeStr,
			DriftCount:        driftCount,
			SyncedCount:       syncedCount,
			UntrackedCount:    untrackedCount,
			Targets:           jsonTargets,
			Orphans:           orphans,
		}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	headerCard := fmt.Sprintf("Namespace:     %s\nDestinations:  %s\nStorage Used:  %s\nScheduled Run: %s\nLast Run:      %s",
		lipgloss.NewStyle().Bold(true).Foreground(tui.ColorAccent).Render(cfg.Namespace),
		destinationsText,
		storageText,
		timerStatusText,
		lastRunText,
	)
	fmt.Println(tui.StyleCard.Render(headerCard))
	fmt.Println()

	// 5. Render Targets Table
	if len(rows) > 0 {
		if statusDriftOnly {
			fmt.Printf("Displaying %d target(s) needing attention (%d synced targets hidden, omit -d to show all):\n\n", len(rows), syncedCount)
		}
		fmt.Println(tui.RenderTable([]string{"Target", "Type", "Archive Size", "Last Synced (UTC)", "Status"}, rows))
	} else if statusDriftOnly {
		fmt.Printf("✔ All %d targets are fully synced! Zero drift detected.\n", len(cfg.Targets))
	} else {
		fmt.Println("No targets configured in config.toml. Run 'castor add <dir>' to register targets.")
	}

	if len(orphans) > 0 {
		fmt.Printf("\n⚠️  Found %d orphaned archive(s) in cloud state (removed from config.toml):\n", len(orphans))
		for _, o := range orphans {
			fmt.Printf("  • %s\n", o)
		}
		fmt.Println("Run 'castor prune' to clean up orphaned remote archives.")
	}

	tui.MaybePrintTip(cfg.AreTipsEnabled())
	return nil
}

type TargetStatusItem struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Path         string `json:"path"`
	Status       string `json:"status"`
	ArchiveBytes int64  `json:"archive_bytes"`
	ArchiveSize  string `json:"archive_size"`
	LastPush     string `json:"last_push"`
}

type StatusPayload struct {
	Namespace         string             `json:"namespace"`
	DestinationsCount int                `json:"destinations_count"`
	StorageUsedBytes  int64              `json:"storage_used_bytes"`
	StorageUsedHuman  string             `json:"storage_used_human"`
	TimerActive       bool               `json:"timer_active"`
	TimerNextRun      string             `json:"timer_next_run,omitempty"`
	LastRunStatus     string             `json:"last_run_status,omitempty"`
	LastRunTime       string             `json:"last_run_time,omitempty"`
	DriftCount        int                `json:"drift_count"`
	SyncedCount       int                `json:"synced_count"`
	UntrackedCount    int                `json:"untracked_count"`
	Targets           []TargetStatusItem `json:"targets"`
	Orphans           []string           `json:"orphans,omitempty"`
}
