package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/storage"
)

func TestInspectAndCatArchive(t *testing.T) {
	ctx := context.Background()

	// 1. Generate Age keypair
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate identity: %v", err)
	}
	pubKey := id.Recipient().String()
	privKey := id.String()

	// 2. Setup source directory
	srcDir := t.TempDir()
	mainContent := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(srcDir, "main.go"), []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.go: %v", err)
	}
	subDir := filepath.Join(srcDir, "config")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to mkdir: %v", err)
	}
	tomlContent := "app_name = \"test\"\n"
	if err := os.WriteFile(filepath.Join(subDir, "app.toml"), []byte(tomlContent), 0644); err != nil {
		t.Fatalf("failed to write app.toml: %v", err)
	}

	// 3. Setup mock memory provider & config
	memProv := storage.NewMemoryProvider("mem")
	targetCfg := config.TargetConfig{
		Name:        "test-target",
		Path:        srcDir,
		Type:        "generic",
		Compression: "zstd",
	}

	cfg := &config.Config{
		Namespace: "workstation",
		Destinations: []config.DestinationConfig{
			{Name: "mem", Provider: "memory"},
		},
		Security: config.SecurityConfig{
			Encrypt:       true,
			AgePublicKeys: []string{pubKey},
		},
		Performance: config.PerformanceConfig{
			CompressionLevel: 19,
			MaxWorkers:       1,
		},
		Targets: []config.TargetConfig{targetCfg},
	}

	canonicalKey := config.CanonicalCloudKey("workstation", targetCfg.Name)
	providers := map[string]storage.Provider{"mem": memProv}

	// 4. Run StreamArchive into memProv
	metrics, err := StreamArchive(ctx, targetCfg, cfg, providers, nil)
	if err != nil {
		t.Fatalf("StreamArchive failed: %v", err)
	}
	if metrics == nil {
		t.Fatal("expected non-nil metrics")
	}

	// 5. Test InspectArchive
	entries, meta, err := InspectArchive(ctx, memProv, canonicalKey, privKey, "")
	if err != nil {
		t.Fatalf("InspectArchive failed: %v", err)
	}
	if meta == nil {
		t.Fatal("expected metadata to be parsed")
	}
	if meta.Namespace != "workstation" {
		t.Errorf("meta.Namespace = %s, want workstation", meta.Namespace)
	}

	// Verify entries contain main.go and config/app.toml
	foundMain := false
	foundToml := false
	for _, e := range entries {
		if e.Name == "main.go" {
			foundMain = true
			if e.Size != int64(len(mainContent)) {
				t.Errorf("main.go size = %d, want %d", e.Size, len(mainContent))
			}
		}
		if e.Name == "config/app.toml" {
			foundToml = true
			if e.Size != int64(len(tomlContent)) {
				t.Errorf("config/app.toml size = %d, want %d", e.Size, len(tomlContent))
			}
		}
	}
	if !foundMain {
		t.Error("expected main.go in archive entries")
	}
	if !foundToml {
		t.Error("expected config/app.toml in archive entries")
	}

	// 6. Test pattern filtering
	filtered, _, err := InspectArchive(ctx, memProv, canonicalKey, privKey, "*.toml")
	if err != nil {
		t.Fatalf("InspectArchive with pattern failed: %v", err)
	}
	if len(filtered) != 1 || filtered[0].Name != "config/app.toml" {
		t.Errorf("expected only config/app.toml to match *.toml, got %+v", filtered)
	}

	// 7. Test CatFileFromArchive for main.go
	var buf bytes.Buffer
	header, err := CatFileFromArchive(ctx, memProv, canonicalKey, "main.go", privKey, &buf)
	if err != nil {
		t.Fatalf("CatFileFromArchive main.go failed: %v", err)
	}
	if header.Name != "main.go" {
		t.Errorf("header.Name = %s, want main.go", header.Name)
	}
	if buf.String() != mainContent {
		t.Errorf("CatFileFromArchive content mismatch: got %q, want %q", buf.String(), mainContent)
	}

	// 8. Test CatFileFromArchive for nested file
	buf.Reset()
	_, err = CatFileFromArchive(ctx, memProv, canonicalKey, "config/app.toml", privKey, &buf)
	if err != nil {
		t.Fatalf("CatFileFromArchive config/app.toml failed: %v", err)
	}
	if buf.String() != tomlContent {
		t.Errorf("CatFileFromArchive content mismatch: got %q, want %q", buf.String(), tomlContent)
	}

	// 9. Test CatFileFromArchive for non-existent file
	buf.Reset()
	_, err = CatFileFromArchive(ctx, memProv, canonicalKey, "non-existent.txt", privKey, &buf)
	if err == nil {
		t.Fatal("expected error when catting non-existent file, got nil")
	}

	// 10. Test InspectArchive with bad key
	_, _, err = InspectArchive(ctx, memProv, canonicalKey, "AGE-SECRET-KEY-BADKEY", "")
	if err == nil {
		t.Fatal("expected error with bad secret key, got nil")
	}
}
