package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/charmbracelet/lipgloss"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/sysinfo"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
)

var (
	addRecursive bool
	addGitOnly   bool
	addMaxDepth  int
	addPrefix    string
	addType      string
	addYes       bool
	addDryRun    bool
)

var addCmd = &cobra.Command{
	Use:   "add <path>",
	Short: "Interactively discover and register child folders into config.toml",
	Long: `Interactively inspects a directory tree, distinguishes Git repositories from generic folders,
checks for name collisions, and writes explicit target declarations directly into config.toml.`,
	Args: cobra.ExactArgs(1),
	RunE: runAdd,
}

func init() {
	addCmd.Flags().BoolVarP(&addRecursive, "recursive", "r", false, "Traverse subdirectories recursively to discover projects")
	addCmd.Flags().BoolVarP(&addGitOnly, "git-only", "g", false, "Restrict discovery strictly to Git repositories (.git directories)")
	addCmd.Flags().IntVar(&addMaxDepth, "max-depth", 0, "Maximum directory recursion depth (default: 1 without -r, 4 with -r)")
	addCmd.Flags().StringVar(&addPrefix, "prefix", "", "Prepend a prefix to discovered target names (e.g. 'work/')")
	addCmd.Flags().StringVar(&addType, "type", "generic", "Default target type for non-Git directories")
	addCmd.Flags().BoolVarP(&addYes, "yes", "y", false, "Non-interactive mode: add all valid discovered targets without prompt")
	addCmd.Flags().BoolVarP(&addDryRun, "dry-run", "n", false, "Print discovered targets without modifying config.toml")
}

func runAdd(cmd *cobra.Command, args []string) error {
	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config '%s': %w (run 'castor init' first)", configPath, err)
	}

	rawPath := args[0]
	absPath := sysinfo.ExpandHome(rawPath)
	absPath, err = filepath.Abs(absPath)
	if err != nil {
		return fmt.Errorf("invalid directory path: %w", err)
	}

	fi, err := os.Stat(absPath)
	if err != nil || !fi.IsDir() {
		return fmt.Errorf("path '%s' is not a valid directory", absPath)
	}

	depth := addMaxDepth
	if depth <= 0 {
		if addRecursive {
			depth = 4
		} else {
			depth = 1
		}
	}

	fmt.Printf("🦫 Castor · Scanning '%s' (namespace: %s, max-depth: %d)...\n", rawPath, cfg.Namespace, depth)

	candidates, err := ScanDirectory(absPath, addRecursive, addGitOnly, depth, addPrefix, addType, cfg.Targets)
	if err != nil {
		return fmt.Errorf("discovery scan failed: %w", err)
	}

	if len(candidates) == 0 {
		fmt.Println("No matching candidates found.")
		return nil
	}

	var confirmedTargets []tui.DiscoveredTarget

	if addDryRun {
		fmt.Printf("\n[DRY-RUN] Found %d candidate targets:\n", len(candidates))
		for _, c := range candidates {
			conflictNote := ""
			if c.Conflict {
				conflictNote = " [ALREADY REGISTERED]"
			}
			fmt.Printf("  • %-24s (%s · %s) -> %s%s\n", c.Name, c.Type, tui.FormatBytes(c.EstimatedBytes), c.Path, conflictNote)
		}
		return nil
	}

	if addYes || noTUI || !tui.IsTTY() {
		for _, c := range candidates {
			if !c.Conflict {
				confirmedTargets = append(confirmedTargets, c)
			}
		}
	} else {
		selected, confirmed, err := tui.RunChecklist(candidates, cfg.Namespace)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("Add cancelled. No changes written to config.")
			return nil
		}
		for _, c := range selected {
			if c.Selected && !c.Conflict {
				confirmedTargets = append(confirmedTargets, c)
			}
		}
	}

	if len(confirmedTargets) == 0 {
		fmt.Println("No targets selected to add.")
		return nil
	}

	if err := AppendTargetsToConfig(configPath, confirmedTargets); err != nil {
		return fmt.Errorf("failed to update config file: %w", err)
	}

	fmt.Printf("\n%s\n", lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render(
		fmt.Sprintf("✔ Added %d new target(s) to %s:", len(confirmedTargets), configPath),
	))
	for _, t := range confirmedTargets {
		fmt.Printf("  + %s (%s)\n", t.Name, t.Type)
	}
	fmt.Println("\nRun 'castor push -n' to inspect your backup plan.")

	return nil
}

// Build-junk directory names to skip during recursive scan
var buildJunkDirs = map[string]bool{
	"node_modules": true,
	".git":         true,
	".venv":        true,
	"venv":         true,
	"env":          true,
	"target":       true,
	"bin":          true,
	"obj":          true,
	".next":        true,
	".nuxt":        true,
	".turbo":       true,
	"dist":         true,
	"build":        true,
	"out":          true,
	"__pycache__":  true,
	".cache":       true,
	".terraform":   true,
}

// ScanDirectory discovers target candidates while respecting depth, symlinks, and exclusions
func ScanDirectory(
	parentPath string,
	recursive, gitOnly bool,
	maxDepth int,
	prefix, defaultType string,
	existingTargets []config.TargetConfig,
) ([]tui.DiscoveredTarget, error) {
	seenPaths := make(map[string]bool)
	seenNames := make(map[string]bool)
	for _, t := range existingTargets {
		clean := filepath.Clean(sysinfo.ExpandHome(t.Path))
		seenPaths[clean] = true
		if t.Name != "" {
			seenNames[t.Name] = true
		}
	}

	visitedInodes := make(map[uint64]bool)
	var candidates []tui.DiscoveredTarget

	err := filepath.WalkDir(parentPath, func(currentPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		// Don't register the root scan directory itself
		if currentPath == parentPath {
			return nil
		}

		if !d.IsDir() {
			return nil
		}

		name := d.Name()

		// Prune build junk
		if buildJunkDirs[name] {
			return filepath.SkipDir
		}

		// Skip hidden directories except when checking .git
		if strings.HasPrefix(name, ".") {
			return filepath.SkipDir
		}

		// Calculate relative depth
		rel, err := filepath.Rel(parentPath, currentPath)
		if err != nil {
			return nil
		}
		currentDepth := len(strings.Split(filepath.ToSlash(rel), "/"))
		if currentDepth > maxDepth {
			return filepath.SkipDir
		}

		// Prevent infinite recursion across symlink cycles
		if fi, err := os.Lstat(currentPath); err == nil {
			if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
				if visitedInodes[stat.Ino] {
					return filepath.SkipDir
				}
				visitedInodes[stat.Ino] = true
			}
		}

		// Check if current directory is a Git repository
		isGit := false
		if _, err := os.Stat(filepath.Join(currentPath, ".git")); err == nil {
			isGit = true
		}

		if gitOnly && !isGit {
			if !recursive {
				return nil
			}
			return nil // Continue walking child directories to search for Git repos
		}

		targetType := defaultType
		if isGit {
			targetType = "git"
		}

		targetName := prefix + name
		cleanCurrentPath := filepath.Clean(currentPath)
		conflict := seenPaths[cleanCurrentPath] || seenNames[targetName]
		conflictDetail := ""
		if conflict {
			conflictDetail = "already in config"
		}

		// Fast size estimate
		estimatedBytes := estimateDirBytes(currentPath)

		candidates = append(candidates, tui.DiscoveredTarget{
			Name:           targetName,
			Path:           cleanCurrentPath,
			Type:           targetType,
			EstimatedBytes: estimatedBytes,
			Selected:       !conflict,
			Conflict:       conflict,
			ConflictDetail: conflictDetail,
		})

		// If this is a Git repository and we found it, don't descend into its internals!
		if isGit {
			return filepath.SkipDir
		}

		if !recursive {
			return filepath.SkipDir
		}

		return nil
	})

	return candidates, err
}

func estimateDirBytes(dirPath string) int64 {
	var total int64
	filesChecked := 0
	_ = filepath.WalkDir(dirPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if buildJunkDirs[d.Name()] {
			return filepath.SkipDir
		}
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
		filesChecked++
		if filesChecked > 200 { // Fast estimate threshold
			return filepath.SkipAll
		}
		return nil
	})
	return total
}

// AppendTargetsToConfig appends selected targets safely to config.toml
func AppendTargetsToConfig(configPath string, newTargets []tui.DiscoveredTarget) error {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return err
	}

	for _, t := range newTargets {
		createBundle := (t.Type == "git")
		cfg.Targets = append(cfg.Targets, config.TargetConfig{
			Name:            t.Name,
			Path:            t.Path,
			Type:            t.Type,
			Compression:     "zstd",
			CreateGitBundle: createBundle,
		})
	}

	return config.SaveConfig(configPath, cfg)
}
