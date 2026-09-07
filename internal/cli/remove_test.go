package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostamand/castor/internal/config"
)

func TestRemoveTargetByNameAndPath(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")

	cfg := config.DefaultConfig()
	cfg.Security.Encrypt = false
	cfg.Targets = []config.TargetConfig{
		{Name: "target-one", Path: "~/projects/one"},
		{Name: "target-two", Path: "~/projects/two"},
		{Name: "target-three", Path: "~/projects/three"},
	}

	if err := config.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// 1. Remove target-one by name using -y
	out, err := executeCommand("remove", "target-one", "-y", "--config", cfgPath)
	if err != nil {
		t.Fatalf("remove target-one failed: %v\nOutput: %s", err, out)
	}
	if !strings.Contains(out, "Removed target 'target-one'") {
		t.Errorf("expected confirmation of removal for target-one, got:\n%s", out)
	}

	// Verify target-one is gone from config
	updatedCfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(updatedCfg.Targets) != 2 {
		t.Fatalf("expected 2 targets left, got %d", len(updatedCfg.Targets))
	}
	if updatedCfg.Targets[0].Name != "target-two" || updatedCfg.Targets[1].Name != "target-three" {
		t.Errorf("unexpected remaining targets: %+v", updatedCfg.Targets)
	}

	// 2. Remove target-two by path using alias 'rm'
	outRm, err := executeCommand("rm", "~/projects/two", "-y", "--config", cfgPath)
	if err != nil {
		t.Fatalf("rm by path failed: %v\nOutput: %s", err, outRm)
	}
	if !strings.Contains(outRm, "Removed target 'target-two'") {
		t.Errorf("expected confirmation of removal for target-two, got:\n%s", outRm)
	}

	updatedCfg2, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(updatedCfg2.Targets) != 1 || updatedCfg2.Targets[0].Name != "target-three" {
		t.Errorf("expected only target-three left, got: %+v", updatedCfg2.Targets)
	}

	// 3. Remove non-existent target should fail with clear error
	outMissing, err := executeCommand("remove", "non-existent", "-y", "--config", cfgPath)
	if err == nil {
		t.Fatalf("expected error removing non-existent target, got output: %s", outMissing)
	}
	if !strings.Contains(outMissing, "not found in config") && !strings.Contains(err.Error(), "not found in config") {
		t.Errorf("expected 'not found in config' error, got err=%v out=%s", err, outMissing)
	}
}
