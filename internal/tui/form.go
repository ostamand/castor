package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
)

// InitFormResult captures user choices from the castor init wizard
type InitFormResult struct {
	Namespace       string
	Providers       []string
	GCSBucket       string
	GCSLocation     string
	GDriveFolder    string
	GenerateAgeKey  bool
	ExistingPubKey  string
}

// RunInitForm launches an interactive setup wizard using Charm's Huh
func RunInitForm(defaultNamespace string) (*InitFormResult, error) {
	result := &InitFormResult{
		Namespace:    defaultNamespace,
		GCSLocation:  "northamerica-northeast1",
		GDriveFolder: "CastorLodge/archives",
		GenerateAgeKey: true,
	}

	theme := huh.ThemeCharm()

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
				Description("Where would you like to store your archives?").
				Options(
					huh.NewOption("Google Cloud Storage (GCS) — Fast, low-cost long-term cloud storage", "gcs").Selected(true),
					huh.NewOption("Google Drive — Back up directly to your personal or Workspace Drive", "gdrive").Selected(true),
					huh.NewOption("Amazon S3 & S3-Compatible — AWS, Cloudflare R2, MinIO, or Backblaze", "s3"),
				).
				Value(&result.Providers).
				Validate(func(v []string) error {
					if len(v) == 0 {
						return fmt.Errorf("please select at least one storage provider")
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
	for _, p := range result.Providers {
		if p == "gcs" {
			hasGCS = true
		}
		if p == "gdrive" {
			hasGDrive = true
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
		))
	}

	if hasGDrive {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().
				Title("Google Drive Folder").
				Description("Folder in your Google Drive where archives will be stored").
				Placeholder("CastorLodge/archives").
				Value(&result.GDriveFolder).
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
				huh.NewOption("I already have an Age public key (age1...)", "existing"),
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
					Title("Existing Age Public Key").
					Placeholder("age1...").
					Value(&result.ExistingPubKey).
					Validate(func(s string) error {
						if !strings.HasPrefix(strings.TrimSpace(s), "age1") {
							return fmt.Errorf("Age public key must begin with 'age1'")
						}
						return nil
					}),
			),
		).WithTheme(theme)
		if err := formKey.Run(); err != nil {
			return nil, err
		}
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
		WithTheme(huh.ThemeCharm()).
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
		WithTheme(huh.ThemeCharm()).
		Run()
	return choice, err
}
