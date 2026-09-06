package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SyncState maintains local tracking of pushed targets and fingerprints
type SyncState struct {
	mu        sync.RWMutex           `json:"-"`
	Version   int                    `json:"version"`
	Namespace string                 `json:"namespace"`
	LastRun   time.Time              `json:"last_run"`
	Targets   map[string]TargetState `json:"targets"`
}

type TargetState struct {
	Fingerprint  string                      `json:"fingerprint"`
	LastPush     time.Time                   `json:"last_push"`
	Destinations map[string]DestinationState `json:"destinations"`
}

type DestinationState struct {
	Synced        bool   `json:"synced"`
	CipherBytes   int64  `json:"cipher_bytes"`
	ArchiveSHA256 string `json:"archive_sha256"`
}

// LoadState reads ~/.config/castor/state.json or returns an empty state
func LoadState(path string) (*SyncState, error) {
	if path == "" {
		path = DefaultStatePath()
	}

	state := &SyncState{
		Version: 1,
		Targets: make(map[string]TargetState),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return nil, fmt.Errorf("failed to read state file '%s': %w", path, err)
	}

	if err := json.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("failed to parse state JSON '%s': %w", path, err)
	}

	if state.Targets == nil {
		state.Targets = make(map[string]TargetState)
	}

	return state, nil
}

// SaveState writes ~/.config/castor/state.json atomically via temp file
func SaveState(path string, state *SyncState) error {
	if path == "" {
		path = DefaultStatePath()
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode state JSON: %w", err)
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return err
	}

	return os.Rename(tmpFile, path)
}

// GetTarget returns the TargetState for a given canonical key
func (s *SyncState) GetTarget(canonicalKey string) (TargetState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.Targets[canonicalKey]
	return t, ok
}

// SetTarget updates the TargetState for a given canonical key
func (s *SyncState) SetTarget(canonicalKey string, ts TargetState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Targets[canonicalKey] = ts
	s.LastRun = time.Now().UTC()
}

// DeleteTarget removes a target entry from state
func (s *SyncState) DeleteTarget(canonicalKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Targets, canonicalKey)
}
