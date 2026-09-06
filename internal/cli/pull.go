package cli

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/engine"
	"github.com/ostamand/castor/internal/storage"
	"github.com/ostamand/castor/internal/sysinfo"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"google.golang.org/api/option"
)

var (
	pullNamespace string
	pullToDir     string
	pullForce     bool
	pullDest      string
	pullSecretKey string
)

var pullCmd = &cobra.Command{
	Use:     "pull [target]",
	Aliases: []string{"retrieve"},
	Short:   "Decrypt and restore an archive from cloud storage",
	Long: `Streams down, decrypts in memory, and unpacks an archive into your workspace.
If the target is a Git repository, committed history, stashes, and working tree files are 100% reconstituted.`,
	RunE: runPull,
}

func init() {
	pullCmd.Flags().StringVarP(&pullNamespace, "namespace", "s", "", "Namespace to restore from (default: current vault namespace)")
	pullCmd.Flags().StringVar(&pullToDir, "to", "", "Restore into a custom destination directory instead of original path")
	pullCmd.Flags().BoolVarP(&pullForce, "force", "f", false, "Overwrite existing destination files without prompt")
	pullCmd.Flags().StringVar(&pullDest, "dest", "", "Specific destination provider to pull from (e.g. 'gcp-coldline')")
	pullCmd.Flags().StringVarP(&pullSecretKey, "key", "k", "", "Age private secret key (AGE-SECRET-KEY-1...) or set CASTOR_AGE_KEY")
}

func runPull(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	targetNamespace := cfg.Namespace
	if pullNamespace != "" {
		targetNamespace = pullNamespace
	}

	// Initialize selected destination provider
	if len(cfg.Destinations) == 0 {
		return fmt.Errorf("no cloud destinations configured in config.toml")
	}

	var activeDest *config.DestinationConfig
	for _, d := range cfg.Destinations {
		if pullDest == "" || d.Name == pullDest {
			activeDest = &d
			break
		}
	}
	if activeDest == nil {
		return fmt.Errorf("destination '%s' not found in config", pullDest)
	}

	var prov storage.Provider
	switch activeDest.Provider {
	case "gcs":
		prov, err = storage.NewGCSProvider(ctx, activeDest.Name, activeDest.Bucket, activeDest.Prefix)
	case "gdrive":
		credsPath := config.DefaultCredentialsPath()
		var opts []option.ClientOption
		if _, err := os.Stat(credsPath); err == nil {
			opts = append(opts, option.WithCredentialsFile(credsPath))
		}
		prov, err = storage.NewGDriveProvider(ctx, activeDest.Name, activeDest.Folder, opts...)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to provider '%s': %w", activeDest.Name, err)
	}
	defer prov.Close()

	var selectedTarget string
	if len(args) > 0 {
		selectedTarget = args[0]
	} else {
		// Launch interactive archive picker
		fmt.Printf("🦫 Castor · Browsing archives in '%s' (%s)...\n", activeDest.Name, targetNamespace)
		objects, err := prov.List(ctx, targetNamespace)
		if err != nil {
			return fmt.Errorf("failed to list remote archives: %w", err)
		}

		var items []tui.ArchiveItem
		for _, obj := range objects {
			if strings.HasSuffix(obj.Name, ".meta.json") || strings.HasSuffix(obj.Name, ".meta.json.age") {
				continue
			}
			cleanKey := strings.TrimPrefix(obj.Name, "archives/")
			cleanKey = strings.TrimSuffix(cleanKey, ".tar.zst.age")
			cleanKey = strings.TrimSuffix(cleanKey, ".tar.zst")
			cleanKey = strings.TrimSuffix(cleanKey, ".tar.xz.age")

			items = append(items, tui.ArchiveItem{
				CanonicalKey: cleanKey,
				ArchiveName:  path.Base(obj.Name),
				Namespace:    targetNamespace,
				Size:         obj.Size,
				Updated:      obj.Updated,
				Status:       "Healthy",
			})
		}

		if len(items) == 0 {
			fmt.Println("No archives found in remote vault.")
			return nil
		}

		chosen, err := tui.RunArchivePicker(items, targetNamespace)
		if err != nil || chosen == nil {
			fmt.Println("Restore cancelled.")
			return nil
		}
		selectedTarget = chosen.CanonicalKey
	}

	// Canonical key resolution
	canonicalKey := selectedTarget
	if !strings.HasPrefix(canonicalKey, targetNamespace+"/") {
		canonicalKey = path.Join(targetNamespace, canonicalKey)
	}

	// Determine restore destination directory
	destPath := pullToDir
	if destPath == "" {
		// Check if target is in local config
		for _, t := range cfg.Targets {
			tKey := config.CanonicalCloudKey(targetNamespace, t.Path, t.Namespace)
			if tKey == canonicalKey || path.Base(tKey) == path.Base(canonicalKey) {
				destPath = sysinfo.ExpandHome(t.Path)
				break
			}
		}
		if destPath == "" {
			// Restore relative to current directory
			destPath = filepath.Join(".", path.Base(canonicalKey))
		}
	}
	destPath, _ = filepath.Abs(destPath)

	// Local collision protection
	if _, err := os.Stat(destPath); err == nil && !pullForce {
		fmt.Printf("\n⚠️  Destination directory '%s' already exists!\n", destPath)
		action, err := tui.ConflictResolutionPrompt(path.Base(destPath), "Local directory already exists on disk")
		if err != nil || action == 3 {
			fmt.Println("Restore aborted to protect existing files.")
			return nil
		}
		if action == 1 {
			destPath = destPath + "-restored"
			fmt.Printf("Restoring to alternate directory: %s\n", destPath)
		}
	}

	// Prompt for Age secret key if needed
	secretKey := pullSecretKey
	if secretKey == "" {
		secretKey = os.Getenv("CASTOR_AGE_KEY")
	}
	if secretKey == "" && cfg.Security.Encrypt {
		fmt.Print("🔑 Enter Age Secret Key (AGE-SECRET-KEY-1...): ")
		keyBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("failed to read secret key: %w", err)
		}
		secretKey = strings.TrimSpace(string(keyBytes))
	}

	fmt.Printf("\n🦫 Castor · Restoring '%s' into '%s'...\n", canonicalKey, destPath)

	res, err := engine.RestoreArchive(ctx, prov, canonicalKey, destPath, secretKey, pullForce)
	if err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render("✔ Successfully restored!"))
	fmt.Printf("  • Destination: %s\n", res.DestDir)
	fmt.Printf("  • Files Unpacked: %d (%s)\n", res.FilesPushed, tui.FormatBytes(res.BytesPushed))
	fmt.Printf("  • Duration: %s\n", res.Duration.Round(time.Millisecond))
	if res.GitRestored {
		fmt.Printf("  • Git: %s\n", lipgloss.NewStyle().Foreground(tui.ColorAccent).Render("Reconstituted repository from bundle"))
	}
	if res.SHAMatched {
		fmt.Printf("  • Integrity: %s\n", lipgloss.NewStyle().Foreground(tui.ColorSuccess).Render("Ciphertext SHA-256 verified against sidecar"))
	}

	return nil
}
