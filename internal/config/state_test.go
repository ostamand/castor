package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStateSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	state, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState failed on empty: %v", err)
	}

	state.Namespace = "workstation"
	state.SetTarget("workstation/user/projects/rollmind", TargetState{
		Fingerprint: "abcdef123456",
		LastPush:    time.Now().UTC(),
		Destinations: map[string]DestinationState{
			"gcp-coldline": {
				Synced:        true,
				CipherBytes:   24582910,
				ArchiveSHA256: "e3b0c44298fc...",
			},
		},
	})

	if err := SaveState(statePath, state); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if loaded.Namespace != "workstation" {
		t.Errorf("loaded.Namespace = %s, want workstation", loaded.Namespace)
	}

	target, exists := loaded.GetTarget("workstation/user/projects/rollmind")
	if !exists {
		t.Fatalf("Target not found in loaded state")
	}
	if target.Fingerprint != "abcdef123456" {
		t.Errorf("Fingerprint = %s, want abcdef123456", target.Fingerprint)
	}
	if !target.Destinations["gcp-coldline"].Synced {
		t.Errorf("Destination not marked synced")
	}
}
