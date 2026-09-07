package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Namespace == "" {
		t.Errorf("DefaultConfig() namespace is empty")
	}
	if cfg.Performance.MaxWorkers < 2 {
		t.Errorf("DefaultConfig() max_workers = %d, expected >= 2", cfg.Performance.MaxWorkers)
	}
	if cfg.Performance.CompressionLevel != 19 {
		t.Errorf("DefaultConfig() compression_level = %d, expected 19", cfg.Performance.CompressionLevel)
	}
	if cfg.Safety.MaxArchiveSizeGB != 5.0 {
		t.Errorf("DefaultConfig() max_archive_size_gb = %f, expected 5.0", cfg.Safety.MaxArchiveSizeGB)
	}
	if len(cfg.Rules.Git.Excludes) == 0 {
		t.Errorf("DefaultConfig() git excludes is empty")
	}
}

func TestConfigSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	cfg := DefaultConfig()
	cfg.Namespace = "test-vault"
	cfg.Security.AgePublicKeys = []string{"age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"}
	cfg.Destinations = []DestinationConfig{
		{
			Name:     "gcp-coldline",
			Provider: "gcs",
			Bucket:   "test-bucket",
		},
	}
	cfg.Targets = []TargetConfig{
		{
			Name: "test-target",
			Path: "~/projects/test",
			Type: "git",
		},
	}

	if err := SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	loaded, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if loaded.Namespace != "test-vault" {
		t.Errorf("loaded.Namespace = %s, want test-vault", loaded.Namespace)
	}
	if len(loaded.Targets) != 1 || loaded.Targets[0].Name != "test-target" {
		t.Errorf("loaded.Targets mismatch: %+v", loaded.Targets)
	}
}

func TestConfigValidation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Namespace = ""
	if err := cfg.Validate(); err == nil {
		t.Errorf("Expected error for empty namespace, got nil")
	}

	cfg.Namespace = "valid"
	cfg.Security.Encrypt = true
	cfg.Security.AgePublicKeys = []string{}
	if err := cfg.Validate(); err == nil {
		t.Errorf("Expected error for encrypt=true with no age_public_keys, got nil")
	}

	cfg.Security.Encrypt = false
	cfg.Targets = []TargetConfig{
		{Name: "app", Path: "/path/one"},
		{Name: "app", Path: "/path/two"},
	}
	if err := cfg.Validate(); err == nil {
		t.Errorf("Expected error for duplicate target name 'app', got nil")
	}
}

func TestNormalizeTargetName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"MyProject", "my-project"},
		{"my_project", "my-project"},
		{"my project", "my-project"},
		{"generated-visions-workflows", "generated-visions-workflows"},
		{"HTMLParser", "html-parser"},
		{"myApp2", "my-app2"},
		{"My.Config.File", "my-config-file"},
		{"__leading__", "leading"},
		{"castor", "castor"},
		{"ALLCAPS", "allcaps"},
		{"App A", "app-a"},
		{"monster word lab", "monster-word-lab"},
		{"vo2", "vo2"},
		{"work/MyProject", "work/my-project"},
		{"git/castor/v1", "git/castor/v1"},
		{"/leading/slash/", "leading/slash"},
	}
	for _, tt := range tests {
		got := NormalizeTargetName(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeTargetName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestActiveDestinationsAndFindDestination(t *testing.T) {
	cfg := &Config{
		Namespace: "test-ns",
		Destinations: []DestinationConfig{
			{Name: "dest-active", Provider: "local", Path: "/tmp/active", Disabled: false},
			{Name: "dest-disabled", Provider: "dropbox", Disabled: true},
		},
	}

	active := cfg.ActiveDestinations()
	if len(active) != 1 {
		t.Fatalf("expected 1 active destination, got %d", len(active))
	}
	if active[0].Name != "dest-active" {
		t.Errorf("expected active destination 'dest-active', got %s", active[0].Name)
	}

	// Test FindDestination case-insensitive
	d, idx, found := cfg.FindDestination("DEST-DISABLED")
	if !found {
		t.Fatalf("expected to find DEST-DISABLED")
	}
	if idx != 1 {
		t.Errorf("expected index 1, got %d", idx)
	}
	if !d.Disabled {
		t.Errorf("expected destination to be disabled")
	}

	_, _, found = cfg.FindDestination("non-existent")
	if found {
		t.Errorf("expected not found for non-existent destination")
	}

	// Test persistence with SaveConfig / LoadConfig
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.toml")
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(loaded.ActiveDestinations()) != 1 {
		t.Errorf("loaded active destinations count = %d, want 1", len(loaded.ActiveDestinations()))
	}
	d2, _, ok := loaded.FindDestination("dest-disabled")
	if !ok || !d2.Disabled {
		t.Errorf("expected loaded dest-disabled to have Disabled = true")
	}
}
