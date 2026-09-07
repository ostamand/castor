package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/engine"
	"github.com/ostamand/castor/internal/storage"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	diffNamespace string
	diffDest      string
	diffSecretKey string
)

var diffCmd = &cobra.Command{
	Use:     "diff [target]",
	Aliases: []string{"compare"},
	Short:   "Compare local projects against your cloud archives",
	Long: `Checks your cloud storage and compares it with your local working projects
in milliseconds without downloading or extracting full archives.

Shows commit distance, branch status, and uncommitted edits.`,
	Example: `  castor diff
  castor diff myproject
  castor diff --json`,
	RunE: runDiff,
}

func init() {
	diffCmd.Flags().StringVarP(&diffNamespace, "namespace", "s", "", "Namespace to inspect")
	diffCmd.Flags().StringVarP(&diffDest, "dest", "d", "", "Specific destination to compare against")
	diffCmd.Flags().StringVarP(&diffSecretKey, "key", "k", "", "Age private secret key or export CASTOR_AGE_KEY")
}

func runDiff(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	targetNamespace := cfg.Namespace
	if diffNamespace != "" {
		targetNamespace = diffNamespace
	}

	if len(cfg.Destinations) == 0 {
		return fmt.Errorf("no cloud destinations configured")
	}

	dest := cfg.Destinations[0]
	if diffDest != "" {
		found := false
		for _, d := range cfg.Destinations {
			if d.Name == diffDest {
				dest = d
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("destination '%s' not found in configuration", diffDest)
		}
	}

	prov, err := storage.NewProviderFromConfig(ctx, dest)
	if err != nil {
		return fmt.Errorf("failed to connect to provider '%s': %w", dest.Name, err)
	}
	defer prov.Close()

	// Resolve secret key
	secretKey := diffSecretKey
	if secretKey == "" {
		secretKey = os.Getenv("CASTOR_AGE_KEY")
	}
	if secretKey == "" && cfg.Security.AgePrivateKeyFile != "" {
		if data, err := os.ReadFile(cfg.Security.AgePrivateKeyFile); err == nil {
			secretKey = strings.TrimSpace(string(data))
		}
	}
	if secretKey == "" && cfg.Security.Encrypt && !jsonOut {
		fmt.Print("🔑 Enter Age Secret Key (AGE-SECRET-KEY-1...): ")
		keyBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("failed to read secret key: %w", err)
		}
		secretKey = strings.TrimSpace(string(keyBytes))
	}

	// Filter targets
	var targetsToDiff []config.TargetConfig
	if len(args) > 0 {
		nameOrPath := args[0]
		found := false
		for _, t := range cfg.Targets {
			if t.Name == nameOrPath || t.Path == nameOrPath || filepath.Base(t.Name) == nameOrPath || strings.HasSuffix(t.Name, "/"+nameOrPath) {
				targetsToDiff = append(targetsToDiff, t)
				found = true
				break
			}
		}
		if !found {
			// Ad-hoc target
			targetsToDiff = append(targetsToDiff, config.TargetConfig{
				Name: nameOrPath,
				Path: nameOrPath,
			})
		}
	} else {
		targetsToDiff = cfg.Targets
	}

	if len(targetsToDiff) == 0 {
		fmt.Println("No targets configured or specified.")
		return nil
	}

	var results []*engine.TargetDiff
	for _, target := range targetsToDiff {
		res, err := engine.CompareTargetWithRemote(ctx, prov, target, targetNamespace, secretKey)
		if err != nil {
			return fmt.Errorf("diff failed for '%s': %w", target.Name, err)
		}
		results = append(results, res)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}

	fmt.Printf("🦫 Castor · Remote Drift Diff (Destination: %s, Namespace: %s)\n\n", dest.Name, targetNamespace)

	for _, d := range results {
		renderDiffCard(d, dest.Name)
	}

	return nil
}

func renderDiffCard(d *engine.TargetDiff, destName string) {
	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tui.ColorMuted).
		Padding(1, 2).
		MarginBottom(1).
		Width(78)

	var statusBadge string
	switch d.Status {
	case engine.StatusSynced:
		statusBadge = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorSuccess).Render("✔ IN SYNC")
	case engine.StatusLocalAhead:
		statusBadge = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorPrimary).Render("↑ LOCAL AHEAD")
	case engine.StatusLocalBehind:
		statusBadge = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorWarning).Render("↓ LOCAL BEHIND")
	case engine.StatusDirtyDrift:
		statusBadge = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorAccent).Render("~ DIRTY DRIFT")
	case engine.StatusDiverged:
		statusBadge = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorDanger).Render("✖ DIVERGED")
	case engine.StatusRemoteMissing:
		statusBadge = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorWarning).Render("? NOT IN CLOUD")
	case engine.StatusLocalMissing:
		statusBadge = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorPrimary).Render("☁ CLOUD ONLY")
	}

	title := fmt.Sprintf("%s · %s", lipgloss.NewStyle().Bold(true).Render(d.TargetName), statusBadge)
	cloudKeyLine := fmt.Sprintf("Cloud Key : %s (%s)", d.CanonicalKey, destName)
	localPathLine := fmt.Sprintf("Local Path: %s", d.LocalPath)

	lines := []string{title, cloudKeyLine, localPathLine, ""}

	if d.IsGit {
		lines = append(lines, lipgloss.NewStyle().Bold(true).Render("Git State:"))
		if d.LocalBranch != "" {
			lines = append(lines, fmt.Sprintf("  Branch        : %s", d.LocalBranch))
		}
		if d.LocalCommit != "" {
			lines = append(lines, fmt.Sprintf("  Local Commit  : %s", d.LocalCommit))
		}
		if d.RemoteCommit != "" {
			lines = append(lines, fmt.Sprintf("  Remote Commit : %s", d.RemoteCommit))
		}
		if d.CommitsAhead > 0 || d.CommitsBehind > 0 {
			lines = append(lines, fmt.Sprintf("  Distance      : +%d ahead / -%d behind", d.CommitsAhead, d.CommitsBehind))
		}
		lines = append(lines, fmt.Sprintf("  Working Tree  : %d uncommitted file(s) (Local) vs %d (Cloud)",
			d.LocalUncommittedCount, d.RemoteUncommittedCount))
		if d.LocalStashes > 0 || d.RemoteStashes {
			lines = append(lines, fmt.Sprintf("  Stashes       : %d local stash(es)", d.LocalStashes))
		}
	} else if d.LocalBytes > 0 || d.RemoteBytes > 0 {
		lines = append(lines, lipgloss.NewStyle().Bold(true).Render("Directory Payload:"))
		lines = append(lines, fmt.Sprintf("  Local Size    : %s", tui.FormatBytes(d.LocalBytes)))
		lines = append(lines, fmt.Sprintf("  Remote Size   : %s", tui.FormatBytes(d.RemoteBytes)))
	}

	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Summary: %s", d.Summary))

	fmt.Println(cardStyle.Render(strings.Join(lines, "\n")))
}
