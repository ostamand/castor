package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/storage"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
)

var (
	lsNamespace     string
	lsAllNamespaces bool
	lsDest          string
	lsInteractive   bool
)

var lsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"cache"},
	Short:   "List remote cloud archives, byte sizes, and timestamps",
	Long:    "Inspects remote vault destinations and displays all backed-up archives, sizes, and tiers.",
	RunE:    runLs,
}

func init() {
	lsCmd.Flags().StringVarP(&lsNamespace, "namespace", "s", "", "List archives for a specific namespace")
	lsCmd.Flags().BoolVar(&lsAllNamespaces, "all-namespaces", false, "List archives across all namespaces in the vault")
	lsCmd.Flags().StringVar(&lsDest, "dest", "", "Filter to a specific storage destination")
	lsCmd.Flags().BoolVarP(&lsInteractive, "interactive", "i", false, "Open an interactive archive browser")
}

func runLs(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if len(cfg.Destinations) == 0 {
		return fmt.Errorf("no cloud destinations configured in config.toml")
	}

	dest := cfg.Destinations[0]
	if lsDest != "" {
		found := false
		for _, d := range cfg.Destinations {
			if d.Name == lsDest {
				dest = d
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("destination '%s' not found in config", lsDest)
		}
	}

	prov, err := storage.NewProviderFromConfig(ctx, dest)
	if err != nil {
		return fmt.Errorf("failed to connect to '%s': %w", dest.Name, err)
	}
	defer prov.Close()

	prefix := cfg.Namespace
	if lsAllNamespaces {
		prefix = ""
	} else if lsNamespace != "" {
		prefix = lsNamespace
	}

	objects, err := prov.List(ctx, prefix)
	if err != nil {
		return fmt.Errorf("failed to list archives: %w", err)
	}

	if len(objects) == 0 {
		fmt.Printf("No archives found in destination '%s' (prefix: '%s').\n", dest.Name, prefix)
		return nil
	}

	var rows [][]string
	var items []tui.ArchiveItem

	for _, obj := range objects {
		if strings.HasSuffix(obj.Name, ".meta.json") || strings.HasSuffix(obj.Name, ".meta.json.age") {
			continue // Hide raw sidecars from primary listing
		}

		cleanName := strings.TrimPrefix(obj.Name, "archives/")
		tier := obj.StorageClass
		if tier == "" {
			tier = "STANDARD"
		}

		rows = append(rows, []string{
			cleanName,
			tui.FormatBytes(obj.Size),
			obj.Updated.Format("2006-01-02 15:04"),
			tier,
		})

		items = append(items, tui.ArchiveItem{
			CanonicalKey: cleanName,
			Size:         obj.Size,
			Updated:      obj.Updated,
			Status:       "Healthy",
		})
	}

	if lsInteractive && tui.IsTTY() {
		_, _ = tui.RunArchivePicker(items, cfg.Namespace)
		return nil
	}

	fmt.Printf("🦫 Castor · Archives in '%s' (%d archives):\n\n", dest.Name, len(rows))
	table := tui.RenderTable([]string{"Archive Key", "Size", "Last Pushed (UTC)", "Storage Tier"}, rows)
	fmt.Println(table)

	return nil
}
