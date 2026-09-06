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
