package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ostamand/castor/internal/config"
)

func TestScanDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Setup directory tree:
	// tmpDir/
	//   repo1/ (.git)
	//     src/
	//   repo2/ (.git)
	//   folder1/ (generic)
	//   node_modules/ (build junk, should be skipped)
	//   .hidden/ (should be skipped)

	repo1 := filepath.Join(tmpDir, "repo1")
	_ = os.MkdirAll(filepath.Join(repo1, ".git"), 0755)
	_ = os.MkdirAll(filepath.Join(repo1, "src"), 0755)
	_ = os.WriteFile(filepath.Join(repo1, "src", "main.go"), []byte("package main"), 0644)

	repo2 := filepath.Join(tmpDir, "repo2")
	_ = os.MkdirAll(filepath.Join(repo2, ".git"), 0755)

	folder1 := filepath.Join(tmpDir, "folder1")
	_ = os.MkdirAll(folder1, 0755)
	_ = os.WriteFile(filepath.Join(folder1, "doc.txt"), []byte("hello"), 0644)

	nodeModules := filepath.Join(tmpDir, "node_modules")
	_ = os.MkdirAll(nodeModules, 0755)

	hidden := filepath.Join(tmpDir, ".hidden")
	_ = os.MkdirAll(hidden, 0755)

	// Test 1: Scan non-recursive, git-only=false
	candidates, err := ScanDirectory(tmpDir, false, false, 1, "", "generic", nil)
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	foundMap := make(map[string]string)
	for _, c := range candidates {
		foundMap[c.Name] = c.Type
	}

	if foundMap["repo1"] != "git" {
		t.Errorf("repo1 not detected as git: got %v", foundMap["repo1"])
	}
	if foundMap["repo2"] != "git" {
		t.Errorf("repo2 not detected as git: got %v", foundMap["repo2"])
	}
	if foundMap["folder1"] != "generic" {
		t.Errorf("folder1 not detected as generic: got %v", foundMap["folder1"])
	}
	if _, exists := foundMap["node_modules"]; exists {
		t.Errorf("node_modules was not pruned")
	}
	if _, exists := foundMap[".hidden"]; exists {
		t.Errorf(".hidden was not skipped")
	}

	// Test 2: Git-only filter
	gitOnlyCandidates, err := ScanDirectory(tmpDir, false, true, 1, "work/", "generic", nil)
	if err != nil {
		t.Fatalf("ScanDirectory git-only failed: %v", err)
	}

	if len(gitOnlyCandidates) != 2 {
		t.Errorf("Expected 2 git repos, got %d", len(gitOnlyCandidates))
	}
	for _, c := range gitOnlyCandidates {
		if c.Type != "git" {
			t.Errorf("Expected type git, got %s", c.Type)
		}
		if c.Name != "work/repo1" && c.Name != "work/repo2" {
			t.Errorf("Prefix not applied correctly: %s", c.Name)
		}
	}

	// Test 3: Existing target conflict detection
	existing := []config.TargetConfig{
		{
			Name: "repo1",
			Path: repo1,
			Type: "git",
		},
	}
	conflictCandidates, err := ScanDirectory(tmpDir, false, false, 1, "", "generic", existing)
	if err != nil {
		t.Fatalf("ScanDirectory with existing failed: %v", err)
	}
	for _, c := range conflictCandidates {
		if c.Name == "repo1" && !c.Conflict {
			t.Errorf("repo1 should be flagged as conflict")
		}
	}
}

func TestScanDirectoryRecursiveAndBoundaries(t *testing.T) {
	root := t.TempDir()

	// Tree structure:
	// root/
	//   groupA/
	//     project1/ (.git, with internal src/ and internal node_modules/)
	//     project2/ (.git)
	//     build/ (junk directory at group level)
	//   groupB/
	//     nested/
	//       project3/ (.git)
	//     .venv/ (junk directory)

	p1 := filepath.Join(root, "groupA", "project1")
	_ = os.MkdirAll(filepath.Join(p1, ".git"), 0755)
	_ = os.MkdirAll(filepath.Join(p1, "src", "nested"), 0755)
	_ = os.MkdirAll(filepath.Join(p1, "node_modules"), 0755)

	p2 := filepath.Join(root, "groupA", "project2")
	_ = os.MkdirAll(filepath.Join(p2, ".git"), 0755)

	groupBuild := filepath.Join(root, "groupA", "build")
	_ = os.MkdirAll(groupBuild, 0755)

	p3 := filepath.Join(root, "groupB", "nested", "project3")
	_ = os.MkdirAll(filepath.Join(p3, ".git"), 0755)

	groupVenv := filepath.Join(root, "groupB", ".venv")
	_ = os.MkdirAll(groupVenv, 0755)

	// Scan recursively with git-only=true, maxDepth=4
	candidates, err := ScanDirectory(root, true, true, 4, "", "git", nil)
	if err != nil {
		t.Fatalf("recursive ScanDirectory failed: %v", err)
	}

	if len(candidates) != 3 {
		t.Fatalf("expected 3 git repositories, got %d", len(candidates))
	}

	names := make(map[string]bool)
	for _, c := range candidates {
		names[c.Name] = true
		if c.Type != "git" {
			t.Errorf("expected type git, got %s for %s", c.Type, c.Name)
		}
	}

	if !names["project1"] || !names["project2"] || !names["project3"] {
		t.Errorf("expected project1, project2, project3; got %v", names)
	}

	// Invariant: Git internals must NEVER be registered as separate sub-targets
	if names["src"] || names["nested"] || names["node_modules"] || names["build"] || names[".venv"] {
		t.Errorf("git subdirectories or build junk leaked into candidate list: %v", names)
	}
}

func TestAddSingleTargetLifecycle(t *testing.T) {
	tmp := t.TempDir()

	// Initialize config
	cfgPath = filepath.Join(tmp, "config.toml")
	defer func() { cfgPath = "" }()

	initCfg := config.DefaultConfig()
	initCfg.Namespace = "test-box"
	initCfg.Security.Encrypt = false
	if err := config.SaveConfig(cfgPath, initCfg); err != nil {
		t.Fatalf("failed saving test config: %v", err)
	}

	// 1. Create a Git repository with subfolders
	gitRepo := filepath.Join(tmp, "awesome-app")
	_ = os.MkdirAll(filepath.Join(gitRepo, ".git"), 0755)
	_ = os.MkdirAll(filepath.Join(gitRepo, "cmd"), 0755)
	_ = os.MkdirAll(filepath.Join(gitRepo, "pkg"), 0755)
	_ = os.WriteFile(filepath.Join(gitRepo, "main.go"), []byte("package main"), 0644)

	// Add single git target
	addRecursive = false
	addScan = false
	addGitOnly = false
	addDryRun = false
	addName = ""
	addPrefix = ""

	if err := runAdd(nil, []string{gitRepo}); err != nil {
		t.Fatalf("runAdd on git repo failed: %v", err)
	}

	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("failed reloading config: %v", err)
	}
	if len(cfg.Targets) != 1 {
		t.Fatalf("expected exactly 1 target, got %d", len(cfg.Targets))
	}
	if cfg.Targets[0].Name != "awesome-app" {
		t.Errorf("expected target name 'awesome-app', got '%s'", cfg.Targets[0].Name)
	}
	if cfg.Targets[0].Type != "git" {
		t.Errorf("expected target type 'git', got '%s'", cfg.Targets[0].Type)
	}
	if !cfg.Targets[0].CreateGitBundle {
		t.Errorf("expected CreateGitBundle to be true for git target")
	}

	// 2. Add single generic folder with subfolders
	docDir := filepath.Join(tmp, "my-notes")
	_ = os.MkdirAll(filepath.Join(docDir, "personal"), 0755)
	_ = os.MkdirAll(filepath.Join(docDir, "work"), 0755)
	_ = os.WriteFile(filepath.Join(docDir, "todo.txt"), []byte("todo list"), 0644)

	if err := runAdd(nil, []string{docDir}); err != nil {
		t.Fatalf("runAdd on generic folder failed: %v", err)
	}

	cfg, err = config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("failed reloading config: %v", err)
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(cfg.Targets))
	}
	if cfg.Targets[1].Name != "my-notes" {
		t.Errorf("expected target name 'my-notes', got '%s'", cfg.Targets[1].Name)
	}
	if cfg.Targets[1].Type != "generic" {
		t.Errorf("expected target type 'generic', got '%s'", cfg.Targets[1].Type)
	}

	// 3. Re-adding should fail with duplicate conflict
	if err := runAdd(nil, []string{gitRepo}); err == nil {
		t.Errorf("expected duplicate target error when re-adding, got nil")
	}
}
