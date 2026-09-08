package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/ostamand/castor/internal/crypto"
	"github.com/ostamand/castor/internal/sysinfo"
)

// InitFormResult captures user choices from the castor init wizard
type InitFormResult struct {
	Namespace          string
	Providers          []string
	GCSBucket          string
	GCSLocation        string
	GCSCredentialsFile string
	GDriveFolder       string
	DropboxFolder      string
	LocalPath          string
	GenerateAgeKey     bool
	ExistingPubKey     string
}

// RunInitForm launches an interactive setup wizard using Charm's Huh
func RunInitForm(defaultNamespace string) (*InitFormResult, error) {
	result := &InitFormResult{
		Namespace:      defaultNamespace,
		GCSLocation:    "northamerica-northeast1",
		GDriveFolder:   "CastorLodge",
		DropboxFolder:  "",
		LocalPath:      "~/Backups/castor",
		GenerateAgeKey: true,
	}

	theme := ThemeCastor()

	// Form 1: Namespace & Providers
	form1 := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Namespace").
				Description("A label to group and organize your archives (e.g. personal, work, lab)").
				Placeholder("workstation").
				Value(&result.Namespace).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if s == "" {
						return fmt.Errorf("namespace cannot be empty")
					}
					if strings.Contains(s, "/") {
						return fmt.Errorf("namespace cannot contain slashes")
					}
					return nil
				}),

			huh.NewMultiSelect[string]().
				Title("Storage Destinations").
				Description("Press [Space] to select one or more destinations to store your archives:").
				Options(
					huh.NewOption("Google Cloud Storage (GCS) — Fast, low-cost long-term cloud storage", "gcs"),
					huh.NewOption("Google Drive — Back up directly to your personal or Workspace Drive", "gdrive"),
					huh.NewOption("Dropbox — Back up directly to your personal or Team Dropbox", "dropbox"),
					huh.NewOption("Local or External Drive / NAS — Back up to a local directory, USB drive, or NAS", "local"),
				).
				Value(&result.Providers).
				Validate(func(v []string) error {
					if len(v) == 0 {
						return fmt.Errorf("please select at least one storage destination with [Space]")
					}
					return nil
				}),
		),
	).WithTheme(theme)

	if err := form1.Run(); err != nil {
		return nil, err
	}

	// Form 2: Provider Specifics
	var groups []*huh.Group

	hasGCS := false
	hasGDrive := false
	hasDropbox := false
	hasLocal := false
	for _, p := range result.Providers {
		if p == "gcs" {
			hasGCS = true
		}
		if p == "gdrive" {
			hasGDrive = true
		}
		if p == "dropbox" {
			hasDropbox = true
		}
		if p == "local" {
			hasLocal = true
		}
	}

	if hasGCS {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().
				Title("Google Cloud Storage Bucket").
				Description("Enter your bucket name (e.g. my-castor-backups)").
				Placeholder("my-castor-backups").
				Value(&result.GCSBucket).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("bucket name cannot be empty")
					}
					return nil
				}),
			huh.NewInput().
				Title("Storage Region").
				Description("Closest region for fast uploads (e.g. us-central1, northamerica-northeast1)").
				Value(&result.GCSLocation),
			huh.NewInput().
				Title("Service Account Key Path").
				Description("Path to your GCP Service Account JSON key file").
				Placeholder("~/.config/castor/gcs-key.json").
				Value(&result.GCSCredentialsFile).
				Validate(func(s string) error {
					trimmed := strings.TrimSpace(s)
					if trimmed == "" {
						return fmt.Errorf("service account key file is required for GCS")
					}
					expanded := sysinfo.ExpandHome(trimmed)
					if _, err := os.Stat(expanded); err != nil {
						return fmt.Errorf("file '%s' not found: %w", trimmed, err)
					}
					return nil
				}),
		))
	}

	if hasGDrive {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().
				Title("Google Drive Folder").
				Description("Folder in your Google Drive where archives will be stored").
				Placeholder("CastorLodge").
				Value(&result.GDriveFolder).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("folder path cannot be empty")
					}
					return nil
				}),
		))
	}

	if hasDropbox {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().
				Title("Dropbox Remote Subfolder (Optional)").
				Description("Dropbox App folder already isolates Castor. Press [Enter] for root, or specify a subfolder").
				Placeholder("(root of App folder)").
				Value(&result.DropboxFolder),
		))
	}

	if hasLocal {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().
				Title("Local or NAS Folder Path").
				Description("Directory path on your local disk, external drive, or NAS mount").
				Placeholder("~/Backups/castor or /mnt/backup").
				Value(&result.LocalPath).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("folder path cannot be empty")
					}
					return nil
				}),
		))
	}

	keyChoice := "generate"
	groups = append(groups, huh.NewGroup(
		huh.NewSelect[string]().
			Title("End-to-End Encryption").
			Description("Protect your archives so only you can unlock and read them").
			Options(
				huh.NewOption("Create a new secure encryption key for me (Recommended)", "generate"),
				huh.NewOption("I already have an encryption key (Secret or Public Key)", "existing"),
			).
			Value(&keyChoice),
	))

	form2 := huh.NewForm(groups...).WithTheme(theme)
	if err := form2.Run(); err != nil {
		return nil, err
	}

	result.GenerateAgeKey = (keyChoice == "generate")
	if !result.GenerateAgeKey {
		formKey := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Existing Encryption Key").
					Description("Paste your Secret Key (AGE-SECRET-KEY-1...) or Public Key (age1...)").
					Placeholder("AGE-SECRET-KEY-1... or age1...").
					Value(&result.ExistingPubKey).
					Validate(func(s string) error {
						_, err := crypto.ParseRecipientOrIdentity(s)
						return err
					}),
			),
		).WithTheme(theme)
		if err := formKey.Run(); err != nil {
			return nil, err
		}

		// Normalize to public key so only the public key is stored in configuration
		pubKey, err := crypto.ParseRecipientOrIdentity(result.ExistingPubKey)
		if err != nil {
			return nil, err
		}
		result.ExistingPubKey = pubKey
	}

	return result, nil
}

// ConfirmPrompt presents an interactive confirmation dialog
func ConfirmPrompt(title string, description string, defaultVal bool) (bool, error) {
	var confirmed bool = defaultVal
	err := huh.NewConfirm().
		Title(title).
		Description(description).
		Value(&confirmed).
		WithTheme(ThemeCastor()).
		Run()
	return confirmed, err
}

// ConfirmSecretKeySavedPrompt asks the user to confirm they have securely stored their secret key
func ConfirmSecretKeySavedPrompt() (bool, error) {
	var confirmed bool = false
	err := huh.NewConfirm().
		Title("Have you safely stored your Secret Key?").
		Description("Castor never saves this key to disk. You will need it to restore archives:").
		Affirmative("Yes, I've saved it").
		Negative("Wait, let me copy it").
		Value(&confirmed).
		WithTheme(ThemeCastor()).
		Run()
	return confirmed, err
}

// ConflictResolutionPrompt presents options for handling remote/local name collision
func ConflictResolutionPrompt(targetName, conflictDetail string) (int, error) {
	var choice int
	err := huh.NewSelect[int]().
		Title(fmt.Sprintf("Archive already exists for '%s'", targetName)).
		Description(conflictDetail).
		Options(
			huh.NewOption("1. Rename target or use a different namespace", 1),
			huh.NewOption("2. Overwrite existing cloud archive with latest version", 2),
			huh.NewOption("3. Cancel", 3),
		).
		Value(&choice).
		WithTheme(ThemeCastor()).
		Run()
	return choice, err
}
