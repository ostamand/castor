package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ostamand/castor/internal/config"
)

func TestScheduleHistoryEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")

	cfg := config.DefaultConfig()
	if err := config.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	out, err := executeCommand("schedule", "history", "--config", cfgPath)
	if err != nil {
		t.Fatalf("schedule history failed: %v\nOutput: %s", err, out)
	}

	if !strings.Contains(out, "No run history yet") {
		t.Errorf("expected 'No run history yet' in output, got:\n%s", out)
	}
}

func TestScheduleHistoryWithRuns(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")
	historyPath := filepath.Join(tmpDir, "history.json")

	cfg := config.DefaultConfig()
	if err := config.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	now := time.Now().UTC()

	// Append sample runs: success, partial, failed
	runs := []config.RunRecord{
		{
			StartedAt:      now.Add(-20 * time.Minute),
			FinishedAt:     now.Add(-18 * time.Minute),
			Trigger:        "manual",
			TargetsTotal:   5,
			TargetsSynced:  5,
			TargetsSkipped: 0,
			TargetsFailed:  0,
			BytesStreamed:  5000000,
			Status:         "success",
		},
		{
			StartedAt:      now.Add(-10 * time.Minute),
			FinishedAt:     now.Add(-9 * time.Minute),
			Trigger:        "scheduled",
			TargetsTotal:   5,
			TargetsSynced:  4,
			TargetsSkipped: 0,
			TargetsFailed:  1,
			BytesStreamed:  4000000,
			Errors:         []string{"target-x failed"},
			Status:         "partial",
		},
		{
			StartedAt:      now.Add(-2 * time.Minute),
			FinishedAt:     now.Add(-2 * time.Minute + 10*time.Second),
			Trigger:        "scheduled",
			TargetsTotal:   5,
			TargetsSynced:  0,
			TargetsSkipped: 0,
			TargetsFailed:  5,
			BytesStreamed:  0,
			Errors:         []string{"network offline"},
			Status:         "failed",
		},
	}

	for _, r := range runs {
		if err := config.AppendRun(historyPath, r); err != nil {
			t.Fatalf("AppendRun failed: %v", err)
		}
	}

	out, err := executeCommand("schedule", "history", "--config", cfgPath)
	if err != nil {
		t.Fatalf("schedule history failed: %v\nOutput: %s", err, out)
	}

	if !strings.Contains(out, "Run History") {
		t.Errorf("expected 'Run History' title, got:\n%s", out)
	}
	if !strings.Contains(out, "SUCCESS") {
		t.Errorf("expected 'SUCCESS' status in table, got:\n%s", out)
	}
	if !strings.Contains(out, "PARTIAL") {
		t.Errorf("expected 'PARTIAL' status in table, got:\n%s", out)
	}
	if !strings.Contains(out, "FAILED") {
		t.Errorf("expected 'FAILED' status in table, got:\n%s", out)
	}

	// Test line limit (-n 1)
	outLimit, err := executeCommand("schedule", "history", "-n", "1", "--config", cfgPath)
	if err != nil {
		t.Fatalf("schedule history -n 1 failed: %v\nOutput: %s", err, outLimit)
	}
	if !strings.Contains(outLimit, "FAILED") {
		t.Errorf("expected latest run (FAILED) in -n 1 output, got:\n%s", outLimit)
	}
	if strings.Contains(outLimit, "PARTIAL") {
		t.Errorf("expected earlier run (PARTIAL) to be omitted with -n 1, got:\n%s", outLimit)
	}
}

func TestScheduleLogsFallback(t *testing.T) {
	// Running schedule logs in a test environment where systemd user service may not be running
	// should either return without crashing or output fallback message
	out, err := executeCommand("schedule", "logs", "-n", "5")
	if err != nil {
		t.Fatalf("schedule logs command returned unexpected error: %v", err)
	}
	// It should execute gracefully
	_ = out
}
