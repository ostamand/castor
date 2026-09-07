package engine

import (
	"archive/tar"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/klauspost/compress/zstd"
)

func TestRestoreScript(t *testing.T) {
	// Find restore.sh relative to this test file
	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	restoreScript := filepath.Join(repoRoot, "restore.sh")
	if _, err := os.Stat(restoreScript); os.IsNotExist(err) {
		t.Fatalf("restore.sh not found at: %s", restoreScript)
	}

	// 1. Generate Age keypair
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate age key: %v", err)
	}
	pubKey := id.Recipient().String()
	privKey := id.String()

	// 2. Build mock archive in memory
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(sourceDir, "hello.txt"), []byte("hello castor restore"), 0644); err != nil {
		t.Fatalf("failed to write hello.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, ".env"), []byte("SECRET=castor123\n"), 0600); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}

	// Create git repo in sourceDir and create .castor/repo.bundle
	gitInit := exec.Command("git", "-C", sourceDir, "init")
	if err := gitInit.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	_ = exec.Command("git", "-C", sourceDir, "config", "user.name", "Castor Bot").Run()
	_ = exec.Command("git", "-C", sourceDir, "config", "user.email", "bot@castor.local").Run()
	_ = exec.Command("git", "-C", sourceDir, "add", ".").Run()
	_ = exec.Command("git", "-C", sourceDir, "commit", "-m", "initial commit").Run()

	castorMetaDir := filepath.Join(sourceDir, ".castor")
	_ = os.MkdirAll(castorMetaDir, 0755)
	bundlePath := filepath.Join(castorMetaDir, "repo.bundle")
	if err := exec.Command("git", "-C", sourceDir, "bundle", "create", bundlePath, "--all").Run(); err != nil {
		t.Fatalf("git bundle create failed: %v", err)
	}

	// Build tar -> zstd -> age encrypted archive
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)

	entries := []struct {
		relPath string
		content []byte
	}{
		{"hello.txt", []byte("hello castor restore")},
		{".env", []byte("SECRET=castor123\n")},
	}
	for _, e := range entries {
		hdr := &tar.Header{
			Name: e.relPath,
			Mode: 0644,
			Size: int64(len(e.content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header write failed: %v", err)
		}
		if _, err := tw.Write(e.content); err != nil {
			t.Fatalf("tar write failed: %v", err)
		}
	}

	// Add bundle
	bundleData, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatalf("failed to read bundle: %v", err)
	}
	hdr := &tar.Header{
		Name: filepath.Join(".castor", "repo.bundle"),
		Mode: 0644,
		Size: int64(len(bundleData)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar bundle header failed: %v", err)
	}
	if _, err := tw.Write(bundleData); err != nil {
		t.Fatalf("tar bundle write failed: %v", err)
	}
	_ = tw.Close()

	// Zstd compress
	var zstdBuf bytes.Buffer
	zw, err := zstd.NewWriter(&zstdBuf)
	if err != nil {
		t.Fatalf("zstd writer failed: %v", err)
	}
	if _, err := zw.ReadFrom(&tarBuf); err != nil {
		t.Fatalf("zstd compress failed: %v", err)
	}
	_ = zw.Close()

	// Age encrypt
	recipient, err := age.ParseX25519Recipient(pubKey)
	if err != nil {
		t.Fatalf("parse recipient failed: %v", err)
	}
	var ageBuf bytes.Buffer
	aw, err := age.Encrypt(&ageBuf, recipient)
	if err != nil {
		t.Fatalf("age encrypt failed: %v", err)
	}
	if _, err := aw.Write(zstdBuf.Bytes()); err != nil {
		t.Fatalf("age write failed: %v", err)
	}
	_ = aw.Close()

	archivePath := filepath.Join(tmpDir, "my-app.tar.zst.age")
	if err := os.WriteFile(archivePath, ageBuf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write test archive: %v", err)
	}

	// --- TEST 1: Single file extraction to stdout (-f .env) ---
	cmd1 := exec.Command("bash", restoreScript, "-k", privKey, "-f", ".env", archivePath)
	out1, err := cmd1.Output()
	if err != nil {
		t.Fatalf("restore.sh -f failed: %v", err)
	}
	if string(out1) != "SECRET=castor123\n" {
		t.Errorf("expected stdout 'SECRET=castor123\\n', got %q", string(out1))
	}

	// --- TEST 2: List contents (-l) ---
	cmd2 := exec.Command("bash", restoreScript, "-k", privKey, "-l", archivePath)
	out2, err := cmd2.Output()
	if err != nil {
		t.Fatalf("restore.sh -l failed: %v", err)
	}
	if !strings.Contains(string(out2), "hello.txt") || !strings.Contains(string(out2), ".env") {
		t.Errorf("expected archive list to contain hello.txt and .env, got: %s", string(out2))
	}

	// --- TEST 3: Full restore into directory ---
	destDir := filepath.Join(tmpDir, "restored-app")
	cmd3 := exec.Command("bash", restoreScript, "-k", privKey, archivePath, destDir)
	out3, err := cmd3.CombinedOutput()
	if err != nil {
		t.Fatalf("restore.sh failed: %v\nOutput: %s", err, string(out3))
	}

	// Verify extracted files
	helloContent, err := os.ReadFile(filepath.Join(destDir, "hello.txt"))
	if err != nil || string(helloContent) != "hello castor restore" {
		t.Errorf("expected 'hello castor restore', got %q (err: %v)", string(helloContent), err)
	}

	// Verify git reconstitution
	gitLog := exec.Command("git", "-C", destDir, "log", "-1", "--oneline")
	gitLogOut, err := gitLog.Output()
	if err != nil || !strings.Contains(string(gitLogOut), "initial commit") {
		t.Errorf("expected git log to contain 'initial commit', got: %s (err: %v)", string(gitLogOut), err)
	}

	// Verify temporary .castor was cleaned up
	if _, err := os.Stat(filepath.Join(destDir, ".castor")); !os.IsNotExist(err) {
		t.Errorf("expected .castor directory to be cleaned up after git reconstitution")
	}
}
