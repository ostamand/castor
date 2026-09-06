package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Castor Test",
		"GIT_AUTHOR_EMAIL=test@castor.local",
		"GIT_COMMITTER_NAME=Castor Test",
		"GIT_COMMITTER_EMAIL=test@castor.local",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %s (%v)", args, string(out), err)
	}
}

func TestGitFingerprintEvolution(t *testing.T) {
	repoDir := t.TempDir()

	// 1. Init Git Repo
	runGitCmd(t, repoDir, "init")
	runGitCmd(t, repoDir, "config", "user.name", "Castor Test")
	runGitCmd(t, repoDir, "config", "user.email", "test@castor.local")

	// Empty repo state
	_, meta0, err := GitFingerprint(repoDir)
	if err != nil {
		t.Fatalf("GitFingerprint failed on empty repo: %v", err)
	}
	if meta0.Commit != "" {
		t.Errorf("expected empty commit, got %s", meta0.Commit)
	}

	// 2. Initial Commit
	f1 := filepath.Join(repoDir, "file1.txt")
	_ = os.WriteFile(f1, []byte("version 1"), 0644)
	runGitCmd(t, repoDir, "add", "file1.txt")
	runGitCmd(t, repoDir, "commit", "-m", "initial commit")

	fp1, meta1, err := GitFingerprint(repoDir)
	if err != nil {
		t.Fatalf("GitFingerprint failed on initial commit: %v", err)
	}
	if meta1.Commit == "" || meta1.Dirty || meta1.HasStash {
		t.Errorf("unexpected meta1 state: %+v", meta1)
	}

	// 3. Unchanged state: Fingerprint must be idempotent!
	fp2, _, _ := GitFingerprint(repoDir)
	if fp1 != fp2 {
		t.Errorf("fingerprint is not idempotent: %s != %s", fp1, fp2)
	}

	// 4. Modify file (unstaged dirty)
	_ = os.WriteFile(f1, []byte("version 2 (unstaged)"), 0644)
	fp3, meta3, err := GitFingerprint(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if fp3 == fp1 {
		t.Errorf("expected fingerprint to change on unstaged edit")
	}
	if !meta3.Dirty {
		t.Errorf("expected Dirty=true on unstaged edit")
	}

	// 5. Stage the modification
	runGitCmd(t, repoDir, "add", "file1.txt")
	fp4, meta4, err := GitFingerprint(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if fp4 == fp3 || fp4 == fp1 {
		t.Errorf("expected fingerprint to change on staged edit")
	}
	if !meta4.Dirty {
		t.Errorf("expected Dirty=true on staged edit")
	}

	// 6. Commit the staged change
	runGitCmd(t, repoDir, "commit", "-m", "second commit")
	fp5, meta5, err := GitFingerprint(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if fp5 == fp4 || fp5 == fp1 {
		t.Errorf("expected fingerprint to change on commit")
	}
	if meta5.Dirty {
		t.Errorf("expected Dirty=false after commit")
	}

	// 7. Add untracked file
	f2 := filepath.Join(repoDir, "untracked.txt")
	_ = os.WriteFile(f2, []byte("untracked content"), 0644)
	fp6, meta6, err := GitFingerprint(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if fp6 == fp5 {
		t.Errorf("expected fingerprint to change on untracked file")
	}
	if !meta6.Dirty {
		t.Errorf("expected Dirty=true with untracked file")
	}

	// 8. Stash changes
	runGitCmd(t, repoDir, "stash", "push", "-u", "-m", "temp stash")
	fp7, meta7, err := GitFingerprint(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if !meta7.HasStash {
		t.Errorf("expected HasStash=true after git stash")
	}
	if fp7 == fp5 {
		t.Errorf("expected fingerprint to change when stash is present")
	}
}

func TestDirectoryFingerprintDrift(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "data.bin")
	_ = os.WriteFile(f, []byte("initial data"), 0644)

	fp1, _, err := DirectoryFingerprint(dir, nil)
	if err != nil {
		t.Fatalf("DirectoryFingerprint failed: %v", err)
	}

	// Idempotent
	fp2, _, _ := DirectoryFingerprint(dir, nil)
	if fp1 != fp2 {
		t.Errorf("expected identical fingerprint on unchanged directory")
	}

	// Modify content & mtime
	time.Sleep(10 * time.Millisecond)
	_ = os.WriteFile(f, []byte("modified data bytes"), 0644)
	fp3, _, _ := DirectoryFingerprint(dir, nil)
	if fp3 == fp1 {
		t.Errorf("expected fingerprint to change when file is modified")
	}
}
