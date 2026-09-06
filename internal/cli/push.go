package cli

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/engine"
	"github.com/ostamand/castor/internal/storage"
	"github.com/ostamand/castor/internal/sysinfo"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
	"google.golang.org/api/option"
)

var (
	pushDryRun bool
	pushForce  bool
	pushWorkers int
)

var pushCmd = &cobra.Command{
	Use:     "push [target]",
	Aliases: []string{"lodge"},
	Short:   "Stream changed targets to configured cloud destinations",
	Long: `Captures, packages, encrypts, and streams explicit target workspaces directly
to all active cloud destinations under your vault namespace.`,
	RunE: runPush,
}

func init() {
	pushCmd.Flags().BoolVarP(&pushDryRun, "dry-run", "n", false, "Simulate push execution without sending data to cloud")
	pushCmd.Flags().BoolVarP(&pushForce, "force", "f", false, "Force push all targets, ignoring fingerprint cache")
	pushCmd.Flags().IntVarP(&pushWorkers, "workers", "w", 0, "Number of concurrent workers (default: from config)")
}

func runPush(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w (run 'castor init' first)", err)
	}

	state, err := config.LoadState(config.DefaultStatePath())
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	// Filter targets if argument specified
	targetsToProcess := cfg.Targets
	if len(args) > 0 {
		filter := stringsTrim(args[0])
		var filtered []config.TargetConfig
		for _, t := range cfg.Targets {
			key := config.CanonicalCloudKey(cfg.Namespace, t.Path, t.Namespace)
			if t.Name == filter || path.Base(key) == filter || filepath.Clean(t.Path) == filter {
				filtered = append(filtered, t)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("no target matching '%s' found in config", filter)
		}
		targetsToProcess = filtered
	}

	if len(targetsToProcess) == 0 {
		fmt.Println("No targets registered in config.toml. Run 'castor add <dir>' to register targets.")
		return nil
	}

	// Initialize cloud storage providers
	providers := make(map[string]storage.Provider)
	for _, dest := range cfg.Destinations {
		var p storage.Provider
		var initErr error

		switch dest.Provider {
		case "gcs":
			p, initErr = storage.NewGCSProvider(ctx, dest.Name, dest.Bucket, dest.Prefix)
		case "gdrive":
			credsPath := config.DefaultCredentialsPath()
			var opts []option.ClientOption
			if _, err := os.Stat(credsPath); err == nil {
				opts = append(opts, option.WithCredentialsFile(credsPath))
			}
			p, initErr = storage.NewGDriveProvider(ctx, dest.Name, dest.Folder, opts...)
		}

		if initErr != nil {
			return fmt.Errorf("failed to initialize provider '%s' (%s): %w", dest.Name, dest.Provider, initErr)
		}
		defer p.Close()
		providers[dest.Name] = p
	}

	// Fingerprint and identify changes
	type targetJob struct {
		target       config.TargetConfig
		canonicalKey string
		name         string
		changed      bool
		fingerprint  string
	}

	var jobs []targetJob
	var changedTargetNames []string

	for _, t := range targetsToProcess {
		key := config.CanonicalCloudKey(cfg.Namespace, t.Path, t.Namespace)
		name := t.Name
		if name == "" {
			name = path.Base(key)
		}

		absPath := sysinfo.ExpandHome(t.Path)
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			fmt.Printf("⚠️  Target '%s' missing locally (%s) · skipping\n", name, absPath)
			continue
		}

		var currentFingerprint string
		if t.Type == "git" {
			currentFingerprint, _, err = engine.GitFingerprint(absPath)
		} else {
			currentFingerprint, _, err = engine.DirectoryFingerprint(absPath, cfg.Rules.Generic.Excludes)
		}
		if err != nil {
			fmt.Printf("✖ Error checking target '%s': %v\n", name, err)
			continue
		}

		existingState, exists := state.GetTarget(key)
		changed := pushForce || !exists || existingState.Fingerprint != currentFingerprint

		jobs = append(jobs, targetJob{
			target:       t,
			canonicalKey: key,
			name:         name,
			changed:      changed,
			fingerprint:  currentFingerprint,
		})

		if changed {
			changedTargetNames = append(changedTargetNames, name)
		}
	}

	// Dry run mode
	if pushDryRun {
		fmt.Printf("🦫 Castor · Plan for '%s' (%d total targets):\n\n", cfg.Namespace, len(jobs))
		var rows [][]string
		for _, j := range jobs {
			action := lipgloss.NewStyle().Foreground(tui.ColorMuted).Render("Unchanged (skip)")
			if j.changed {
				action = lipgloss.NewStyle().Foreground(tui.ColorAccent).Bold(true).Render("Stream to Cloud")
			}
			rows = append(rows, []string{j.name, j.target.Type, j.canonicalKey, action})
		}
		fmt.Println(tui.RenderTable([]string{"Target", "Type", "Cloud Key", "Planned Action"}, rows))
		return nil
	}

	if len(changedTargetNames) == 0 {
		fmt.Println("✔ All targets are up to date. (0 B transferred)")
		return nil
	}

	// Concurrency workers setup
	numWorkers := cfg.Performance.MaxWorkers
	if pushWorkers > 0 {
		numWorkers = pushWorkers
	}
	if numWorkers > len(changedTargetNames) {
		numWorkers = len(changedTargetNames)
	}

	dashboard := tui.NewLiveDashboard(changedTargetNames)
	defer dashboard.Stop()

	jobsChan := make(chan targetJob, len(jobs))
	resultsChan := make(chan *engine.ArchiveResult, len(jobs))
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobsChan {
				res, err := engine.StreamArchive(ctx, job.target, cfg, providers, func(name string, bytes int64, done bool) {
					dashboard.Update(name, bytes, done, nil)
				})
				if err != nil {
					dashboard.Update(job.name, 0, true, err)
					resultsChan <- &engine.ArchiveResult{
						TargetName: job.name,
						CanonicalKey: job.canonicalKey,
						DestErrors: map[string]error{"stream": err},
					}
				} else {
					dashboard.Update(job.name, res.CipherBytes, true, nil)
					resultsChan <- res
				}
			}
		}()
	}

	for _, j := range jobs {
		if j.changed {
			jobsChan <- j
		}
	}
	close(jobsChan)

	wg.Wait()
	close(resultsChan)
	dashboard.Stop()

	// Process results & update state
	var totalTransferred int64
	var pushErrors []string

	for res := range resultsChan {
		if len(res.DestErrors) > 0 {
			for dest, destErr := range res.DestErrors {
				pushErrors = append(pushErrors, fmt.Sprintf("%s (%s): %v", res.TargetName, dest, destErr))
			}
			continue
		}

		totalTransferred += res.CipherBytes

		// Update state
		destStates := make(map[string]config.DestinationState)
		for _, d := range cfg.Destinations {
			destStates[d.Name] = config.DestinationState{
				Synced:        true,
				CipherBytes:   res.CipherBytes,
				ArchiveSHA256: res.ArchiveSHA256,
			}
		}
		state.SetTarget(res.CanonicalKey, config.TargetState{
			Fingerprint:  res.Fingerprint,
			LastPush:     time.Now().UTC(),
			Destinations: destStates,
		})
	}

	_ = config.SaveState(config.DefaultStatePath(), state)

	// Summary output
	fmt.Println()
	if len(pushErrors) > 0 {
		sysinfo.SendDesktopAlert("Castor Backup Alert", fmt.Sprintf("Backup completed with %d error(s). Run 'castor status'.", len(pushErrors)), true)
		return fmt.Errorf("backup completed with %d errors:\n  %s", len(pushErrors), fmt.Sprintf("%v", pushErrors))
	}

	fmt.Printf("%s Synced %d changed target(s) (%s total ciphertext) in %s\n",
		lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render("✔ Done!"),
		len(changedTargetNames),
		tui.FormatBytes(totalTransferred),
		time.Now().Format("15:04:05"),
	)

	return nil
}

func stringsTrim(s string) string {
	return filepath.Clean(s)
}
