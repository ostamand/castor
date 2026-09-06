package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/storage"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %s: %v", args, string(out), err)
	}
}

func TestCompareTargetWithRemote(t *testing.T) {
	ctx := context.Background()

	// 1. Setup Age keys
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate identity: %v", err)
	}
	pubKey := id.Recipient().String()
	privKey := id.String()

	// 2. Setup Git target repository
	repoDir := t.TempDir()
	runGit(t, repoDir, "init", "-b", "main")
	testFile := filepath.Join(repoDir, "hello.txt")
	_ = os.WriteFile(testFile, []byte("hello world\n"), 0644)
	runGit(t, repoDir, "add", "hello.txt")
	runGit(t, repoDir, "commit", "-m", "initial commit")

	targetCfg := config.TargetConfig{
		Name:            "git-diff-repo",
		Path:            repoDir,
		Type:            "git",
		Compression:     "zstd",
		CreateGitBundle: true,
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

	memProv := storage.NewMemoryProvider("mem")
	providers := map[string]storage.Provider{"mem": memProv}

	// Case A: Target never pushed -> RemoteMissing
	diffNeverPushed, err := CompareTargetWithRemote(ctx, memProv, targetCfg, "workstation", privKey)
	if err != nil {
		t.Fatalf("CompareTargetWithRemote failed: %v", err)
	}
	if diffNeverPushed.Status != StatusRemoteMissing {
		t.Errorf("expected status %s, got %s", StatusRemoteMissing, diffNeverPushed.Status)
	}

	// Push archive to vault
	_, err = StreamArchive(ctx, targetCfg, cfg, providers, nil)
	if err != nil {
		t.Fatalf("StreamArchive failed: %v", err)
	}

	// Case B: Immediately after push -> Synced
	diffSynced, err := CompareTargetWithRemote(ctx, memProv, targetCfg, "workstation", privKey)
	if err != nil {
		t.Fatalf("CompareTargetWithRemote failed: %v", err)
	}
	if diffSynced.Status != StatusSynced {
		t.Errorf("expected status %s, got %s (summary: %s)", StatusSynced, diffSynced.Status, diffSynced.Summary)
	}
	if diffSynced.CommitsAhead != 0 || diffSynced.CommitsBehind != 0 {
		t.Errorf("expected 0 ahead / 0 behind, got %d ahead / %d behind", diffSynced.CommitsAhead, diffSynced.CommitsBehind)
	}

	// Case C: Uncommitted file added locally -> DirtyDrift
	uncommittedFile := filepath.Join(repoDir, "draft.txt")
	_ = os.WriteFile(uncommittedFile, []byte("wip"), 0644)

	diffDirty, err := CompareTargetWithRemote(ctx, memProv, targetCfg, "workstation", privKey)
	if err != nil {
		t.Fatalf("CompareTargetWithRemote failed: %v", err)
	}
	if diffDirty.Status != StatusDirtyDrift {
		t.Errorf("expected status %s, got %s", StatusDirtyDrift, diffDirty.Status)
	}
	if diffDirty.LocalUncommittedCount != 1 || !diffDirty.LocalDirty {
		t.Errorf("expected local uncommitted count 1 and dirty true, got %d and %v",
			diffDirty.LocalUncommittedCount, diffDirty.LocalDirty)
	}

	// Case D: Commit the uncommitted change -> LocalAhead
	runGit(t, repoDir, "add", "draft.txt")
	runGit(t, repoDir, "commit", "-m", "second commit")

	diffAhead, err := CompareTargetWithRemote(ctx, memProv, targetCfg, "workstation", privKey)
	if err != nil {
		t.Fatalf("CompareTargetWithRemote failed: %v", err)
	}
	if diffAhead.Status != StatusLocalAhead {
		t.Errorf("expected status %s, got %s", StatusLocalAhead, diffAhead.Status)
	}
	if diffAhead.CommitsAhead != 1 || diffAhead.CommitsBehind != 0 {
		t.Errorf("expected 1 ahead / 0 behind, got %d ahead / %d behind", diffAhead.CommitsAhead, diffAhead.CommitsBehind)
	}

	// Case E: Local directory removed from disk -> LocalMissing
	_ = os.RemoveAll(repoDir)
	diffMissing, err := CompareTargetWithRemote(ctx, memProv, targetCfg, "workstation", privKey)
	if err != nil {
		t.Fatalf("CompareTargetWithRemote failed: %v", err)
	}
	if diffMissing.Status != StatusLocalMissing {
		t.Errorf("expected status %s, got %s", StatusLocalMissing, diffMissing.Status)
	}
}
