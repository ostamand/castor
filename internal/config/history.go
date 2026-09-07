package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RunRecord captures the result of a single push run
type RunRecord struct {
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at"`
	Trigger        string    `json:"trigger"` // "manual", "scheduled"
	TargetsTotal   int       `json:"targets_total"`
	TargetsSynced  int       `json:"targets_synced"`
	TargetsSkipped int       `json:"targets_skipped"`
	TargetsFailed  int       `json:"targets_failed"`
	BytesStreamed   int64    `json:"bytes_streamed"`
	Errors         []string `json:"errors,omitempty"`
	Status         string   `json:"status"` // "success", "partial", "failed"
}

// RunHistory stores the last N run records
type RunHistory struct {
	Runs []RunRecord `json:"runs"`
}

const maxHistoryRuns = 50

// DefaultHistoryPath returns ~/.config/castor/history.json
func DefaultHistoryPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "castor", "history.json")
}

// HistoryPathForConfig returns history.json located alongside the given config.toml
func HistoryPathForConfig(configPath string) string {
	if configPath == "" {
		return DefaultHistoryPath()
	}
	return filepath.Join(filepath.Dir(configPath), "history.json")
}

// LoadHistory reads history.json or returns an empty history
func LoadHistory(path string) (*RunHistory, error) {
	if path == "" {
		path = DefaultHistoryPath()
	}

	history := &RunHistory{
		Runs: []RunRecord{},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return history, nil
		}
		return nil, fmt.Errorf("failed to read history file '%s': %w", path, err)
	}

	if err := json.Unmarshal(data, history); err != nil {
		return nil, fmt.Errorf("failed to parse history JSON '%s': %w", path, err)
	}

	return history, nil
}

// SaveHistory writes history.json atomically
func SaveHistory(path string, history *RunHistory) error {
	if path == "" {
		path = DefaultHistoryPath()
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode history JSON: %w", err)
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return err
	}

	return os.Rename(tmpFile, path)
}

// AppendRun adds a run record and trims history to maxHistoryRuns
func AppendRun(path string, record RunRecord) error {
	history, err := LoadHistory(path)
	if err != nil {
		history = &RunHistory{Runs: []RunRecord{}}
	}

	history.Runs = append(history.Runs, record)

	// Trim to keep only the latest runs
	if len(history.Runs) > maxHistoryRuns {
		history.Runs = history.Runs[len(history.Runs)-maxHistoryRuns:]
	}

	return SaveHistory(path, history)
}
