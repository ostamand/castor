package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/ostamand/castor/internal/auth"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/storage"
	"github.com/ostamand/castor/internal/sysinfo"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
)

var (
	destAddName     string
	destAddPath     string
	destAddFolder   string
	destAddBucket   string
	destAddLocation string
	destAddPrefix   string
	destAddYes      bool

	destRemoveYes bool
)

// destinationCmd manages storage destinations / providers
var destinationCmd = &cobra.Command{
	Use:     "destination [command]",
	Aliases: []string{"dest", "provider", "providers", "destinations"},
	Short:   "Manage storage destinations and providers (Local, Google Drive, Dropbox, GCS)",
	Long: `Manage storage destinations where Castor streams encrypted archives.

Castor supports multiple simultaneous destinations via zero-disk fan-out streaming:
  • local:   Local directory, external NVMe/USB drive, or NFS/SMB NAS mount
  • gdrive:  Google Drive folder (personal or Google Workspace)
  • dropbox: Dropbox folder (personal or Team)
  • gcs:     Google Cloud Storage bucket (Standard, Nearline, Coldline, Archive)

You can invoke this command using 'castor destination' or 'castor provider'.`,
	Example: `  # List all configured destinations
  castor provider list
  castor destination

  # Add a local NAS or external drive
  castor provider add local /mnt/nas/castor --name local-nas
  castor provider add local ~/Backups/castor

  # Add a Google Drive destination
  castor provider add gdrive --folder CastorLodge --name gdrive-backup

  # Add a Dropbox destination
  castor provider add dropbox --folder CastorLodge --name dbx-backup

  # Add a Google Cloud Storage bucket
  castor provider add gcs --bucket my-castor-coldline --location northamerica-northeast1

  # Interactive wizard (prompts for provider, name, and details)
  castor provider add

  # Test connectivity and reachability
  castor provider test
  castor provider test local-nas

  # Remove a destination
  castor provider remove local-nas`,
	RunE: runDestinationList,
}

var destinationListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all configured storage destinations",
	RunE:    runDestinationList,
}

var destinationAddCmd = &cobra.Command{
	Use:   "add [provider] [location] [flags]",
	Short: "Add a new storage destination",
	Long: `Adds a new storage destination to ~/.config/castor/config.toml.

Supported providers:
  • local   - Local filesystem directory, external drive, or NAS mount
  • gdrive  - Google Drive folder
  • dropbox - Dropbox folder
  • gcs     - Google Cloud Storage bucket

If invoked without arguments, an interactive setup prompt will guide you.`,
	Example: `  castor provider add local /mnt/backup/castor --name nas-backup
  castor provider add local ~/Backups/castor
  castor provider add gdrive --folder CastorLodge --name google-drive
  castor provider add dropbox --folder CastorLodge --name my-dropbox
  castor provider add gcs --bucket my-backup-bucket --name gcs-coldline
  castor provider add`,
	Args: cobra.MaximumNArgs(2),
	RunE: runDestinationAdd,
}

var destinationRemoveCmd = &cobra.Command{
	Use:     "remove <name>",
	Aliases: []string{"rm", "delete"},
	Short:   "Remove a storage destination from configuration",
	Long:    "Removes a storage destination from ~/.config/castor/config.toml by name.",
	Example: `  castor provider remove local-nas
  castor provider rm google-drive -y`,
	Args: cobra.ExactArgs(1),
	RunE: runDestinationRemove,
}

var destinationTestCmd = &cobra.Command{
	Use:     "test [name]",
	Aliases: []string{"check", "ping"},
	Short:   "Test connectivity and permissions for storage destinations",
	Long:    "Tests reachability and read/write access for all destinations, or a single destination by name.",
	Example: `  castor provider test
  castor provider test local-nas`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDestinationTest,
}

func init() {
	destinationAddCmd.Flags().StringVarP(&destAddName, "name", "n", "", "Unique name for the destination (e.g. 'local-nas', 'gdrive')")
	destinationAddCmd.Flags().StringVarP(&destAddPath, "path", "p", "", "Directory path for local provider (e.g. /mnt/nas/castor)")
	destinationAddCmd.Flags().StringVarP(&destAddFolder, "folder", "f", "", "Folder name for Google Drive provider (default: 'CastorLodge')")
	destinationAddCmd.Flags().StringVarP(&destAddBucket, "bucket", "b", "", "Bucket name for Google Cloud Storage provider")
	destinationAddCmd.Flags().StringVarP(&destAddLocation, "location", "l", "northamerica-northeast1", "Storage region for GCS provider")
	destinationAddCmd.Flags().StringVar(&destAddPrefix, "prefix", "archives", "Object prefix for GCS provider")
	destinationAddCmd.Flags().BoolVarP(&destAddYes, "yes", "y", false, "Non-interactive mode: auto-confirm directory creation")

	destinationRemoveCmd.Flags().BoolVarP(&destRemoveYes, "yes", "y", false, "Skip confirmation prompt")

	destinationCmd.AddCommand(destinationListCmd)
	destinationCmd.AddCommand(destinationAddCmd)
	destinationCmd.AddCommand(destinationRemoveCmd)
	destinationCmd.AddCommand(destinationTestCmd)
}

// DestinationSummary holds output details for JSON or TUI listing
type DestinationSummary struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Location string `json:"location"`
	Targets  string `json:"targets"`
}

func runDestinationList(cmd *cobra.Command, args []string) error {
	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if len(cfg.Destinations) == 0 {
		if jsonOut {
			return json.NewEncoder(os.Stdout).Encode([]DestinationSummary{})
		}
		fmt.Println("No storage destinations configured in " + configPath)
		fmt.Println("Add one using: castor provider add [local|gdrive|gcs]")
		return nil
	}

	var summaries []DestinationSummary
	for _, dest := range cfg.Destinations {
		loc := formatDestinationLocation(dest)
		routing := computeTargetRouting(dest.Name, cfg)
		summaries = append(summaries, DestinationSummary{
			Name:     dest.Name,
			Provider: dest.Provider,
			Location: loc,
			Targets:  routing,
		})
	}

	if jsonOut {
		return json.NewEncoder(os.Stdout).Encode(summaries)
	}

	boldStyle := lipgloss.NewStyle().Bold(true).Foreground(tui.ColorAccent)
	fmt.Printf("%s · Storage Destinations (%s)\n\n", boldStyle.Render("🦫 Castor"), configPath)

	headers := []string{"Name", "Provider", "Target Location", "Active Targets"}
	var rows [][]string
	for _, s := range summaries {
		provBadge := s.Provider
		switch s.Provider {
		case "local", "fs", "file":
			provBadge = lipgloss.NewStyle().Foreground(tui.ColorSuccess).Render("local")
		case "gdrive":
			provBadge = lipgloss.NewStyle().Foreground(tui.ColorPrimary).Render("gdrive")
		case "dropbox", "dbx":
			provBadge = lipgloss.NewStyle().Foreground(tui.ColorAccent).Render("dropbox")
		case "gcs":
			provBadge = lipgloss.NewStyle().Foreground(tui.ColorWarning).Render("gcs")
		}

		rows = append(rows, []string{
			lipgloss.NewStyle().Bold(true).Render(s.Name),
			provBadge,
			s.Location,
			lipgloss.NewStyle().Foreground(tui.ColorSecondary).Render(s.Targets),
		})
	}

	fmt.Println(tui.RenderTable(headers, rows))
	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Faint(true).Render("💡 Test connectivity: castor provider test  •  Add destination: castor provider add"))

	return nil
}

func formatDestinationLocation(dest config.DestinationConfig) string {
	switch dest.Provider {
	case "local", "file", "fs":
		return dest.Path
	case "gdrive":
		folder := dest.Folder
		if folder == "" {
			folder = "CastorLodge"
		}
		return "folder: " + folder
	case "dropbox", "dbx":
		folder := dest.Folder
		if folder == "" {
			folder = "CastorLodge"
		}
		return "folder: " + folder
	case "gcs":
		loc := "bucket: " + dest.Bucket
		if dest.Location != "" {
			loc += fmt.Sprintf(" (%s)", dest.Location)
		}
		return loc
	default:
		if dest.Path != "" {
			return dest.Path
		}
		if dest.Bucket != "" {
			return "bucket: " + dest.Bucket
		}
		return "-"
	}
}

func computeTargetRouting(destName string, cfg *config.Config) string {
	if len(cfg.Targets) == 0 {
		return "0 targets"
	}

	specificCount := 0
	allCount := 0
	for _, t := range cfg.Targets {
		if len(t.Destinations) == 0 {
			allCount++
		} else {
			for _, d := range t.Destinations {
				if d == destName {
					specificCount++
					break
				}
			}
		}
	}

	total := specificCount + allCount
	if specificCount == 0 && allCount == len(cfg.Targets) {
		return fmt.Sprintf("all %d targets", total)
	}
	return fmt.Sprintf("%d of %d targets", total, len(cfg.Targets))
}

func runDestinationAdd(cmd *cobra.Command, args []string) error {
	defer func() {
		destAddName = ""
		destAddPath = ""
		destAddFolder = ""
		destAddBucket = ""
		destAddLocation = "northamerica-northeast1"
		destAddPrefix = "archives"
		destAddYes = false
	}()

	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	providerType := ""
	locationArg := ""
	if len(args) >= 1 {
		providerType = strings.ToLower(strings.TrimSpace(args[0]))
	}
	if len(args) >= 2 {
		locationArg = strings.TrimSpace(args[1])
	}

	// If no provider type given and not in quiet mode, launch interactive selection
	if providerType == "" {
		if noTUI {
			return fmt.Errorf("provider type required (local, gdrive, gcs). Example: castor provider add local /mnt/nas/castor")
		}

		selected, err := promptProviderType()
		if err != nil {
			return err
		}
		providerType = selected
	}

	// Normalize provider type
	switch providerType {
	case "local", "file", "fs", "disk", "nas":
		providerType = "local"
	case "gdrive", "drive", "google-drive", "googledrive":
		providerType = "gdrive"
	case "dropbox", "dbx":
		providerType = "dropbox"
	case "gcs", "google-cloud-storage", "cloud-storage":
		providerType = "gcs"
	default:
		return fmt.Errorf("unsupported provider '%s'. Supported providers: local, gdrive, dropbox, gcs", providerType)
	}

	var newDest config.DestinationConfig
	newDest.Provider = providerType

	switch providerType {
	case "local":
		path := destAddPath
		if path == "" {
			path = locationArg
		}
		if path == "" {
			if noTUI {
				return fmt.Errorf("flag --path or path argument is required for local provider")
			}
			path, err = promptLocalPath()
			if err != nil {
				return err
			}
		}

		cleanPath := filepath.Clean(sysinfo.ExpandHome(path))
		newDest.Path = path

		// Name determination
		name := destAddName
		if name == "" {
			base := filepath.Base(cleanPath)
			if base == "." || base == "/" || base == "" {
				name = "local-backup"
			} else {
				name = config.NormalizeTargetName(base)
			}
			newDest.Name = generateUniqueDestName(name, cfg)
		} else {
			newDest.Name = name
		}

		// Check directory existence
		if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
			createDir := destAddYes
			if !createDir && !noTUI {
				createDir, _ = tui.ConfirmPrompt(
					"Directory Does Not Exist",
					fmt.Sprintf("Directory %s does not exist yet. Create it now?", cleanPath),
					true,
				)
			}
			if createDir {
				if err := os.MkdirAll(cleanPath, 0755); err != nil {
					return fmt.Errorf("failed to create directory '%s': %w", cleanPath, err)
				}
				fmt.Printf("✔ Created directory: %s\n", cleanPath)
			}
		}

	case "gdrive":
		folder := destAddFolder
		if folder == "" {
			folder = locationArg
		}
		if folder == "" {
			folder = "CastorLodge"
		}
		newDest.Folder = folder

		name := destAddName
		if name == "" {
			newDest.Name = generateUniqueDestName("google-drive", cfg)
		} else {
			newDest.Name = name
		}

	case "dropbox":
		folder := destAddFolder
		if folder == "" {
			folder = locationArg
		}
		if folder == "" && !noTUI {
			folder, err = promptDropboxFolder()
			if err != nil {
				return err
			}
		}
		if folder == "" {
			folder = "CastorLodge"
		}
		newDest.Folder = folder

		name := destAddName
		if name == "" {
			newDest.Name = generateUniqueDestName("dropbox", cfg)
		} else {
			newDest.Name = name
		}

		if !auth.HasValidDropboxCredentials() && !noTUI {
			authNow, _ := tui.ConfirmPrompt(
				"Dropbox Not Authenticated",
				"No Dropbox credentials found. Would you like to log in via browser now?",
				true,
			)
			if authNow {
				appKey := auth.GetDropboxAppKey()
				if appKey == "" {
					fmt.Print("Enter your Dropbox App Key: ")
					fmt.Scanln(&appKey)
				}
				if appKey != "" {
					ctx := context.Background()
					if _, err := auth.DropboxLogin(ctx, appKey, false); err != nil {
						fmt.Printf("⚠️  Dropbox login failed: %v\n", err)
					}
				}
			}
		}

	case "gcs":
		bucket := destAddBucket
		if bucket == "" {
			bucket = locationArg
		}
		if bucket == "" {
			if noTUI {
				return fmt.Errorf("flag --bucket or bucket argument is required for gcs provider")
			}
			bucket, err = promptGCSBucket()
			if err != nil {
				return err
			}
		}
		newDest.Bucket = bucket
		newDest.Location = destAddLocation
		if newDest.Location == "" {
			newDest.Location = "northamerica-northeast1"
		}
		newDest.Prefix = destAddPrefix
		if newDest.Prefix == "" {
			newDest.Prefix = "archives"
		}

		name := destAddName
		if name == "" {
			newDest.Name = generateUniqueDestName("gcs-"+bucket, cfg)
		} else {
			newDest.Name = name
		}
	}

	// Check if name already exists
	for _, existing := range cfg.Destinations {
		if existing.Name == newDest.Name {
			return fmt.Errorf("destination '%s' already exists in config.toml. Specify a different name with --name", newDest.Name)
		}
	}

	cfg.Destinations = append(cfg.Destinations, newDest)

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	if err := config.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	successStyle := lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true)
	fmt.Println()
	fmt.Printf("%s Added destination '%s' (%s → %s)\n",
		successStyle.Render("✔"),
		newDest.Name,
		newDest.Provider,
		formatDestinationLocation(newDest),
	)

	if newDest.Provider == "gdrive" {
		ctx := context.Background()
		if _, err := auth.GetGoogleClientOptions(ctx); err != nil {
			fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorWarning).Render("💡 Notice: Remember to run 'castor auth gdrive' to connect your Google account."))
		}
	}

	fmt.Printf("💡 Run 'castor provider test %s' to verify access.\n", newDest.Name)
	return nil
}

func generateUniqueDestName(base string, cfg *config.Config) string {
	name := base
	existing := make(map[string]bool)
	for _, d := range cfg.Destinations {
		existing[d.Name] = true
	}

	if !existing[name] {
		return name
	}

	counter := 2
	for {
		candidate := fmt.Sprintf("%s-%d", base, counter)
		if !existing[candidate] {
			return candidate
		}
		counter++
	}
}

func promptProviderType() (string, error) {
	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select Storage Provider Type").
				Description("Choose the destination type for storing your encrypted archives:").
				Options(
					huh.NewOption("Local Filesystem / External Drive / NAS Mount", "local"),
					huh.NewOption("Google Drive (personal or Workspace)", "gdrive"),
					huh.NewOption("Dropbox (personal or Team)", "dropbox"),
					huh.NewOption("Google Cloud Storage (GCS Bucket)", "gcs"),
				).
				Value(&selected),
		),
	).WithTheme(tui.ThemeCastor())

	if err := form.Run(); err != nil {
		return "", err
	}
	return selected, nil
}

func promptDropboxFolder() (string, error) {
	folder := "CastorLodge"
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Dropbox Remote Folder").
				Description("Enter the folder inside Dropbox to store archives:").
				Placeholder("CastorLodge").
				Value(&folder),
		),
	).WithTheme(tui.ThemeCastor())

	if err := form.Run(); err != nil {
		return "", err
	}
	return folder, nil
}

func promptLocalPath() (string, error) {
	var path string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Local / NAS Folder Path").
				Description("Enter the destination path (e.g. /mnt/nas/castor, ~/Backups/castor):").
				Placeholder("~/Backups/castor").
				Value(&path).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("path cannot be empty")
					}
					return nil
				}),
		),
	).WithTheme(tui.ThemeCastor())

	if err := form.Run(); err != nil {
		return "", err
	}
	return path, nil
}

func promptGCSBucket() (string, error) {
	var bucket string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("GCS Bucket Name").
				Description("Enter the name of your Google Cloud Storage bucket:").
				Placeholder("my-castor-coldline").
				Value(&bucket).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("bucket name cannot be empty")
					}
					return nil
				}),
		),
	).WithTheme(tui.ThemeCastor())

	if err := form.Run(); err != nil {
		return "", err
	}
	return bucket, nil
}

func runDestinationRemove(cmd *cobra.Command, args []string) error {
	defer func() {
		destRemoveYes = false
	}()

	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	targetName := strings.TrimSpace(args[0])
	foundIdx := -1
	for i, d := range cfg.Destinations {
		if d.Name == targetName {
			foundIdx = i
			break
		}
	}

	if foundIdx == -1 {
		return fmt.Errorf("destination '%s' not found. Run 'castor provider list' to see available destinations", targetName)
	}

	// Warn if targets specifically route to this destination
	var affectedTargets []string
	for _, t := range cfg.Targets {
		for _, d := range t.Destinations {
			if d == targetName {
				affectedTargets = append(affectedTargets, t.Name)
				break
			}
		}
	}

	if len(affectedTargets) > 0 {
		fmt.Printf("⚠️  Notice: %d target(s) specifically route to this destination: %s\n",
			len(affectedTargets),
			strings.Join(affectedTargets, ", "),
		)
	}

	if !destRemoveYes && !noTUI {
		confirmed, err := tui.ConfirmPrompt(
			"Remove Destination",
			fmt.Sprintf("Are you sure you want to remove destination '%s' (%s)?", targetName, cfg.Destinations[foundIdx].Provider),
			false,
		)
		if err != nil || !confirmed {
			fmt.Println("Destination removal cancelled.")
			return nil
		}
	}

	cfg.Destinations = append(cfg.Destinations[:foundIdx], cfg.Destinations[foundIdx+1:]...)

	if err := config.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	fmt.Printf("%s Removed destination '%s' from config.toml\n",
		lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render("✔"),
		targetName,
	)
	return nil
}

type DestinationTestResult struct {
	Name     string        `json:"name"`
	Provider string        `json:"provider"`
	Status   string        `json:"status"`
	Latency  time.Duration `json:"latency_ms"`
	Details  string        `json:"details"`
}

func runDestinationTest(cmd *cobra.Command, args []string) error {
	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if len(cfg.Destinations) == 0 {
		return fmt.Errorf("no destinations configured in %s. Run 'castor provider add' to add one", configPath)
	}

	var toTest []config.DestinationConfig
	if len(args) > 0 {
		filterName := strings.TrimSpace(args[0])
		for _, d := range cfg.Destinations {
			if d.Name == filterName {
				toTest = append(toTest, d)
				break
			}
		}
		if len(toTest) == 0 {
			return fmt.Errorf("destination '%s' not found in configuration", filterName)
		}
	} else {
		toTest = cfg.Destinations
	}

	if !jsonOut {
		boldStyle := lipgloss.NewStyle().Bold(true).Foreground(tui.ColorAccent)
		fmt.Printf("%s · Testing %d destination(s)...\n\n", boldStyle.Render("🦫 Castor"), len(toTest))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var results []DestinationTestResult

	for _, dest := range toTest {
		start := time.Now()
		prov, err := storage.NewProviderFromConfig(ctx, dest)
		elapsed := time.Since(start)

		if err != nil {
			results = append(results, DestinationTestResult{
				Name:     dest.Name,
				Provider: dest.Provider,
				Status:   "FAIL",
				Latency:  elapsed,
				Details:  err.Error(),
			})
			continue
		}

		// Perform deep write/read probe
		status := "ONLINE"
		details := "Ready for backup streaming"

		if dest.Provider == "local" || dest.Provider == "fs" || dest.Provider == "file" {
			cleanPath := filepath.Clean(sysinfo.ExpandHome(dest.Path))
			probeFile := filepath.Join(cleanPath, ".castor-probe")
			if writeErr := os.WriteFile(probeFile, []byte("castor-probe"), 0600); writeErr != nil {
				status = "FAIL"
				details = fmt.Sprintf("Write check failed: %v", writeErr)
			} else {
				_ = os.Remove(probeFile)
				details = "Read/write verified"
			}
		} else {
			// Remote reachability check via List probe
			if _, listErr := prov.List(ctx, ""); listErr != nil {
				status = "FAIL"
				details = fmt.Sprintf("List probe failed: %v", listErr)
			} else {
				details = "Reachability confirmed"
			}
		}

		_ = prov.Close()
		results = append(results, DestinationTestResult{
			Name:     dest.Name,
			Provider: dest.Provider,
			Status:   status,
			Latency:  elapsed,
			Details:  details,
		})
	}

	if jsonOut {
		return json.NewEncoder(os.Stdout).Encode(results)
	}

	headers := []string{"Destination", "Provider", "Latency", "Health", "Probe Result"}
	var rows [][]string
	for _, res := range results {
		latencyStr := fmt.Sprintf("%dms", res.Latency.Milliseconds())
		badge := tui.BadgeStatus(res.Status)
		rows = append(rows, []string{
			lipgloss.NewStyle().Bold(true).Render(res.Name),
			res.Provider,
			latencyStr,
			badge,
			res.Details,
		})
	}

	fmt.Println(tui.RenderTable(headers, rows))
	return nil
}
