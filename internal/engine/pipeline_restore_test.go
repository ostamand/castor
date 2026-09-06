package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/crypto"
	"github.com/ostamand/castor/internal/storage"
)

func TestEndToEndPipelineAndRestore(t *testing.T) {
	ctx := context.Background()

	// 1. Generate Age encryption keypair
	kp, err := crypto.GenerateKeypair()
	if err != nil {
		t.Fatalf("failed generating Age keypair: %v", err)
	}

	// 2. Setup a realistic Git workspace with commits, stashes, and uncommitted edits
	sourceDir := t.TempDir()
	runGitCmd(t, sourceDir, "init")
	runGitCmd(t, sourceDir, "config", "user.name", "Castor Developer")
	runGitCmd(t, sourceDir, "config", "user.email", "dev@castor.local")

	// Commit 1
	mainGo := filepath.Join(sourceDir, "main.go")
	readme := filepath.Join(sourceDir, "README.md")
	_ = os.WriteFile(mainGo, []byte("package main\n\nfunc main() {}\n"), 0644)
	_ = os.WriteFile(readme, []byte("# My Core Project\n"), 0644)
	runGitCmd(t, sourceDir, "add", "main.go", "README.md")
	runGitCmd(t, sourceDir, "commit", "-m", "feat: initial commit")

	// Stash a WIP feature
	wipFile := filepath.Join(sourceDir, "wip.txt")
	_ = os.WriteFile(wipFile, []byte("experimental idea"), 0644)
	runGitCmd(t, sourceDir, "stash", "push", "-u", "-m", "wip-stash")

	// Commit 2
	featureGo := filepath.Join(sourceDir, "feature.go")
	_ = os.WriteFile(featureGo, []byte("package main\n\nfunc Feature() string { return \"ok\" }\n"), 0644)
	runGitCmd(t, sourceDir, "add", "feature.go")
	runGitCmd(t, sourceDir, "commit", "-m", "feat: add feature")

	// Uncommitted edits (both modified tracked file and new untracked file)
	modifiedMain := []byte("package main\n\n// Uncommitted modification\nfunc main() {}\n")
	_ = os.WriteFile(mainGo, modifiedMain, 0644)
	untrackedFile := filepath.Join(sourceDir, "config.local.json")
	untrackedContent := []byte("{\"local\": true, \"secret\": \"test-secret\"}")
	_ = os.WriteFile(untrackedFile, untrackedContent, 0644)

	// 3. Configure Castor
	targetCfg := config.TargetConfig{
		Name:            "my-core-project",
		Path:            sourceDir,
		Type:            "git",
		Compression:     "zstd",
		CreateGitBundle: true,
	}

	cfg := &config.Config{
		Namespace: "workstation",
		Security: config.SecurityConfig{
			Encrypt:       true,
			AgePublicKeys: []string{kp.PublicKey},
		},
		Performance: config.PerformanceConfig{
			CompressionLevel: 19,
			MaxWorkers:       2,
		},
		Destinations: []config.DestinationConfig{
			{Name: "mock-cloud", Provider: "memory"},
		},
	}

	memProv := storage.NewMemoryProvider("mock-cloud")
	providers := map[string]storage.Provider{
		"mock-cloud": memProv,
	}

	// 4. Run StreamArchive
	res, err := StreamArchive(ctx, targetCfg, cfg, providers, nil)
	if err != nil {
		t.Fatalf("StreamArchive failed: %v", err)
	}

	if res.Skipped {
		t.Fatalf("expected initial archive not to be skipped")
	}
	if res.UncompressedBytes == 0 || res.CipherBytes == 0 {
		t.Fatalf("expected positive byte counts, got uncompressed=%d, cipher=%d", res.UncompressedBytes, res.CipherBytes)
	}
	if res.ArchiveSHA256 == "" {
		t.Fatalf("expected non-empty ArchiveSHA256")
	}

	// Verify remote objects were created in memory provider
	objects, err := memProv.List(ctx, "")
	if err != nil {
		t.Fatalf("failed to list memory provider: %v", err)
	}
	if len(objects) < 2 {
		t.Fatalf("expected at least archive and sidecar manifest, got %d objects", len(objects))
	}

	var archiveObjName, metaObjName string
	for _, obj := range objects {
		if strings.HasSuffix(obj.Name, ".tar.zst.age") {
			archiveObjName = obj.Name
		}
		if strings.HasSuffix(obj.Name, ".meta.json.age") {
			metaObjName = obj.Name
		}
	}
	if archiveObjName == "" {
		t.Fatalf("archive object (.tar.zst.age) not found in storage")
	}
	if metaObjName == "" {
		t.Fatalf("sidecar manifest (.meta.json.age) not found in storage")
	}

	// 5. Test Decrypting Sidecar Metadata
	metaR, err := memProv.NewReader(ctx, metaObjName)
	if err != nil {
		t.Fatalf("failed to open metadata reader: %v", err)
	}
	sidecarMeta, err := ReadMetadata(metaR, kp.SecretKey, true)
	metaR.Close()
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}

	if sidecarMeta.Namespace != "workstation" {
		t.Errorf("metadata namespace mismatch: got %s", sidecarMeta.Namespace)
	}
	if sidecarMeta.Payload.ArchiveSHA256 != res.ArchiveSHA256 {
		t.Errorf("SHA256 mismatch between result and sidecar: %s vs %s", res.ArchiveSHA256, sidecarMeta.Payload.ArchiveSHA256)
	}
	if sidecarMeta.Git == nil || !sidecarMeta.Git.Dirty || !sidecarMeta.Git.HasStash {
		t.Errorf("git metadata not recorded properly in sidecar: %+v", sidecarMeta.Git)
	}

	// 6. Test Collision Protection on Restore
	restoreDir := t.TempDir()
	collisionFile := filepath.Join(restoreDir, "main.go")
	_ = os.WriteFile(collisionFile, []byte("existing collision content"), 0644)

	_, err = RestoreArchive(ctx, memProv, res.CanonicalKey, restoreDir, kp.SecretKey, false)
	if err == nil {
		t.Fatalf("expected RestoreArchive to fail due to pre-existing file without overwrite flag")
	}

	// 7. Test Successful Restore (with overwrite=true)
	restoreRes, err := RestoreArchive(ctx, memProv, res.CanonicalKey, restoreDir, kp.SecretKey, true)
	if err != nil {
		t.Fatalf("RestoreArchive failed: %v", err)
	}

	if !restoreRes.SHAMatched {
		t.Errorf("expected ciphertext SHA-256 to match sidecar manifest")
	}
	if !restoreRes.GitRestored {
		t.Errorf("expected Git repository to be reconstituted from bundle")
	}

	// 8. Verify Restored File Contents
	restoredMain, err := os.ReadFile(filepath.Join(restoreDir, "main.go"))
	if err != nil {
		t.Fatalf("restored main.go missing: %v", err)
	}
	if string(restoredMain) != string(modifiedMain) {
		t.Errorf("uncommitted modifications were not restored! Got:\n%s\nWant:\n%s", string(restoredMain), string(modifiedMain))
	}

	restoredUntracked, err := os.ReadFile(filepath.Join(restoreDir, "config.local.json"))
	if err != nil {
		t.Fatalf("untracked file was not restored: %v", err)
	}
	if string(restoredUntracked) != string(untrackedContent) {
		t.Errorf("untracked file content mismatch: %s", string(restoredUntracked))
	}

	// Verify temporary .castor directory was cleaned up
	if _, err := os.Stat(filepath.Join(restoreDir, ".castor")); !os.IsNotExist(err) {
		t.Errorf(".castor temporary bundle directory was not cleaned up after restore")
	}

	// 9. Verify Git Commit History in Restored Repo
	logCmd := exec.Command("git", "log", "--oneline")
	logCmd.Dir = restoreDir
	logOut, err := logCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log failed in restored repo: %s (%v)", string(logOut), err)
	}
	logStr := string(logOut)
	if !strings.Contains(logStr, "feat: initial commit") || !strings.Contains(logStr, "feat: add feature") {
		t.Errorf("git commit history was not reconstituted properly: %s", logStr)
	}

	// 10. Verify Git Stashes in Restored Repo
	stashCmd := exec.Command("git", "stash", "list")
	stashCmd.Dir = restoreDir
	stashOut, err := stashCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git stash list failed: %s (%v)", string(stashOut), err)
	}
	if !strings.Contains(string(stashOut), "wip-stash") {
		t.Errorf("stashes were not reconstituted into git repository: %s", string(stashOut))
	}

	// Recover the stash in restored repo to ensure 100% git object validity
	popCmd := exec.Command("git", "stash", "pop")
	popCmd.Dir = restoreDir
	popOut, err := popCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git stash pop failed in restored repo: %s (%v)", string(popOut), err)
	}
	recoveredWip, err := os.ReadFile(filepath.Join(restoreDir, "wip.txt"))
	if err != nil || string(recoveredWip) != "experimental idea" {
		t.Errorf("stash recovery failed: %s (%v)", string(recoveredWip), err)
	}
}
