package cli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/crypto"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Castor CI",
		"GIT_AUTHOR_EMAIL=ci@castor.local",
		"GIT_COMMITTER_NAME=Castor CI",
		"GIT_COMMITTER_EMAIL=ci@castor.local",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s failed: %s (%v)", args, dir, string(out), err)
	}
}

func executeCommand(args ...string) (string, error) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w

	var cmdBuf bytes.Buffer
	RootCmd.SetOut(&cmdBuf)
	RootCmd.SetErr(&cmdBuf)
	RootCmd.SetArgs(args)

	execErr := RootCmd.Execute()

	_ = w.Close()
	var pipeBuf bytes.Buffer
	_, _ = io.Copy(&pipeBuf, r)
	_ = r.Close()
	os.Stdout = oldStdout

	return pipeBuf.String() + cmdBuf.String(), execErr
}

func TestFullCLIIntegrationLifecycle(t *testing.T) {
	testEnv := t.TempDir()

	// 1. Setup isolated environment
	cfgDir := filepath.Join(testEnv, "config")
	vaultDir := filepath.Join(testEnv, "vault")
	workDir := filepath.Join(testEnv, "workspace")
	restoreDir := filepath.Join(testEnv, "restored")

	_ = os.MkdirAll(cfgDir, 0755)
	_ = os.MkdirAll(vaultDir, 0755)
	_ = os.MkdirAll(workDir, 0755)
	_ = os.MkdirAll(restoreDir, 0755)

	configPath := filepath.Join(cfgDir, "config.toml")
	cfgPath = configPath // Point global persistent flag to isolated config

	// Generate Age keypair
	kp, err := crypto.GenerateKeypair()
	if err != nil {
		t.Fatalf("failed generating keypair: %v", err)
	}

	// 2. Setup targets in workDir:
	// - app-a: Git repo with commit, stash, and untracked file
	// - docs: generic folder with markdown
	// - node_modules: junk folder (must be pruned)
	appADir := filepath.Join(workDir, "app-a")
	_ = os.MkdirAll(appADir, 0755)
	runGit(t, appADir, "init")
	runGit(t, appADir, "config", "user.name", "Castor CI")
	runGit(t, appADir, "config", "user.email", "ci@castor.local")

	appMain := filepath.Join(appADir, "main.go")
	_ = os.WriteFile(appMain, []byte("package main\n\nfunc main() {}\n"), 0644)
	runGit(t, appADir, "add", "main.go")
	runGit(t, appADir, "commit", "-m", "initial commit")

	appStash := filepath.Join(appADir, "stash.txt")
	_ = os.WriteFile(appStash, []byte("stashed content"), 0644)
	runGit(t, appADir, "stash", "push", "-u", "-m", "wip-feature")

	appUntracked := filepath.Join(appADir, "untracked.env")
	_ = os.WriteFile(appUntracked, []byte("SECRET_KEY=12345"), 0644)

	docsDir := filepath.Join(workDir, "docs")
	_ = os.MkdirAll(docsDir, 0755)
	_ = os.WriteFile(filepath.Join(docsDir, "architecture.md"), []byte("# Arch Doc"), 0644)

	junkDir := filepath.Join(workDir, "node_modules")
	_ = os.MkdirAll(junkDir, 0755)
	_ = os.WriteFile(filepath.Join(junkDir, "junk.js"), []byte("junk"), 0644)

	// 3. Write initial config.toml
	initCfg := &config.Config{
		Namespace: "ci-workstation",
		Security: config.SecurityConfig{
			Encrypt:       true,
			AgePublicKeys: []string{kp.PublicKey},
		},
		Performance: config.PerformanceConfig{
			CompressionLevel: 19,
			MaxWorkers:       2,
		},
		Destinations: []config.DestinationConfig{
			{
				Name:     "local-vault",
				Provider: "local",
				Path:     vaultDir,
			},
		},
	}
	if err := config.SaveConfig(configPath, initCfg); err != nil {
		t.Fatalf("failed saving initial config: %v", err)
	}

	// 4. Test `castor doctor`
	doctorOut, err := executeCommand("doctor", "--config", configPath)
	if err != nil {
		t.Fatalf("doctor failed: %v\nOutput: %s", err, doctorOut)
	}
	if !strings.Contains(doctorOut, "ONLINE") {
		t.Errorf("doctor expected ONLINE status, got: %s", doctorOut)
	}

	// 5. Test `castor add`
	addOut, err := executeCommand("add", workDir, "-r", "--yes", "--config", configPath)
	if err != nil {
		t.Fatalf("add failed: %v\nOutput: %s", err, addOut)
	}
	if !strings.Contains(addOut, "app-a") || !strings.Contains(addOut, "docs") {
		t.Errorf("add expected app-a and docs in output, got: %s", addOut)
	}
	if strings.Contains(addOut, "node_modules") {
		t.Errorf("node_modules was not pruned by add")
	}

	// Reload config to verify persistence of added targets
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed reloading config: %v", err)
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("expected 2 targets added to config, got %d", len(cfg.Targets))
	}

	// 6. Test `castor status` (pre-push: UNTRACKED)
	statusOut, err := executeCommand("status", "--config", configPath)
	if err != nil {
		t.Fatalf("status failed: %v\nOutput: %s", err, statusOut)
	}
	if !strings.Contains(statusOut, "UNTRACKED") {
		t.Errorf("expected UNTRACKED status before initial push, got: %s", statusOut)
	}

	// 7. Test `castor push` (initial synchronization)
	pushOut, err := executeCommand("push", "--no-tui", "--config", configPath)
	if err != nil {
		t.Fatalf("push failed: %v\nOutput: %s", err, pushOut)
	}
	if !strings.Contains(pushOut, "app-a") || !strings.Contains(pushOut, "docs") {
		t.Errorf("push expected to sync app-a and docs, got: %s", pushOut)
	}

	vaultArchivesDir := filepath.Join(vaultDir, "archives")
	var foundArchives []string
	_ = filepath.Walk(vaultArchivesDir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			foundArchives = append(foundArchives, filepath.Base(p))
		}
		return nil
	})
	if len(foundArchives) < 4 { // 2 archives (.tar.zst.age) + 2 sidecars (.meta.json.age)
		t.Fatalf("expected at least 4 objects in vault, found %d: %v", len(foundArchives), foundArchives)
	}

	// 8. Test `castor push` idempotency (zero drift -> skipped)
	pushIdempotentOut, err := executeCommand("push", "--no-tui", "--config", configPath)
	if err != nil {
		t.Fatalf("second push failed: %v\nOutput: %s", err, pushIdempotentOut)
	}
	if !strings.Contains(strings.ToLower(pushIdempotentOut), "up to date") {
		t.Errorf("expected zero-work skip on second push, got: %s", pushIdempotentOut)
	}

	// 9. Test `castor ls`
	lsOut, err := executeCommand("ls", "--no-tui", "--config", configPath)
	if err != nil {
		t.Fatalf("ls failed: %v\nOutput: %s", err, lsOut)
	}
	if !strings.Contains(lsOut, "app-a") || !strings.Contains(lsOut, "docs") {
		t.Errorf("ls expected to list app-a and docs, got: %s", lsOut)
	}

	// 10. Test `castor verify`
	verifyOut, err := executeCommand("verify", "app-a", "--key", kp.SecretKey, "--config", configPath)
	if err != nil {
		t.Fatalf("verify failed: %v\nOutput: %s", err, verifyOut)
	}
	if !strings.Contains(verifyOut, "HEALTHY") || !strings.Contains(verifyOut, "SHA-256 verified") {
		t.Errorf("expected successful verify output, got: %s", verifyOut)
	}

	// 11. Test `castor pull` (full restore of Git repo, stashes, and untracked files)
	destAppA := filepath.Join(restoreDir, "app-a")
	pullOut, err := executeCommand("pull", "app-a", "--to", destAppA, "--key", kp.SecretKey, "--force", "--config", configPath)
	if err != nil {
		t.Fatalf("pull failed: %v\nOutput: %s", err, pullOut)
	}
	if !strings.Contains(pullOut, "Successfully restored") {
		t.Errorf("expected success message in pull output, got: %s", pullOut)
	}

	// Verify restored file contents
	restoredMain, err := os.ReadFile(filepath.Join(destAppA, "main.go"))
	if err != nil || !strings.Contains(string(restoredMain), "package main") {
		t.Errorf("main.go not restored properly: %s (%v)", string(restoredMain), err)
	}
	restoredUntracked, err := os.ReadFile(filepath.Join(destAppA, "untracked.env"))
	if err != nil || string(restoredUntracked) != "SECRET_KEY=12345" {
		t.Errorf("untracked.env not restored properly: %s (%v)", string(restoredUntracked), err)
	}

	// Verify restored Git commit log
	logCmd := exec.Command("git", "log", "--oneline")
	logCmd.Dir = destAppA
	logOut, err := logCmd.CombinedOutput()
	if err != nil || !strings.Contains(string(logOut), "initial commit") {
		t.Errorf("git commit history was not restored in pulled app-a: %s (%v)", string(logOut), err)
	}

	// Verify restored Git stashes
	stashCmd := exec.Command("git", "stash", "list")
	stashCmd.Dir = destAppA
	stashOut, err := stashCmd.CombinedOutput()
	if err != nil || !strings.Contains(string(stashOut), "wip-feature") {
		t.Errorf("git stash was not restored in pulled app-a: %s (%v)", string(stashOut), err)
	}

	// 12. Test `castor prune`
	// Delete 'docs' from config.toml, then run prune
	cfg.Targets = []config.TargetConfig{cfg.Targets[0]} // keep only app-a
	_ = config.SaveConfig(configPath, cfg)

	pruneDryOut, err := executeCommand("prune", "--dry-run", "--config", configPath)
	if err != nil {
		t.Fatalf("prune dry-run failed: %v\nOutput: %s", err, pruneDryOut)
	}
	if !strings.Contains(pruneDryOut, "docs") {
		t.Errorf("prune expected to detect docs as orphaned, got: %s", pruneDryOut)
	}

	pruneYesOut, err := executeCommand("prune", "--yes", "--config", configPath)
	if err != nil {
		t.Fatalf("prune --yes failed: %v\nOutput: %s", err, pruneYesOut)
	}
	if !strings.Contains(pruneYesOut, "Pruned") {
		t.Errorf("expected prune confirmation in output, got: %s", pruneYesOut)
	}
}
