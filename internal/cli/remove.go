package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/storage"
	"github.com/ostamand/castor/internal/sysinfo"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
)

var (
	removeYes   bool
	removePurge bool
)

var removeCmd = &cobra.Command{
	Use:     "remove <target>",
	Aliases: []string{"rm"},
	Short:   "Remove a target from config.toml",
	Long: `Removes a registered backup target from config.toml by name or path.

By default only the config entry is removed. The cloud archive (if any) is left
untouched so you can still restore it later.

Use --purge to also delete the remote archive and clean local state.`,
	Example: `  castor remove myproject
  castor rm myproject -y
  castor remove myproject --purge`,
	Args: cobra.ExactArgs(1),
	RunE: runRemove,
}

func init() {
	removeCmd.Flags().BoolVarP(&removeYes, "yes", "y", false, "Skip confirmation prompt")
	removeCmd.Flags().BoolVar(&removePurge, "purge", false, "Also delete cloud archives and clean local state")
}

func runRemove(cmd *cobra.Command, args []string) error {
	defer func() {
		removeYes = false
		removePurge = false
	}()

	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	identifier := strings.TrimSpace(args[0])

	// Find the matching target by name or path
	matchIdx := -1
	for i, t := range cfg.Targets {
		if t.Name == identifier {
			matchIdx = i
			break
		}
		cleanPath := filepath.Clean(sysinfo.ExpandHome(t.Path))
		cleanInput := filepath.Clean(sysinfo.ExpandHome(identifier))
		if cleanPath == cleanInput {
			matchIdx = i
			break
		}
	}

	if matchIdx < 0 {
		for i, t := range cfg.Targets {
			if filepath.Base(t.Name) == identifier || strings.HasSuffix(t.Name, "/"+identifier) {
				matchIdx = i
				break
			}
		}
	}

	if matchIdx < 0 {
		return fmt.Errorf("target '%s' not found in config\n\nRun 'castor status' to see registered targets.", identifier)
	}

	target := cfg.Targets[matchIdx]
	canonicalKey := config.CanonicalCloudKey(cfg.Namespace, target.Name)

	// Show what will be removed
	fmt.Println()
	fmt.Printf("  Target:  %s\n", lipgloss.NewStyle().Bold(true).Render(target.Name))
	fmt.Printf("  Path:    %s\n", target.Path)
	fmt.Printf("  Type:    %s\n", target.Type)
	fmt.Printf("  Key:     %s\n", canonicalKey)
	if removePurge {
		fmt.Printf("  Purge:   %s\n", lipgloss.NewStyle().Foreground(tui.ColorWarning).Render("cloud archives will be deleted"))
	}
	fmt.Println()

	// Confirm
	if !removeYes {
		action := "Remove target from config?"
		desc := fmt.Sprintf("This will remove '%s' from config.toml.", target.Name)
		if removePurge {
			desc = fmt.Sprintf("This will remove '%s' from config.toml AND permanently delete its cloud archives.", target.Name)
		}
		confirmed, err := tui.ConfirmPrompt(action, desc, false)
		if err != nil || !confirmed {
			fmt.Println("Remove cancelled.")
			return nil
		}
	}

	// Remove from config
	cfg.Targets = append(cfg.Targets[:matchIdx], cfg.Targets[matchIdx+1:]...)
	if err := config.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Clean state and optionally purge cloud archives
	statePath := config.StatePathForConfig(configPath)
	state, stateErr := config.LoadState(statePath)

	if removePurge {
		ctx := context.Background()
		deletedDests := 0

		for _, dest := range cfg.Destinations {
			prov, initErr := storage.NewProviderFromConfig(ctx, dest)
			if initErr != nil {
				continue
			}
			defer prov.Close()

			// Delete archive file
			archiveName := "archives/" + canonicalKey + "." + archiveExtension(target.Compression, cfg.Security.Encrypt)
			_ = prov.Delete(ctx, archiveName)

			// Delete metadata sidecar
			metaName := "archives/" + config.MetadataFileName(
				filepath.Base(canonicalKey), cfg.Security.Encrypt,
			)
			metaDir := filepath.Dir(canonicalKey)
			if metaDir != "." {
				metaName = "archives/" + filepath.ToSlash(filepath.Join(metaDir, config.MetadataFileName(
					filepath.Base(canonicalKey), cfg.Security.Encrypt,
				)))
			}
			_ = prov.Delete(ctx, metaName)
			deletedDests++
		}

		if deletedDests > 0 {
			fmt.Printf("  Purged archives from %d destination(s)\n", deletedDests)
		}
	}

	// Always clean state entry for removed target
	if stateErr == nil {
		state.DeleteTarget(canonicalKey)
		_ = config.SaveState(statePath, state)
	}

	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render(
		fmt.Sprintf("✔ Removed target '%s' from %s", target.Name, configPath),
	))

	if !removePurge {
		fmt.Println()
		fmt.Println(tui.StyleDim.Render("  Cloud archives were preserved. Use 'castor prune' to clean orphans."))
	}

	return nil
}

// archiveExtension returns the file extension portion for a given compression+encryption combo
func archiveExtension(compression string, encrypted bool) string {
	ext := compression
	if ext == "zstd" || ext == "" {
		ext = "zst"
	}
	base := "tar." + ext
	if encrypted {
		base += ".age"
	}
	return base
}
