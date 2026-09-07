package cli

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/engine"
	"github.com/ostamand/castor/internal/storage"
	"github.com/ostamand/castor/internal/sysinfo"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
)

var (
	pushDryRun bool
	pushForce  bool
	pushWorkers int
)

var pushCmd = &cobra.Command{
	Use:     "push [target]",
	Aliases: []string{"backup"},
	Short:   "Back up changed projects to your cloud storage",
	Long: `Packages, encrypts, and streams your project workspaces directly
to your configured cloud storage destinations.`,
	Example: `  castor push
  castor push myproject
  castor push -n
  castor push --force`,
	RunE: runPush,
}

func init() {
	pushCmd.Flags().BoolVarP(&pushDryRun, "dry-run", "n", false, "Simulate push execution without sending data to cloud")
	pushCmd.Flags().BoolVarP(&pushForce, "force", "f", false, "Force push all targets, ignoring fingerprint cache")
	pushCmd.Flags().IntVarP(&pushWorkers, "workers", "w", 0, "Number of concurrent workers (default: from config)")
}

func runPush(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	runStarted := time.Now().UTC()
	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w (run 'castor init' first)", err)
	}

	statePath := config.StatePathForConfig(configPath)
	state, err := config.LoadState(statePath)
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	// Filter targets if argument specified
	targetsToProcess := cfg.Targets
	if len(args) > 0 {
		filter := strings.TrimSpace(filepath.Clean(args[0]))
		var filtered []config.TargetConfig
		for _, t := range cfg.Targets {
			key := config.CanonicalCloudKey(cfg.Namespace, t.Name)
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
		p, initErr := storage.NewProviderFromConfig(ctx, dest)
		if initErr != nil {
			return fmt.Errorf("failed to initialize provider '%s' (%s): %w", dest.Name, dest.Provider, initErr)
		}
		defer p.Close()
		providers[dest.Name] = p
	}

	// Fingerprint and identify changes
	type targetJob struct {
		target               config.TargetConfig
		canonicalKey         string
		name                 string
		changed              bool
		changeReason         string
		destinationsToStream []string
		fingerprint          string
		totalBytes           int64
		packageBytes         int64
		excludeBytes         int64
		destinations         string
	}

	var jobs []targetJob
	var changedTargetNames []string

	for _, t := range targetsToProcess {
		key := config.CanonicalCloudKey(cfg.Namespace, t.Name)
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

		activeDestNames := targetActiveDestinationNames(t, cfg)
		existingState, exists := state.GetTarget(key)
		contentChanged := pushForce || !exists || existingState.Fingerprint != currentFingerprint

		var streamDestNames []string
		var changeReason string

		if contentChanged {
			streamDestNames = activeDestNames
			if !exists {
				changeReason = "New target"
			} else if pushForce {
				changeReason = "Force push"
			} else {
				changeReason = "Content modified"
			}
		} else {
			// Content unchanged: check if any active destination is not yet synced
			for _, dName := range activeDestNames {
				dState, dExists := existingState.Destinations[dName]
				if !dExists || !dState.Synced {
					streamDestNames = append(streamDestNames, dName)
				}
			}
			if len(streamDestNames) > 0 {
				changeReason = fmt.Sprintf("Sync to %s", strings.Join(streamDestNames, ", "))
			}
		}

		changed := len(streamDestNames) > 0
		sizeEst := engine.EstimateTargetSize(t, cfg.Rules)
		dests := resolveTargetDestinations(t, cfg)

		jobs = append(jobs, targetJob{
			target:               t,
			canonicalKey:         key,
			name:                 name,
			changed:              changed,
			changeReason:         changeReason,
			destinationsToStream: streamDestNames,
			fingerprint:          currentFingerprint,
			totalBytes:           sizeEst.TotalBytes,
			packageBytes:         sizeEst.PackagedBytes,
			excludeBytes:         sizeEst.ExcludedBytes,
			destinations:         dests,
		})

		if changed {
			changedTargetNames = append(changedTargetNames, name)
		}
	}

	// Dry run mode
	if pushDryRun {
		fmt.Printf("🦫 Castor · Plan for '%s' (%d total targets):\n\n", cfg.Namespace, len(jobs))
		var rows [][]string
		var toStreamCount int
		var toStreamPackageBytes int64
		var unchangedCount int

		for _, j := range jobs {
			action := lipgloss.NewStyle().Foreground(tui.ColorMuted).Render("Unchanged (skip)")
			if j.changed {
				targetDestDesc := strings.Join(j.destinationsToStream, ", ")
				if len(j.destinationsToStream) == len(cfg.Destinations) && len(cfg.Destinations) > 1 {
					targetDestDesc = "all destinations"
				}
				action = lipgloss.NewStyle().Foreground(tui.ColorAccent).Bold(true).Render("Stream to " + targetDestDesc)
				toStreamCount++
				toStreamPackageBytes += j.packageBytes
			} else {
				unchangedCount++
			}
			rows = append(rows, []string{
				j.name,
				j.target.Type,
				tui.FormatBytes(j.totalBytes),
				tui.FormatBytes(j.packageBytes),
				j.destinations,
				j.canonicalKey,
				action,
			})
		}
		fmt.Println(tui.RenderTable([]string{"Target", "Type", "On Disk", "To Archive", "Destination", "Cloud Key", "Planned Action"}, rows))
		fmt.Printf("\nPlan Summary: %d to stream (%s payload), %d unchanged\n",
			toStreamCount, tui.FormatBytes(toStreamPackageBytes), unchangedCount)
		tui.MaybePrintTip(cfg.AreTipsEnabled())
		return nil
	}

	if len(changedTargetNames) == 0 {
		fmt.Println("✔ All targets are up to date. (0 B transferred)")
		tui.MaybePrintTip(cfg.AreTipsEnabled())

		// Record up-to-date run
		historyPath := config.HistoryPathForConfig(configPath)
		trigger := "manual"
		if os.Getenv("CASTOR_SCHEDULED") == "1" {
			trigger = "scheduled"
		}
		_ = config.AppendRun(historyPath, config.RunRecord{
			StartedAt:      runStarted,
			FinishedAt:     time.Now().UTC(),
			Trigger:        trigger,
			TargetsTotal:   len(targetsToProcess),
			TargetsSkipped: len(targetsToProcess),
			Status:         "success",
		})

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

	targetTotals := make(map[string]int64)
	targetDestinations := make(map[string]string)
	for _, j := range jobs {
		if j.changed {
			targetTotals[j.name] = j.packageBytes
			targetDestinations[j.name] = strings.Join(j.destinationsToStream, ", ")
		}
	}

	dashboard := tui.NewLiveDashboard(changedTargetNames, targetTotals, targetDestinations)
	defer dashboard.Stop()

	jobsChan := make(chan targetJob, len(jobs))
	resultsChan := make(chan *engine.ArchiveResult, len(jobs))
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobsChan {
				jobTarget := job.target
				jobTarget.Destinations = job.destinationsToStream
				res, err := engine.StreamArchive(ctx, jobTarget, cfg, providers, func(name string, bytes int64, done bool) {
					dashboard.Update(name, bytes, done, nil)
				})
				if err != nil {
					dashboard.Update(job.name, 0, true, err)
					resultsChan <- &engine.ArchiveResult{
						TargetName:    job.name,
						CanonicalKey:  job.canonicalKey,
						DestErrors:    map[string]error{"stream": err},
						StreamedDests: job.destinationsToStream,
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

		// Update state preserving existing synced destinations
		existingState, exists := state.GetTarget(res.CanonicalKey)
		destStates := make(map[string]config.DestinationState)
		if exists && existingState.Destinations != nil {
			for k, v := range existingState.Destinations {
				destStates[k] = v
			}
		}

		for _, dName := range res.StreamedDests {
			if res.DestErrors[dName] == nil {
				destStates[dName] = config.DestinationState{
					Synced:        true,
					CipherBytes:   res.CipherBytes,
					ArchiveSHA256: res.ArchiveSHA256,
				}
			} else {
				destStates[dName] = config.DestinationState{
					Synced: false,
				}
			}
		}

		state.SetTarget(res.CanonicalKey, config.TargetState{
			Fingerprint:  res.Fingerprint,
			LastPush:     time.Now().UTC(),
			Destinations: destStates,
		})
	}

	_ = config.SaveState(statePath, state)

	// Summary output
	fmt.Println()
	if len(pushErrors) > 0 {
		sysinfo.SendDesktopAlert("Castor Backup Alert", fmt.Sprintf("Backup completed with %d error(s). Run 'castor status'.", len(pushErrors)), true)
		
		// Record failed/partial run in history
		historyPath := config.HistoryPathForConfig(configPath)
		trigger := "manual"
		if os.Getenv("CASTOR_SCHEDULED") == "1" {
			trigger = "scheduled"
		}
		runRecord := config.RunRecord{
			StartedAt:      runStarted,
			FinishedAt:     time.Now().UTC(),
			Trigger:        trigger,
			TargetsTotal:   len(targetsToProcess),
			TargetsSynced:  len(changedTargetNames) - len(pushErrors),
			TargetsSkipped: len(targetsToProcess) - len(changedTargetNames),
			TargetsFailed:  len(pushErrors),
			BytesStreamed:  totalTransferred,
			Errors:         pushErrors,
			Status:         "partial",
		}
		if len(changedTargetNames) == len(pushErrors) {
			runRecord.Status = "failed"
		}
		_ = config.AppendRun(historyPath, runRecord)

		return fmt.Errorf("backup completed with %d errors:\n  %s", len(pushErrors), fmt.Sprintf("%v", pushErrors))
	}

	fmt.Printf("%s Synced %d changed target(s) (%s total ciphertext) in %s\n",
		lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render("✔ Done!"),
		len(changedTargetNames),
		tui.FormatBytes(totalTransferred),
		time.Now().Format("15:04:05"),
	)

	tui.MaybePrintTip(cfg.AreTipsEnabled())

	// Record run in history
	historyPath := config.HistoryPathForConfig(configPath)
	trigger := "manual"
	if os.Getenv("CASTOR_SCHEDULED") == "1" {
		trigger = "scheduled"
	}
	runRecord := config.RunRecord{
		StartedAt:      runStarted,
		FinishedAt:     time.Now().UTC(),
		Trigger:        trigger,
		TargetsTotal:   len(targetsToProcess),
		TargetsSynced:  len(changedTargetNames),
		TargetsSkipped: len(targetsToProcess) - len(changedTargetNames),
		TargetsFailed:  0,
		BytesStreamed:  totalTransferred,
		Status:         "success",
	}
	_ = config.AppendRun(historyPath, runRecord)

	return nil
}

func resolveTargetDestinations(target config.TargetConfig, cfg *config.Config) string {
	if len(target.Destinations) > 0 {
		return strings.Join(target.Destinations, ", ")
	}
	var names []string
	for _, d := range cfg.Destinations {
		names = append(names, d.Name)
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

func targetActiveDestinationNames(target config.TargetConfig, cfg *config.Config) []string {
	if len(target.Destinations) > 0 {
		return target.Destinations
	}
	var names []string
	for _, d := range cfg.Destinations {
		names = append(names, d.Name)
	}
	return names
}
