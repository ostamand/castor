package engine

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/crypto"
	"github.com/ostamand/castor/internal/storage"
)

type failingWriter struct{}

func (f *failingWriter) Write(p []byte) (int, error) {
	return 0, errors.New("insufficient_space (simulated quota exceeded)")
}

func (f *failingWriter) Close() error {
	return errors.New("insufficient_space on close")
}

type failingQuotaProvider struct {
	name string
}

func (f *failingQuotaProvider) Name() string { return f.name }
func (f *failingQuotaProvider) Type() string { return "dropbox" }
func (f *failingQuotaProvider) NewWriter(ctx context.Context, remotePath string) (io.WriteCloser, error) {
	return &failingWriter{}, nil
}
func (f *failingQuotaProvider) NewReader(ctx context.Context, remotePath string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}
func (f *failingQuotaProvider) List(ctx context.Context, prefix string) ([]storage.ObjectInfo, error) {
	return nil, nil
}
func (f *failingQuotaProvider) Delete(ctx context.Context, remotePath string) error {
	return nil
}
func (f *failingQuotaProvider) Close() error {
	return nil
}

func TestStreamArchivePartialDestinationResilience(t *testing.T) {
	ctx := context.Background()

	kp, err := crypto.GenerateKeypair()
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	testDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(testDir, "data.txt"), []byte("important code and assets"), 0644)

	targetCfg := config.TargetConfig{
		Name:        "test-target",
		Path:        testDir,
		Type:        "generic",
		Compression: "zstd",
	}

	cfg := &config.Config{
		Namespace: "workstation",
		Security: config.SecurityConfig{
			Encrypt:       true,
			AgePublicKeys: []string{kp.PublicKey},
		},
		Performance: config.PerformanceConfig{
			CompressionLevel: 3,
			MaxWorkers:       2,
		},
		Destinations: []config.DestinationConfig{
			{Name: "healthy-cloud", Provider: "memory"},
			{Name: "failing-dropbox", Provider: "dropbox"},
		},
	}

	healthyProv := storage.NewMemoryProvider("healthy-cloud")
	failingProv := &failingQuotaProvider{name: "failing-dropbox"}

	providers := map[string]storage.Provider{
		"healthy-cloud":   healthyProv,
		"failing-dropbox": failingProv,
	}

	// StreamArchive should succeed despite failing-dropbox because healthy-cloud succeeded
	res, err := StreamArchive(ctx, targetCfg, cfg, providers, nil)
	if err != nil {
		t.Fatalf("StreamArchive failed unexpectedly with partial destination failure: %v", err)
	}

	if res == nil {
		t.Fatalf("expected non-nil ArchiveResult")
	}

	// Verify failing-dropbox is recorded in DestErrors
	if len(res.DestErrors) == 0 {
		t.Fatalf("expected DestErrors to contain failing-dropbox, got empty")
	}
	if res.DestErrors["failing-dropbox"] == nil {
		t.Errorf("expected failing-dropbox error in DestErrors, got nil")
	}
	if res.DestErrors["healthy-cloud"] != nil {
		t.Errorf("expected healthy-cloud to have no error, got: %v", res.DestErrors["healthy-cloud"])
	}

	// Verify healthy-cloud received the archive and the metadata manifest
	objects, err := healthyProv.List(ctx, "")
	if err != nil {
		t.Fatalf("failed to list healthy provider: %v", err)
	}
	if len(objects) < 2 {
		t.Fatalf("expected healthy provider to have at least archive and manifest, got %d objects", len(objects))
	}

	// Check that total bytes were recorded
	if res.CipherBytes == 0 {
		t.Errorf("expected non-zero CipherBytes")
	}
}

func TestStreamArchiveAllDestinationsFail(t *testing.T) {
	ctx := context.Background()

	kp, err := crypto.GenerateKeypair()
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	testDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(testDir, "data.txt"), []byte("will fail everywhere"), 0644)

	targetCfg := config.TargetConfig{
		Name:        "fail-target",
		Path:        testDir,
		Type:        "generic",
		Compression: "zstd",
	}

	cfg := &config.Config{
		Namespace: "workstation",
		Security: config.SecurityConfig{
			Encrypt:       true,
			AgePublicKeys: []string{kp.PublicKey},
		},
		Performance: config.PerformanceConfig{
			CompressionLevel: 3,
			MaxWorkers:       2,
		},
		Destinations: []config.DestinationConfig{
			{Name: "dropbox-1", Provider: "dropbox"},
			{Name: "dropbox-2", Provider: "dropbox"},
		},
	}

	providers := map[string]storage.Provider{
		"dropbox-1": &failingQuotaProvider{name: "dropbox-1"},
		"dropbox-2": &failingQuotaProvider{name: "dropbox-2"},
	}

	// StreamArchive MUST return an error when ALL destinations fail
	res, err := StreamArchive(ctx, targetCfg, cfg, providers, nil)
	if err == nil {
		t.Fatalf("expected error when all destinations fail, got nil (res: %+v)", res)
	}
}
