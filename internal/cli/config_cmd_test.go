package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostamand/castor/internal/config"
)

func captureOutput(f func()) string {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	os.Stdout = oldStdout

	return buf.String()
}

func TestStringSlicesEqual(t *testing.T) {
	tests := []struct {
		a, b []string
		want bool
	}{
		{nil, nil, true},
		{[]string{}, []string{}, true},
		{[]string{"a", "b"}, []string{"a", "b"}, true},
		{[]string{"a", "b"}, []string{"a"}, false},
		{[]string{"a"}, []string{"a", "b"}, false},
		{[]string{"a", "b"}, []string{"b", "a"}, false},
	}

	for _, tt := range tests {
		got := stringSlicesEqual(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("stringSlicesEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestCompareConfigWarningsAndNotices(t *testing.T) {
	oldCfg := &config.Config{
		Namespace: "workstation",
		Performance: config.PerformanceConfig{
			MaxWorkers:       4,
			CompressionLevel: 19,
		},
		Security: config.SecurityConfig{
			Encrypt:       true,
			AgePublicKeys: []string{"age1publickeyA"},
		},
		Destinations: []config.DestinationConfig{
			{Name: "gcs-backup", Provider: "gcs", Bucket: "my-bucket"},
			{Name: "drive-backup", Provider: "gdrive", Folder: "Castor"},
		},
		Targets: []config.TargetConfig{
			{Name: "app-1", Path: "~/Work/app1"},
			{Name: "app-2", Path: "~/Work/app2"},
		},
	}

	newCfg := &config.Config{
		Namespace: "laptop", // Namespace changed!
		Performance: config.PerformanceConfig{
			MaxWorkers:       2,  // Workers changed!
			CompressionLevel: 10, // Compression changed!
		},
		Security: config.SecurityConfig{
			Encrypt:       true,
			AgePublicKeys: []string{"age1publickeyB"}, // Key changed!
		},
		Destinations: []config.DestinationConfig{
			{Name: "gcs-backup", Provider: "gcs", Bucket: "my-bucket"},
			// drive-backup removed!
			{Name: "local-nas", Provider: "local", Path: "/mnt/nas"}, // Added destination!
		},
		Targets: []config.TargetConfig{
			{Name: "app-1", Path: "~/Work/app1"},
			// app-2 removed!
			{Name: "app-3", Path: "~/Work/app3"}, // Added target!
		},
	}

	out := captureOutput(func() {
		compareConfig(oldCfg, newCfg)
	})

	// Check dangerous warnings
	if !strings.Contains(out, "Namespace changed: 'workstation' → 'laptop'") {
		t.Errorf("expected namespace change warning, got:\n%s", out)
	}
	if !strings.Contains(out, "Destination 'drive-backup' was removed") {
		t.Errorf("expected destination removed warning, got:\n%s", out)
	}
	if !strings.Contains(out, "Age public key changed") {
		t.Errorf("expected age public key warning, got:\n%s", out)
	}

	// Check non-dangerous notices
	if !strings.Contains(out, "Compression level changed: 19 → 10") {
		t.Errorf("expected compression level change notice, got:\n%s", out)
	}
	if !strings.Contains(out, "Workers changed: 4 → 2") {
		t.Errorf("expected workers change notice, got:\n%s", out)
	}
	if !strings.Contains(out, "Added destination 'local-nas' (local)") {
		t.Errorf("expected added destination notice, got:\n%s", out)
	}
	if !strings.Contains(out, "Added 1 new target(s)") {
		t.Errorf("expected added target notice, got:\n%s", out)
	}
	if !strings.Contains(out, "Removed 1 target(s)") {
		t.Errorf("expected removed target notice, got:\n%s", out)
	}
}

func TestCompareConfigEncryptionDisabled(t *testing.T) {
	oldCfg := &config.Config{
		Security: config.SecurityConfig{Encrypt: true, AgePublicKeys: []string{"key"}},
	}
	newCfg := &config.Config{
		Security: config.SecurityConfig{Encrypt: false},
	}

	out := captureOutput(func() {
		compareConfig(oldCfg, newCfg)
	})

	if !strings.Contains(out, "Encryption disabled") {
		t.Errorf("expected encryption disabled warning, got:\n%s", out)
	}
}

func TestRunConfigShow(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")

	cfg := config.DefaultConfig()
	cfg.Namespace = "unit-test-ns"
	cfg.Security.Encrypt = true
	cfg.Security.AgePublicKeys = []string{"age1publickeytest"}
	cfg.Destinations = []config.DestinationConfig{
		{Name: "test-gdrive", Provider: "gdrive", Folder: "TestFolder"},
	}
	cfg.Targets = []config.TargetConfig{
		{Name: "proj-alpha", Path: "~/proj/alpha", Type: "git"},
	}

	if err := config.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	out, err := executeCommand("config", "--config", cfgPath)
	if err != nil {
		t.Fatalf("executeCommand config failed: %v\nOutput: %s", err, out)
	}

	if !strings.Contains(out, "unit-test-ns") {
		t.Errorf("expected namespace in config output, got:\n%s", out)
	}
	if !strings.Contains(out, "test-gdrive") || !strings.Contains(out, "TestFolder") {
		t.Errorf("expected destination in config output, got:\n%s", out)
	}
	if !strings.Contains(out, "proj-alpha") {
		t.Errorf("expected target name in config output, got:\n%s", out)
	}
}
