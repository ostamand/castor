package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	inspectNamespace string
	inspectDest      string
	inspectSecretKey string
	inspectPattern   string
)

var inspectCmd = &cobra.Command{
	Use:     "inspect <target>",
	Aliases: []string{"view", "info"},
	Short:   "Inspect the contents of a remote archive in-memory",
	Long: `Decompresses and decrypts the remote archive stream directly in memory,
listing all archived files, permissions, sizes, and timestamps without extracting to disk.`,
	Args: cobra.ExactArgs(1),
	RunE: runInspect,
}

func init() {
	inspectCmd.Flags().StringVarP(&inspectNamespace, "namespace", "s", "", "Namespace to inspect")
	inspectCmd.Flags().StringVarP(&inspectDest, "dest", "d", "", "Specific destination to inspect")
	inspectCmd.Flags().StringVarP(&inspectSecretKey, "key", "k", "", "Age private secret key or export CASTOR_AGE_KEY")
	inspectCmd.Flags().StringVarP(&inspectPattern, "pattern", "p", "", "Filter file paths by glob pattern (e.g. '*.go', 'config/*')")
}

func runInspect(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	targetName := args[0]

	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	targetNamespace := cfg.Namespace
	if inspectNamespace != "" {
		targetNamespace = inspectNamespace
	}

	if len(cfg.Destinations) == 0 {
		return fmt.Errorf("no cloud destinations configured")
	}

	dest := cfg.Destinations[0]
	if inspectDest != "" {
		found := false
		for _, d := range cfg.Destinations {
			if d.Name == inspectDest {
				dest = d
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("destination '%s' not found in configuration", inspectDest)
		}
	}

	prov, err := storage.NewProviderFromConfig(ctx, dest)
	if err != nil {
		return fmt.Errorf("failed to connect to provider '%s': %w", dest.Name, err)
	}
	defer prov.Close()

	// Resolve secret key
	secretKey := inspectSecretKey
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

	canonicalKey := config.ResolveCanonicalKey(targetName, targetNamespace, cfg.Targets)

	entries, meta, err := engine.InspectArchive(ctx, prov, canonicalKey, secretKey, inspectPattern)
	if err != nil {
		return fmt.Errorf("failed to inspect archive '%s': %w", targetName, err)
	}

	if jsonOut {
		type jsonPayload struct {
			Target       string                `json:"target"`
			CanonicalKey string                `json:"canonical_key"`
			Destination  string                `json:"destination"`
			Metadata     *engine.ArchiveMetadata `json:"metadata,omitempty"`
			EntryCount   int                   `json:"entry_count"`
			Entries      []engine.ArchiveEntry `json:"entries"`
		}
		out := jsonPayload{
			Target:       targetName,
			CanonicalKey: canonicalKey,
			Destination:  dest.Name,
			Metadata:     meta,
			EntryCount:   len(entries),
			Entries:      entries,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	// Render Header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(tui.ColorPrimary)
	fmt.Printf("🦫 %s · %s\n", headerStyle.Render("Archive Inspection"), targetName)
	fmt.Printf("Canonical Key: %s (%s)\n", canonicalKey, dest.Name)

	if meta != nil {
		fmt.Printf("Created      : %s (by %s on %s/%s)\n",
			meta.CreatedAt.Format("2006-01-02 15:04:05 MST"),
			meta.Host.User, meta.Host.OS, meta.Host.Arch)
		if meta.Git != nil {
			fmt.Printf("Git State    : commit %s on '%s' (dirty: %v)\n",
				engine.ShortCommit(meta.Git.Commit), meta.Git.Branch, meta.Git.Dirty)
		}
		fmt.Printf("Payload      : %s · SHA-256: %s\n",
			meta.Payload.Compression, engine.ShortCommit(meta.Payload.ArchiveSHA256))
	}
	fmt.Println()

	// Render File Table
	fmt.Printf("%-11s  %10s  %-19s  %s\n", "MODE", "SIZE", "MODIFIED", "NAME")
	fmt.Println(strings.Repeat("─", 80))

	var totalBytes int64
	for _, e := range entries {
		totalBytes += e.Size
		nameDisplay := e.Name
		if e.IsDir {
			nameDisplay = lipgloss.NewStyle().Foreground(tui.ColorPrimary).Render(e.Name + "/")
		} else if e.IsGitBundle {
			nameDisplay = lipgloss.NewStyle().Foreground(tui.ColorAccent).Render(e.Name + " (Git History Bundle)")
		}

		fmt.Printf("%-11s  %10s  %-19s  %s\n",
			e.Mode.String(),
			tui.FormatBytes(e.Size),
			e.ModTime.Format("2006-01-02 15:04:05"),
			nameDisplay,
		)
	}

	fmt.Println(strings.Repeat("─", 80))
	summaryText := fmt.Sprintf("Total: %d entries (%s uncompressed)", len(entries), tui.FormatBytes(totalBytes))
	if inspectPattern != "" {
		summaryText += fmt.Sprintf(" [Filter: '%s']", inspectPattern)
	}
	fmt.Println(lipgloss.NewStyle().Bold(true).Render(summaryText))

	return nil
}
