package cli

import (
	"context"
	"fmt"
	"io"
	"path"
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
	mvYes     bool
	mvDryRun  bool
	mvNoCloud bool
)

var mvCmd = &cobra.Command{
	Use:     "mv <target> <new-name>",
	Aliases: []string{"rename"},
	Short:   "Rename a target and migrate its local state and cloud archives",
	Long: `Renames an existing backup target in config.toml.

Also migrates local tracking state in state.json and moves remote cloud archives
(and sidecar metadata) directly across storage destinations without re-uploading.`,
	Example: `  castor mv modo git/modo
  castor rename git/modo personal/modo
  castor mv ~/Work/git/modo git/modo --dry-run`,
	Args: cobra.ExactArgs(2),
	RunE: runMv,
}

func init() {
	mvCmd.Flags().BoolVarP(&mvYes, "yes", "y", false, "Skip confirmation prompt")
	mvCmd.Flags().BoolVarP(&mvDryRun, "dry-run", "n", false, "Preview changes without modifying config or storage")
	mvCmd.Flags().BoolVar(&mvNoCloud, "no-cloud", false, "Only rename in config and state, skip renaming cloud archives")
}

func runMv(cmd *cobra.Command, args []string) error {
	defer func() {
		mvYes = false
		mvDryRun = false
		mvNoCloud = false
	}()

	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	oldIdentifier := strings.TrimSpace(args[0])
	newIdentifier := strings.TrimSpace(args[1])

	// 1. Locate target by name, path, or leaf-name
	matchIdx := -1
	for i, t := range cfg.Targets {
		if t.Name == oldIdentifier {
			matchIdx = i
			break
		}
		cleanPath := filepath.Clean(sysinfo.ExpandHome(t.Path))
		cleanInput := filepath.Clean(sysinfo.ExpandHome(oldIdentifier))
		if cleanPath == cleanInput {
			matchIdx = i
			break
		}
	}
	if matchIdx < 0 {
		for i, t := range cfg.Targets {
			if filepath.Base(t.Name) == oldIdentifier || strings.HasSuffix(t.Name, "/"+oldIdentifier) {
				matchIdx = i
				break
			}
		}
	}

	if matchIdx < 0 {
		return fmt.Errorf("target '%s' not found in config\n\nRun 'castor status' to see registered targets.", oldIdentifier)
	}

	target := cfg.Targets[matchIdx]
	cleanNewName := config.NormalizeTargetName(newIdentifier)
	if cleanNewName == "" {
		return fmt.Errorf("new target name cannot be empty")
	}

	if target.Name == cleanNewName {
		fmt.Printf("Target '%s' is already named '%s'. Nothing to do.\n", target.Name, cleanNewName)
		return nil
	}

	// 2. Validate no collision with other existing targets
	for i, t := range cfg.Targets {
		if i != matchIdx && (t.Name == cleanNewName || config.NormalizeTargetName(t.Name) == cleanNewName) {
			return fmt.Errorf("a target named '%s' is already registered in %s (path: %s)", cleanNewName, configPath, t.Path)
		}
	}

	oldCanonicalKey := config.CanonicalCloudKey(cfg.Namespace, target.Name)
	newCanonicalKey := config.CanonicalCloudKey(cfg.Namespace, cleanNewName)

	if mvDryRun {
		fmt.Printf("[DRY-RUN] Target rename plan:\n")
		fmt.Printf("  • Target: %s → %s\n", target.Name, cleanNewName)
		fmt.Printf("  • Key:    %s → %s\n", oldCanonicalKey, newCanonicalKey)
		fmt.Printf("  • Path:   %s\n", sysinfo.ExpandHome(target.Path))
		if mvNoCloud {
			fmt.Println("  • Cloud archives: skipped (--no-cloud)")
		} else {
			fmt.Println("  • Cloud archives: will be migrated across configured destinations")
		}
		return nil
	}

	// Interactive confirmation
	if !mvYes && tui.IsTTY() && !noTUI {
		action := fmt.Sprintf("Rename target '%s' to '%s'?", target.Name, cleanNewName)
		desc := fmt.Sprintf("Canonical cloud key will move from '%s' to '%s'.", oldCanonicalKey, newCanonicalKey)
		confirmed, err := tui.ConfirmPrompt(action, desc, true)
		if err != nil || !confirmed {
			fmt.Println("Rename cancelled.")
			return nil
		}
	}

	// 3. Update target in config.toml
	cfg.Targets[matchIdx].Name = cleanNewName
	if err := config.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config file: %w", err)
	}

	// 4. Update sync state in state.json
	statePath := config.StatePathForConfig(configPath)
	state, _ := config.LoadState(statePath)
	if state != nil && state.Targets != nil {
		if tState, exists := state.Targets[oldCanonicalKey]; exists {
			state.Targets[newCanonicalKey] = tState
			delete(state.Targets, oldCanonicalKey)
			_ = config.SaveState(statePath, state)
		}
	}

	// 5. Migrate remote cloud archives
	migratedCount := 0
	if !mvNoCloud && len(cfg.Destinations) > 0 {
		ctx := context.Background()
		for _, dest := range cfg.Destinations {
			prov, err := storage.NewProviderFromConfig(ctx, dest)
			if err != nil {
				if verbose {
					fmt.Printf("  ⚠ Could not connect to destination '%s': %v\n", dest.Name, err)
				}
				continue
			}

			migrated, err := migrateDestinationObjects(ctx, prov, oldCanonicalKey, newCanonicalKey)
			_ = prov.Close()
			if err != nil && verbose {
				fmt.Printf("  ⚠ Failed to migrate objects in '%s': %v\n", dest.Name, err)
			}
			migratedCount += migrated
		}
	}

	// Summary output
	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render(
		fmt.Sprintf("✔ Renamed target '%s' → '%s'", target.Name, cleanNewName),
	))
	if migratedCount > 0 {
		fmt.Printf("  Migrated %d cloud storage object(s).\n", migratedCount)
	} else if !mvNoCloud {
		fmt.Println("  No existing cloud archives found to migrate.")
	}
	fmt.Printf("  Updated %s\n", configPath)
	fmt.Println()
	fmt.Println("Run 'castor status' to view your updated targets.")

	return nil
}

func migrateDestinationObjects(ctx context.Context, prov storage.Provider, oldCanonicalKey, newCanonicalKey string) (int, error) {
	oldDir := path.Dir(oldCanonicalKey)
	oldBase := path.Base(oldCanonicalKey)
	newDir := path.Dir(newCanonicalKey)
	newBase := path.Base(newCanonicalKey)

	objects, err := prov.List(ctx, oldDir)
	if err != nil {
		return 0, err
	}

	migrated := 0
	for _, obj := range objects {
		cleanObj := strings.TrimPrefix(obj.Name, "archives/")
		objDir := path.Dir(cleanObj)
		objBase := path.Base(cleanObj)

		if objDir == oldDir && (objBase == oldBase || strings.HasPrefix(objBase, oldBase+".")) {
			suffix := strings.TrimPrefix(objBase, oldBase)
			newPath := path.Join(newDir, newBase+suffix)

			if err := streamMoveObject(ctx, prov, obj.Name, newPath); err != nil {
				return migrated, err
			}
			migrated++
		}
	}

	return migrated, nil
}

func streamMoveObject(ctx context.Context, prov storage.Provider, oldName, newName string) error {
	r, err := prov.NewReader(ctx, oldName)
	if err != nil {
		return fmt.Errorf("failed to open source reader for '%s': %w", oldName, err)
	}
	defer r.Close()

	w, err := prov.NewWriter(ctx, newName)
	if err != nil {
		return fmt.Errorf("failed to open target writer for '%s': %w", newName, err)
	}
	defer w.Close()

	if _, err := io.Copy(w, r); err != nil {
		return fmt.Errorf("streaming copy failed from '%s' to '%s': %w", oldName, newName, err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to close target writer '%s': %w", newName, err)
	}

	return prov.Delete(ctx, oldName)
}
