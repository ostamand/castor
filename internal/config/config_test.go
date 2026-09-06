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
}
