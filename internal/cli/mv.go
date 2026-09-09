package cli

import (
	"context"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"
	"time"

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

	ctx := context.Background()
	if cmd != nil && cmd.Context() != nil {
		ctx = cmd.Context()
	}

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
	cleanNewName := config.NormalizeTargetName(newIdentifier)
	if cleanNewName == "" {
		return fmt.Errorf("new target name cannot be empty")
	}

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

	// If target not found under old identifier, check if it was already renamed to newIdentifier
	if matchIdx < 0 {
		alreadyIdx := -1
		for i, t := range cfg.Targets {
			if t.Name == cleanNewName || config.NormalizeTargetName(t.Name) == cleanNewName {
				alreadyIdx = i
				break
			}
		}
		if alreadyIdx >= 0 {
			oldCanonicalKey := config.CanonicalCloudKey(cfg.Namespace, oldIdentifier)
			newCanonicalKey := config.CanonicalCloudKey(cfg.Namespace, cleanNewName)

			fmt.Printf("Target '%s' is already registered in %s.\n", cleanNewName, configPath)
			activeDests := cfg.ActiveDestinations()
			if !mvNoCloud && len(activeDests) > 0 {
				fmt.Printf("\nChecking for any unmigrated remote archives under '%s'...\n", oldCanonicalKey)
				migratedCount := migrateAllDestinations(ctx, activeDests, oldCanonicalKey, newCanonicalKey)
				if migratedCount > 0 {
					fmt.Printf("\n✔ Migrated %d remaining cloud storage object(s) to '%s'.\n", migratedCount, newCanonicalKey)
				} else {
					fmt.Println("  No unmigrated cloud archives found.")
				}
			}
			return nil
		}

		return fmt.Errorf("target '%s' not found in config\n\nRun 'castor status' to see registered targets.", oldIdentifier)
	}

	target := cfg.Targets[matchIdx]

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

	// 3. Migrate remote cloud archives on active destinations
	activeDests := cfg.ActiveDestinations()
	migratedCount := 0
	if !mvNoCloud && len(activeDests) > 0 {
		fmt.Println()
		migratedCount = migrateAllDestinations(ctx, activeDests, oldCanonicalKey, newCanonicalKey)
	}

	// 4. Update target in config.toml
	cfg.Targets[matchIdx].Name = cleanNewName
	if err := config.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config file: %w", err)
	}

	// 5. Update sync state in state.json
	statePath := config.StatePathForConfig(configPath)
	state, _ := config.LoadState(statePath)
	if state != nil && state.Targets != nil {
		if tState, exists := state.Targets[oldCanonicalKey]; exists {
			state.Targets[newCanonicalKey] = tState
			delete(state.Targets, oldCanonicalKey)
			_ = config.SaveState(statePath, state)
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

func migrateAllDestinations(ctx context.Context, dests []config.DestinationConfig, oldCanonicalKey, newCanonicalKey string) int {
	migratedCount := 0
	for _, dest := range dests {
		fmt.Printf("📦 Destination '%s' (%s):\n", dest.Name, dest.Provider)
		prov, err := storage.NewProviderFromConfig(ctx, dest)
		if err != nil {
			fmt.Printf("   ⚠ Could not connect: %v\n", err)
			continue
		}

		migrated, err := migrateDestinationObjects(ctx, prov, oldCanonicalKey, newCanonicalKey)
		_ = prov.Close()
		if err != nil {
			fmt.Printf("   ⚠ Failed to migrate objects: %v\n", err)
		}
		migratedCount += migrated
	}
	return migratedCount
}

func migrateDestinationObjects(ctx context.Context, prov storage.Provider, oldCanonicalKey, newCanonicalKey string) (int, error) {
	oldDir := path.Dir(oldCanonicalKey)
	oldBase := path.Base(oldCanonicalKey)
	newDir := path.Dir(newCanonicalKey)
	newBase := path.Base(newCanonicalKey)

	// List using the targeted canonical key prefix
	objects, err := prov.List(ctx, oldCanonicalKey)
	if err != nil || len(objects) == 0 {
		// Fallback to oldDir in case provider requires folder-level listing
		dirObjects, dirErr := prov.List(ctx, oldDir)
		if dirErr == nil && len(dirObjects) > 0 {
			objects = dirObjects
		} else if err != nil {
			return 0, err
		}
	}

	var toMigrate []storage.ObjectInfo
	for _, obj := range objects {
		cleanObj := strings.TrimPrefix(obj.Name, "archives/")
		objDir := path.Dir(cleanObj)
		objBase := path.Base(cleanObj)

		if objDir == oldDir && (objBase == oldBase || strings.HasPrefix(objBase, oldBase+".")) {
			toMigrate = append(toMigrate, obj)
		}
	}

	if len(toMigrate) == 0 {
		fmt.Printf("   • No existing archives found for '%s'\n", oldCanonicalKey)
		return 0, nil
	}

	fmt.Printf("   • Found %d archive object(s)\n", len(toMigrate))
	migrated := 0
	for _, obj := range toMigrate {
		cleanObj := strings.TrimPrefix(obj.Name, "archives/")
		objBase := path.Base(cleanObj)
		suffix := strings.TrimPrefix(objBase, oldBase)
		newPath := path.Join(newDir, newBase+suffix)

		start := time.Now()
		_, isMover := prov.(storage.Mover)
		modeDesc := "stream"
		if isMover {
			modeDesc = "instant"
		}

		fmt.Printf("   • Moving %s → %s (%s)...", objBase, path.Base(newPath), modeDesc)
		if err := moveObject(ctx, prov, obj.Name, newPath); err != nil {
			fmt.Printf(" ❌ failed: %v\n", err)
			return migrated, err
		}
		fmt.Printf(" ✔ done (%s)\n", time.Since(start).Round(time.Millisecond))
		migrated++
	}

	return migrated, nil
}

func moveObject(ctx context.Context, prov storage.Provider, oldName, newName string) error {
	if mover, ok := prov.(storage.Mover); ok {
		return mover.Move(ctx, oldName, newName)
	}
	return streamMoveObject(ctx, prov, oldName, newName)
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
