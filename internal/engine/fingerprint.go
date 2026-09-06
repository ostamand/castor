package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitFingerprint computes a fast fingerprint (<10ms) of a Git repository
func GitFingerprint(repoPath string) (string, *GitMeta, error) {
	// Verify it's a git repo
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); err != nil {
		return "", nil, fmt.Errorf("not a git repository: %s", repoPath)
	}

	// 1. Get HEAD commit (may be empty on unborn branches/new repos)
	headCmd := exec.Command("git", "rev-parse", "HEAD")
	headCmd.Dir = repoPath
	headOut, _ := headCmd.Output()
	headCommit := strings.TrimSpace(string(headOut))

	// 2. Get current branch
	branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchCmd.Dir = repoPath
	branchOut, _ := branchCmd.Output()
	branch := strings.TrimSpace(string(branchOut))
	if branch == "" {
		branch = "HEAD"
	}

	// 3. Get status --porcelain (staged, unstaged, untracked)
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = repoPath
	statusOut, _ := statusCmd.Output()
	statusLines := strings.TrimSpace(string(statusOut))

	dirty := len(statusLines) > 0
	uncommittedCount := 0
	if dirty {
		uncommittedCount = len(strings.Split(statusLines, "\n"))
	}

	// 4. Check for stash
	stashCmd := exec.Command("git", "rev-parse", "--verify", "refs/stash")
	stashCmd.Dir = repoPath
	hasStash := stashCmd.Run() == nil

	// Hash state: commit + branch + status + hasStash
	h := sha256.New()
	h.Write([]byte(headCommit))
	h.Write([]byte(branch))
	h.Write([]byte(statusLines))
	if hasStash {
		h.Write([]byte("stash:true"))
	}
	fingerprint := hex.EncodeToString(h.Sum(nil))

	gitMeta := &GitMeta{
		Commit:           headCommit,
		Branch:           branch,
		HasStash:         hasStash,
		Dirty:            dirty,
		UncommittedCount: uncommittedCount,
	}

	return fingerprint, gitMeta, nil
}

// DirectoryFingerprint fast-walks a directory tree hashing paths, mtimes, and sizes (<20ms)
func DirectoryFingerprint(dirPath string, excludes []string) (string, int64, error) {
	h := sha256.New()
	var totalBytes int64

	err := filepath.WalkDir(dirPath, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // Skip unreadable paths without failing walk
		}

		rel, err := filepath.Rel(dirPath, p)
		if err != nil {
			return nil
		}
		if rel == "." {
			return nil
		}

		// Check exclusions
		name := d.Name()
		for _, ex := range excludes {
			if matched, _ := filepath.Match(ex, name); matched {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.Contains(rel, ex) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		// Hash: relative path + mtime nano + size
		fmt.Fprintf(h, "%s:%d:%d\n", rel, info.ModTime().UnixNano(), info.Size())
		if !d.IsDir() {
			totalBytes += info.Size()
		}

		return nil
	})

	if err != nil {
		return "", 0, fmt.Errorf("failed directory fingerprint walk for '%s': %w", dirPath, err)
	}

	return hex.EncodeToString(h.Sum(nil)), totalBytes, nil
}
