package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/storage"
)

func TestMvTargetConfigAndState(t *testing.T) {
	tmp := t.TempDir()
	cfgPath = filepath.Join(tmp, "config.toml")
	defer func() { cfgPath = "" }()

	initCfg := config.DefaultConfig()
	initCfg.Namespace = "test-box"
	initCfg.Security.Encrypt = false
	initCfg.Targets = []config.TargetConfig{
		{
			Name: "modo",
			Path: filepath.Join(tmp, "modo"),
			Type: "git",
		},
		{
			Name: "other",
			Path: filepath.Join(tmp, "other"),
			Type: "git",
		},
	}
	if err := config.SaveConfig(cfgPath, initCfg); err != nil {
		t.Fatalf("failed saving test config: %v", err)
	}

	// Create test state
	statePath := config.StatePathForConfig(cfgPath)
	state := &config.SyncState{
		Version:   1,
		Namespace: "test-box",
		Targets: map[string]config.TargetState{
			"test-box/modo": {
				Fingerprint: "hash123",
				LastPush:    time.Now().UTC(),
			},
		},
	}
	if err := config.SaveState(statePath, state); err != nil {
		t.Fatalf("failed saving test state: %v", err)
	}

	// Run mv
	mvYes = true
	mvDryRun = false
	mvNoCloud = true

	if err := runMv(nil, []string{"modo", "git/modo"}); err != nil {
		t.Fatalf("runMv failed: %v", err)
	}

	// Verify config updated
	loadedCfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("failed loading updated config: %v", err)
	}

	foundNew := false
	for _, target := range loadedCfg.Targets {
		if target.Name == "modo" {
			t.Errorf("old target name 'modo' still present in config")
		}
		if target.Name == "git/modo" {
			foundNew = true
		}
	}
	if !foundNew {
		t.Errorf("new target name 'git/modo' not found in config")
	}

	// Verify state updated
	loadedState, err := config.LoadState(statePath)
	if err != nil {
		t.Fatalf("failed loading updated state: %v", err)
	}

	if _, exists := loadedState.Targets["test-box/modo"]; exists {
		t.Errorf("old state key 'test-box/modo' still exists")
	}
	newTargetState, exists := loadedState.Targets["test-box/git/modo"]
	if !exists {
		t.Errorf("new state key 'test-box/git/modo' does not exist")
	} else if newTargetState.Fingerprint != "hash123" {
		t.Errorf("fingerprint mismatch in migrated state: got %s, want hash123", newTargetState.Fingerprint)
	}
}

func TestMvTargetWithCloudArchives(t *testing.T) {
	tmp := t.TempDir()
	cfgPath = filepath.Join(tmp, "config.toml")
	vaultDir := filepath.Join(tmp, "vault")
	defer func() { cfgPath = "" }()

	initCfg := config.DefaultConfig()
	initCfg.Namespace = "test-box"
	initCfg.Security.Encrypt = false
	initCfg.Destinations = []config.DestinationConfig{
		{
			Name:     "local-vault",
			Provider: "local",
			Path:     vaultDir,
		},
	}
	initCfg.Targets = []config.TargetConfig{
		{
			Name: "modo",
			Path: filepath.Join(tmp, "modo"),
			Type: "git",
		},
	}
	if err := config.SaveConfig(cfgPath, initCfg); err != nil {
		t.Fatalf("failed saving test config: %v", err)
	}

	// Seed cloud objects using LocalProvider
	ctx := context.Background()
	prov, err := storage.NewLocalProvider("local-vault", vaultDir)
	if err != nil {
		t.Fatalf("failed initializing local provider: %v", err)
	}

	w1, err := prov.NewWriter(ctx, "test-box/modo.tar.zst")
	if err != nil {
		t.Fatalf("failed creating test archive object: %v", err)
	}
	_, _ = w1.Write([]byte("fake archive content"))
	_ = w1.Close()

	w2, err := prov.NewWriter(ctx, "test-box/modo.meta.json")
	if err != nil {
		t.Fatalf("failed creating test metadata object: %v", err)
	}
	_, _ = w2.Write([]byte("fake metadata content"))
	_ = w2.Close()
	_ = prov.Close()

	// Run mv
	mvYes = true
	mvDryRun = false
	mvNoCloud = false

	if err := runMv(nil, []string{"modo", "git/modo"}); err != nil {
		t.Fatalf("runMv with cloud migration failed: %v", err)
	}

	// Verify cloud storage objects
	provVerify, err := storage.NewLocalProvider("local-vault", vaultDir)
	if err != nil {
		t.Fatalf("failed re-opening local provider: %v", err)
	}
	defer provVerify.Close()

	objects, err := provVerify.List(ctx, "test-box")
	if err != nil {
		t.Fatalf("failed listing objects: %v", err)
	}

	objNames := make(map[string]bool)
	for _, o := range objects {
		objNames[strings.TrimPrefix(o.Name, "archives/")] = true
	}

	if objNames["test-box/modo.tar.zst"] {
		t.Errorf("old archive 'test-box/modo.tar.zst' was not deleted")
	}
	if objNames["test-box/modo.meta.json"] {
		t.Errorf("old metadata 'test-box/modo.meta.json' was not deleted")
	}

	if !objNames["test-box/git/modo.tar.zst"] {
		t.Errorf("new archive 'test-box/git/modo.tar.zst' not found, objects: %v", objNames)
	}
	if !objNames["test-box/git/modo.meta.json"] {
		t.Errorf("new metadata 'test-box/git/modo.meta.json' not found, objects: %v", objNames)
	}
}

func TestMvTargetCollisionAndNotFound(t *testing.T) {
	tmp := t.TempDir()
	cfgPath = filepath.Join(tmp, "config.toml")
	defer func() { cfgPath = "" }()

	initCfg := config.DefaultConfig()
	initCfg.Namespace = "test-box"
	initCfg.Security.Encrypt = false
	initCfg.Targets = []config.TargetConfig{
		{Name: "targetA", Path: filepath.Join(tmp, "targetA"), Type: "git"},
		{Name: "targetB", Path: filepath.Join(tmp, "targetB"), Type: "git"},
	}
	if err := config.SaveConfig(cfgPath, initCfg); err != nil {
		t.Fatalf("failed saving test config: %v", err)
	}

	mvYes = true
	mvDryRun = false

	// Target not found
	if err := runMv(nil, []string{"nonexistent", "new-name"}); err == nil {
		t.Errorf("expected error for non-existent target, got nil")
	}

	// Target collision
	if err := runMv(nil, []string{"targetA", "targetB"}); err == nil {
		t.Errorf("expected collision error renaming targetA to targetB, got nil")
	}

	// Dry run does not alter config
	mvDryRun = true
	if err := runMv(nil, []string{"targetA", "targetC"}); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}

	loaded, _ := config.LoadConfig(cfgPath)
	if loaded.Targets[0].Name != "targetA" {
		t.Errorf("dry-run modified config: got %s, want targetA", loaded.Targets[0].Name)
	}
}

func TestMvAlreadyRenamedRecovery(t *testing.T) {
	tmp := t.TempDir()
	cfgPath = filepath.Join(tmp, "config.toml")
	vaultDir := filepath.Join(tmp, "vault")
	defer func() { cfgPath = "" }()

	initCfg := config.DefaultConfig()
	initCfg.Namespace = "test-box"
	initCfg.Security.Encrypt = false
	initCfg.Destinations = []config.DestinationConfig{
		{
			Name:     "local-vault",
			Provider: "local",
			Path:     vaultDir,
		},
	}
	// Config already has the new target name (simulating an interrupted run)
	initCfg.Targets = []config.TargetConfig{
		{
			Name: "ai-images/castor",
			Path: filepath.Join(tmp, "castor"),
			Type: "generic",
		},
	}
	if err := config.SaveConfig(cfgPath, initCfg); err != nil {
		t.Fatalf("failed saving test config: %v", err)
	}

	// Seed cloud objects with OLD name
	ctx := context.Background()
	prov, err := storage.NewLocalProvider("local-vault", vaultDir)
	if err != nil {
		t.Fatalf("failed initializing local provider: %v", err)
	}
	w1, _ := prov.NewWriter(ctx, "test-box/images/castor.tar.zst")
	_, _ = w1.Write([]byte("archive"))
	_ = w1.Close()
	_ = prov.Close()

	mvYes = true
	mvDryRun = false
	mvNoCloud = false

	// Re-run mv with old-name new-name
	if err := runMv(nil, []string{"images/castor", "ai-images/castor"}); err != nil {
		t.Fatalf("expected recovery to succeed, got error: %v", err)
	}

	// Verify object moved to new name
	provVerify, _ := storage.NewLocalProvider("local-vault", vaultDir)
	defer provVerify.Close()
	objects, _ := provVerify.List(ctx, "test-box/ai-images/castor")
	if len(objects) != 1 {
		t.Errorf("expected 1 migrated object under test-box/ai-images/castor, got %d", len(objects))
	}
}
