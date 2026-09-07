package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostamand/castor/internal/config"
)

func setupTestConfig(t *testing.T) (string, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "castor-dest-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	cfgPath := filepath.Join(tmpDir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Namespace = "test-namespace"
	cfg.Security.Encrypt = false
	cfg.Destinations = []config.DestinationConfig{
		{
			Name:     "initial-local",
			Provider: "local",
			Path:     filepath.Join(tmpDir, "backups"),
		},
	}
	cfg.Targets = []config.TargetConfig{
		{
			Name: "my-target",
			Path: filepath.Join(tmpDir, "target"),
			Type: "generic",
		},
		{
			Name:         "routed-target",
			Path:         filepath.Join(tmpDir, "target-routed"),
			Type:         "generic",
			Destinations: []string{"initial-local"},
		},
	}

	if err := config.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	return cfgPath, cleanup
}

func TestDestinationList(t *testing.T) {
	testCfgPath, cleanup := setupTestConfig(t)
	defer cleanup()

	// Save global cfgPath and restore
	origCfgPath := cfgPath
	origJsonOut := jsonOut
	defer func() {
		cfgPath = origCfgPath
		jsonOut = origJsonOut
	}()

	cfgPath = testCfgPath
	jsonOut = false

	output := captureOutput(func() {
		err := runDestinationList(destinationListCmd, []string{})
		if err != nil {
			t.Errorf("runDestinationList returned error: %v", err)
		}
	})

	if !strings.Contains(output, "initial-local") {
		t.Errorf("expected output to contain 'initial-local', got: %s", output)
	}
	if !strings.Contains(output, "local") {
		t.Errorf("expected output to contain provider 'local', got: %s", output)
	}
	if !strings.Contains(output, "Storage Destinations") {
		t.Errorf("expected output to contain 'Storage Destinations', got: %s", output)
	}
}

func TestDestinationAddLocal(t *testing.T) {
	testCfgPath, cleanup := setupTestConfig(t)
	defer cleanup()

	origCfgPath := cfgPath
	origNoTUI := noTUI
	defer func() {
		cfgPath = origCfgPath
		noTUI = origNoTUI
	}()

	cfgPath = testCfgPath
	noTUI = true

	// 1. Add local destination with positional argument and flags
	newBackupDir := filepath.Join(filepath.Dir(testCfgPath), "new-backup-store")
	destAddName = "secondary-nas"
	destAddPath = newBackupDir
	destAddYes = true

	err := runDestinationAdd(destinationAddCmd, []string{"local"})
	if err != nil {
		t.Fatalf("runDestinationAdd failed: %v", err)
	}

	// Verify it was saved to config
	cfg, err := config.LoadConfig(testCfgPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	found := false
	for _, d := range cfg.Destinations {
		if d.Name == "secondary-nas" {
			found = true
			if d.Provider != "local" {
				t.Errorf("expected provider 'local', got '%s'", d.Provider)
			}
			if d.Path != newBackupDir {
				t.Errorf("expected path '%s', got '%s'", newBackupDir, d.Path)
			}
		}
	}
	if !found {
		t.Errorf("destination 'secondary-nas' not found in saved config")
	}

	// 2. Adding duplicate name should fail
	destAddName = "secondary-nas"
	destAddPath = newBackupDir
	destAddYes = true
	err = runDestinationAdd(destinationAddCmd, []string{"local"})
	if err == nil {
		t.Errorf("expected error when adding duplicate destination name, got nil")
	}
}

func TestDestinationAddGDriveAndGCS(t *testing.T) {
	testCfgPath, cleanup := setupTestConfig(t)
	defer cleanup()

	origCfgPath := cfgPath
	origNoTUI := noTUI
	defer func() {
		cfgPath = origCfgPath
		noTUI = origNoTUI
	}()

	cfgPath = testCfgPath
	noTUI = true

	// Add Google Drive destination
	destAddName = "my-gdrive"
	destAddFolder = "CustomCastorLodge"
	err := runDestinationAdd(destinationAddCmd, []string{"gdrive"})
	if err != nil {
		t.Fatalf("failed to add gdrive destination: %v", err)
	}

	// Add GCS destination
	destAddName = "my-coldline"
	destAddBucket = "test-coldline-bucket"
	destAddLocation = "us-east1"
	err = runDestinationAdd(destinationAddCmd, []string{"gcs"})
	if err != nil {
		t.Fatalf("failed to add gcs destination: %v", err)
	}

	cfg, err := config.LoadConfig(testCfgPath)
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}

	var hasGDrive, hasGCS bool
	for _, d := range cfg.Destinations {
		if d.Name == "my-gdrive" && d.Provider == "gdrive" && d.Folder == "CustomCastorLodge" {
			hasGDrive = true
		}
		if d.Name == "my-coldline" && d.Provider == "gcs" && d.Bucket == "test-coldline-bucket" && d.Location == "us-east1" {
			hasGCS = true
		}
	}

	if !hasGDrive {
		t.Errorf("my-gdrive destination not found or has incorrect parameters")
	}
	if !hasGCS {
		t.Errorf("my-coldline destination not found or has incorrect parameters")
	}
}

func TestDestinationAddDropbox(t *testing.T) {
	testCfgPath, cleanup := setupTestConfig(t)
	defer cleanup()

	origCfgPath := cfgPath
	origNoTUI := noTUI
	defer func() {
		cfgPath = origCfgPath
		noTUI = origNoTUI
	}()

	cfgPath = testCfgPath
	noTUI = true

	// Add Dropbox destination
	destAddName = "my-dropbox"
	destAddFolder = "MyCastorLodge"
	err := runDestinationAdd(destinationAddCmd, []string{"dropbox"})
	if err != nil {
		t.Fatalf("failed to add dropbox destination: %v", err)
	}

	cfg, err := config.LoadConfig(testCfgPath)
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}

	var found bool
	for _, d := range cfg.Destinations {
		if d.Name == "my-dropbox" && d.Provider == "dropbox" && d.Folder == "MyCastorLodge" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("my-dropbox destination not found or has incorrect parameters in config")
	}

	// Verify listing includes dropbox
	output := captureOutput(func() {
		_ = runDestinationList(destinationListCmd, []string{})
	})
	if !strings.Contains(output, "my-dropbox") {
		t.Errorf("expected destination list to contain 'my-dropbox', got: %s", output)
	}
	if !strings.Contains(output, "dropbox") {
		t.Errorf("expected destination list to contain 'dropbox', got: %s", output)
	}
}

func TestDestinationAddDropboxDefaultRoot(t *testing.T) {
	testCfgPath, cleanup := setupTestConfig(t)
	defer cleanup()

	origCfgPath := cfgPath
	origNoTUI := noTUI
	defer func() {
		cfgPath = origCfgPath
		noTUI = origNoTUI
	}()

	cfgPath = testCfgPath
	noTUI = true

	// Add Dropbox destination with NO folder specified (recommended root of app folder)
	destAddName = "root-dropbox"
	destAddFolder = ""
	err := runDestinationAdd(destinationAddCmd, []string{"dropbox"})
	if err != nil {
		t.Fatalf("failed to add default dropbox destination: %v", err)
	}

	cfg, err := config.LoadConfig(testCfgPath)
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}

	var found bool
	for _, d := range cfg.Destinations {
		if d.Name == "root-dropbox" && d.Provider == "dropbox" && d.Folder == "" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("root-dropbox destination not found with empty folder in config")
	}

	// Verify listing displays "root (app folder)"
	output := captureOutput(func() {
		_ = runDestinationList(destinationListCmd, []string{})
	})
	if !strings.Contains(output, "root (app folder)") {
		t.Errorf("expected destination list to contain 'root (app folder)', got: %s", output)
	}
}

func TestDestinationRemove(t *testing.T) {
	testCfgPath, cleanup := setupTestConfig(t)
	defer cleanup()

	origCfgPath := cfgPath
	origNoTUI := noTUI
	defer func() {
		cfgPath = origCfgPath
		noTUI = origNoTUI
	}()

	cfgPath = testCfgPath
	noTUI = true
	destRemoveYes = true

	// Remove nonexistent destination
	err := runDestinationRemove(destinationRemoveCmd, []string{"does-not-exist"})
	if err == nil {
		t.Errorf("expected error when removing nonexistent destination, got nil")
	}

	// Remove existing destination
	err = runDestinationRemove(destinationRemoveCmd, []string{"initial-local"})
	if err != nil {
		t.Fatalf("failed to remove existing destination: %v", err)
	}

	// Verify removal
	cfg, err := config.LoadConfig(testCfgPath)
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}

	for _, d := range cfg.Destinations {
		if d.Name == "initial-local" {
			t.Errorf("initial-local destination still present after removal")
		}
	}
}

func TestDestinationTestLocal(t *testing.T) {
	testCfgPath, cleanup := setupTestConfig(t)
	defer cleanup()

	origCfgPath := cfgPath
	origJsonOut := jsonOut
	defer func() {
		cfgPath = origCfgPath
		jsonOut = origJsonOut
	}()

	cfgPath = testCfgPath
	jsonOut = false

	output := captureOutput(func() {
		err := runDestinationTest(destinationTestCmd, []string{"initial-local"})
		if err != nil {
			t.Errorf("runDestinationTest returned error: %v", err)
		}
	})

	if !strings.Contains(output, "initial-local") {
		t.Errorf("expected output to contain 'initial-local', got: %s", output)
	}
	if !strings.Contains(output, "ONLINE") {
		t.Errorf("expected output to contain status 'ONLINE', got: %s", output)
	}
}
