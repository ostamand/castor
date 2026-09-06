package cli

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/storage"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
)

var (
	pruneDryRun bool
	pruneYes    bool
)

var pruneCmd = &cobra.Command{
	Use:     "prune",
	Aliases: []string{"gc"},
	Short:   "Interactively remove cloud archives no longer registered in config.toml",
	Long:    "Discovers orphaned remote archives that were removed from config.toml and safely garbage-collects them.",
	RunE:    runPrune,
}

func init() {
	pruneCmd.Flags().BoolVarP(&pruneDryRun, "dry-run", "n", false, "List orphaned archives without deleting")
	pruneCmd.Flags().BoolVarP(&pruneYes, "yes", "y", false, "Confirm deletion without interactive prompt")
}

func runPrune(cmd *cobra.Command, args []string) error {
	defer func() {
		pruneDryRun = false
		pruneYes = false
	}()
	ctx := context.Background()
	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	state, err := config.LoadState(config.DefaultStatePath())
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	if len(cfg.Destinations) == 0 {
		return fmt.Errorf("no cloud destinations configured")
	}

	// Active targets set
	validKeys := make(map[string]bool)
	for _, t := range cfg.Targets {
		key := config.CanonicalCloudKey(cfg.Namespace, t.Path, t.Namespace)
		validKeys[key] = true
	}

	// Connect to providers
	providers := make(map[string]storage.Provider)
	for _, dest := range cfg.Destinations {
		p, initErr := storage.NewProviderFromConfig(ctx, dest)
		if initErr == nil {
			defer p.Close()
			providers[dest.Name] = p
		}
	}

	// Identify orphaned archives across destinations
	type orphanObj struct {
		canonicalKey string
		archivePath  string
		metaPath     string
		destName     string
		size         int64
	}

	var orphans []orphanObj
	for destName, prov := range providers {
		objects, err := prov.List(ctx, cfg.Namespace)
		if err != nil {
			continue
		}

		for _, obj := range objects {
			if strings.HasSuffix(obj.Name, ".meta.json") || strings.HasSuffix(obj.Name, ".meta.json.age") {
				continue
			}

			cleanKey := strings.TrimPrefix(obj.Name, "archives/")
			cleanKey = strings.TrimSuffix(cleanKey, ".tar.zst.age")
			cleanKey = strings.TrimSuffix(cleanKey, ".tar.zst")
			cleanKey = strings.TrimSuffix(cleanKey, ".tar.xz.age")

			if !validKeys[cleanKey] {
				dirScope := path.Dir(cleanKey)
				baseName := path.Base(cleanKey)
				metaObj := path.Join(dirScope, baseName+".meta.json.age")

				orphans = append(orphans, orphanObj{
					canonicalKey: cleanKey,
					archivePath:  obj.Name,
					metaPath:     metaObj,
					destName:     destName,
					size:         obj.Size,
				})
			}
		}
	}

	if len(orphans) == 0 {
		fmt.Println("✔ No orphaned archives found in cloud vault. Everything is clean!")
		return nil
	}

	fmt.Printf("⚠️  Found %d orphaned archive(s) no longer registered in config.toml:\n\n", len(orphans))
	var rows [][]string
	var totalReclaim int64
	for _, o := range orphans {
		rows = append(rows, []string{o.canonicalKey, o.destName, tui.FormatBytes(o.size)})
		totalReclaim += o.size
	}
	fmt.Println(tui.RenderTable([]string{"Orphan Key", "Destination", "Size"}, rows))
	fmt.Printf("\nTotal space to reclaim: %s\n", tui.FormatBytes(totalReclaim))

	if pruneDryRun {
		fmt.Println("\n[DRY-RUN] No archives were deleted.")
		return nil
	}

	confirmed := pruneYes
	if !confirmed {
		confirmed, err = tui.ConfirmPrompt(
			"Permanently delete orphaned archives?",
			fmt.Sprintf("This will permanently remove %d archives and free %s.", len(orphans), tui.FormatBytes(totalReclaim)),
			false,
		)
		if err != nil || !confirmed {
			fmt.Println("Prune cancelled.")
			return nil
		}
	}

	// Execute deletion
	deletedCount := 0
	for _, o := range orphans {
		prov, ok := providers[o.destName]
		if !ok {
			continue
		}
		_ = prov.Delete(ctx, o.archivePath)
		_ = prov.Delete(ctx, o.metaPath)
		state.DeleteTarget(o.canonicalKey)
		deletedCount++
	}

	_ = config.SaveState(config.DefaultStatePath(), state)

	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render(
		fmt.Sprintf("✔ Pruned %d orphaned archive(s) across all destinations!", deletedCount),
	))

	return nil
}
