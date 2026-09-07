package config

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryPathForConfig(t *testing.T) {
	tests := []struct {
		configPath string
		want       string
	}{
		{"/custom/dir/config.toml", "/custom/dir/history.json"},
		{"/root/castor/config.toml", "/root/castor/history.json"},
	}

	for _, tt := range tests {
		got := HistoryPathForConfig(tt.configPath)
		if got != tt.want {
			t.Errorf("HistoryPathForConfig(%q) = %q, want %q", tt.configPath, got, tt.want)
		}
	}

	// Empty configPath defaults to DefaultHistoryPath
	if got := HistoryPathForConfig(""); got != DefaultHistoryPath() {
		t.Errorf("HistoryPathForConfig(\"\") = %q, want %q", got, DefaultHistoryPath())
	}
}

func TestHistorySaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := filepath.Join(tmpDir, "history.json")

	// Loading non-existent history should return empty history without error
	h, err := LoadHistory(historyPath)
	if err != nil {
		t.Fatalf("LoadHistory on non-existent file failed: %v", err)
	}
	if len(h.Runs) != 0 {
		t.Errorf("expected empty runs, got %d", len(h.Runs))
	}

	now := time.Now().UTC().Truncate(time.Second)
	sampleRun := RunRecord{
		StartedAt:      now.Add(-time.Minute),
		FinishedAt:     now,
		Trigger:        "manual",
		TargetsTotal:   3,
		TargetsSynced:  2,
		TargetsSkipped: 1,
		TargetsFailed:  0,
		BytesStreamed:  1048576,
		Status:         "success",
	}

	h.Runs = append(h.Runs, sampleRun)
	if err := SaveHistory(historyPath, h); err != nil {
		t.Fatalf("SaveHistory failed: %v", err)
	}

	loaded, err := LoadHistory(historyPath)
	if err != nil {
		t.Fatalf("LoadHistory after save failed: %v", err)
	}
	if len(loaded.Runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(loaded.Runs))
	}

	got := loaded.Runs[0]
	if !got.StartedAt.Equal(sampleRun.StartedAt) || !got.FinishedAt.Equal(sampleRun.FinishedAt) {
		t.Errorf("timestamp mismatch: got %v - %v, want %v - %v", got.StartedAt, got.FinishedAt, sampleRun.StartedAt, sampleRun.FinishedAt)
	}
	if got.Status != "success" || got.BytesStreamed != 1048576 || got.TargetsSynced != 2 {
		t.Errorf("run record mismatch: %+v", got)
	}
}

func TestAppendRunAndTrimming(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := filepath.Join(tmpDir, "history.json")

	now := time.Now().UTC()

	// Append 60 runs (more than maxHistoryRuns = 50)
	for i := 0; i < 60; i++ {
		rec := RunRecord{
			StartedAt:     now.Add(time.Duration(i) * time.Minute),
			FinishedAt:    now.Add(time.Duration(i)*time.Minute + 30*time.Second),
			Trigger:       "scheduled",
			TargetsTotal:  5,
			TargetsSynced: 5,
			Status:        fmt.Sprintf("run-%d", i),
		}
		if err := AppendRun(historyPath, rec); err != nil {
			t.Fatalf("AppendRun iteration %d failed: %v", i, err)
		}
	}

	loaded, err := LoadHistory(historyPath)
	if err != nil {
		t.Fatalf("LoadHistory failed: %v", err)
	}

	if len(loaded.Runs) != maxHistoryRuns {
		t.Fatalf("expected %d runs (capped at maxHistoryRuns), got %d", maxHistoryRuns, len(loaded.Runs))
	}

	// First element should be run-10 (0..9 trimmed out)
	if loaded.Runs[0].Status != "run-10" {
		t.Errorf("expected first run to be 'run-10', got %q", loaded.Runs[0].Status)
	}
	// Last element should be run-59
	if loaded.Runs[len(loaded.Runs)-1].Status != "run-59" {
		t.Errorf("expected last run to be 'run-59', got %q", loaded.Runs[len(loaded.Runs)-1].Status)
	}
}
