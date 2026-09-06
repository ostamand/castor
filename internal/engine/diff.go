package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/storage"
	"github.com/ostamand/castor/internal/sysinfo"
)

// DiffStatus identifies the synchronization status between local workspace and remote vault
type DiffStatus string

const (
	StatusSynced        DiffStatus = "SYNCED"
	StatusLocalAhead    DiffStatus = "LOCAL_AHEAD"
	StatusLocalBehind   DiffStatus = "LOCAL_BEHIND"
	StatusDiverged      DiffStatus = "DIVERGED"
	StatusDirtyDrift    DiffStatus = "DIRTY_DRIFT"
	StatusRemoteMissing DiffStatus = "REMOTE_MISSING"
	StatusLocalMissing  DiffStatus = "LOCAL_MISSING"
)

// TargetDiff holds the comparison details between local target and remote sidecar manifest
type TargetDiff struct {
	TargetName             string          `json:"target_name"`
	CanonicalKey           string          `json:"canonical_key"`
	LocalPath              string          `json:"local_path"`
	RemoteMeta             *ArchiveMetadata `json:"remote_metadata,omitempty"`
	IsGit                  bool            `json:"is_git"`
	LocalCommit            string          `json:"local_commit,omitempty"`
	RemoteCommit           string          `json:"remote_commit,omitempty"`
	CommitsAhead           int             `json:"commits_ahead"`
	CommitsBehind          int             `json:"commits_behind"`
	LocalBranch            string          `json:"local_branch,omitempty"`
	RemoteBranch           string          `json:"remote_branch,omitempty"`
	LocalDirty             bool            `json:"local_dirty"`
	LocalUncommittedCount  int             `json:"local_uncommitted_count"`
	RemoteDirty            bool            `json:"remote_dirty"`
	RemoteUncommittedCount int             `json:"remote_uncommitted_count"`
	LocalStashes           int             `json:"local_stashes"`
	RemoteStashes          bool            `json:"remote_stashes"`
	LocalBytes             int64           `json:"local_bytes"`
	RemoteBytes            int64           `json:"remote_bytes"`
	Status                 DiffStatus      `json:"status"`
	Summary                string          `json:"summary"`
}

// CompareTargetWithRemote compares the active local workspace against the remote vault manifest
func CompareTargetWithRemote(
	ctx context.Context,
	prov storage.Provider,
	target config.TargetConfig,
	namespace string,
	secretKey string,
) (*TargetDiff, error) {
	canonicalKey := config.CanonicalCloudKey(namespace, target.Path, target.Namespace)
	targetPath := sysinfo.ExpandHome(target.Path)

	diff := &TargetDiff{
		TargetName:   target.Name,
		CanonicalKey: canonicalKey,
		LocalPath:    targetPath,
	}

	// 1. Locate and read remote metadata
	_, metaObj, err := locateRemoteArchive(ctx, prov, canonicalKey)
	if err == nil && metaObj != "" {
		if metaR, err := prov.NewReader(ctx, metaObj); err == nil {
			meta, _ := ReadMetadata(metaR, secretKey, strings.HasSuffix(metaObj, ".age"))
			_ = metaR.Close()
			diff.RemoteMeta = meta
		}
	}

	// 2. Check local existence
	fi, err := os.Stat(targetPath)
	localExists := err == nil && fi.IsDir()

	if !localExists && diff.RemoteMeta == nil {
		diff.Status = StatusRemoteMissing
		diff.Summary = "Target path does not exist locally, and no remote archive exists in cloud."
		return diff, nil
	}
	if !localExists && diff.RemoteMeta != nil {
		diff.Status = StatusLocalMissing
		diff.RemoteBytes = diff.RemoteMeta.Payload.UncompressedBytes
		diff.Summary = "Target path does not exist locally (available in vault for pull)."
		return diff, nil
	}
	if localExists && diff.RemoteMeta == nil {
		diff.Status = StatusRemoteMissing
		diff.Summary = "Never pushed to cloud vault."
		return diff, nil
	}

	// Both local and remote exist
	diff.RemoteBytes = diff.RemoteMeta.Payload.UncompressedBytes
	if diff.RemoteMeta.Git != nil {
		diff.RemoteCommit = diff.RemoteMeta.Git.Commit
		diff.RemoteBranch = diff.RemoteMeta.Git.Branch
		diff.RemoteDirty = diff.RemoteMeta.Git.Dirty
		diff.RemoteUncommittedCount = diff.RemoteMeta.Git.UncommittedCount
		diff.RemoteStashes = diff.RemoteMeta.Git.HasStash
	}

	// 3. Inspect local target
	gitDir := filepath.Join(targetPath, ".git")
	_, gitErr := os.Stat(gitDir)
	diff.IsGit = gitErr == nil

	if diff.IsGit {
		// Run git queries
		cmdBranch := exec.Command("git", "-C", targetPath, "rev-parse", "--abbrev-ref", "HEAD")
		if out, err := cmdBranch.Output(); err == nil {
			diff.LocalBranch = strings.TrimSpace(string(out))
		}

		cmdHead := exec.Command("git", "-C", targetPath, "rev-parse", "HEAD")
		if out, err := cmdHead.Output(); err == nil {
			diff.LocalCommit = strings.TrimSpace(string(out))
		}

		cmdStatus := exec.Command("git", "-C", targetPath, "status", "--porcelain")
		if out, err := cmdStatus.Output(); err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			count := 0
			for _, l := range lines {
				if strings.TrimSpace(l) != "" {
					count++
				}
			}
			diff.LocalUncommittedCount = count
			diff.LocalDirty = count > 0
		}

		cmdStash := exec.Command("git", "-C", targetPath, "stash", "list")
		if out, err := cmdStash.Output(); err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			count := 0
			for _, l := range lines {
				if strings.TrimSpace(l) != "" {
					count++
				}
			}
			diff.LocalStashes = count
		}

		// Evaluate Git difference
		if diff.RemoteCommit != "" && diff.LocalCommit != "" {
			if diff.LocalCommit == diff.RemoteCommit {
				if diff.LocalDirty != diff.RemoteDirty || diff.LocalUncommittedCount != diff.RemoteUncommittedCount {
					diff.Status = StatusDirtyDrift
					diff.Summary = fmt.Sprintf("Same commit (%s), but uncommitted changes differ (Local: %d uncommitted, Remote: %d).",
						ShortCommit(diff.LocalCommit), diff.LocalUncommittedCount, diff.RemoteUncommittedCount)
				} else {
					diff.Status = StatusSynced
					diff.Summary = fmt.Sprintf("In sync at commit %s on branch '%s'.", ShortCommit(diff.LocalCommit), diff.LocalBranch)
				}
			} else {
				// Check commit distance
				cmdRevList := exec.Command("git", "-C", targetPath, "rev-list", "--left-right", "--count", diff.RemoteCommit+"..."+diff.LocalCommit)
				if out, err := cmdRevList.Output(); err == nil {
					parts := strings.Fields(strings.TrimSpace(string(out)))
					if len(parts) == 2 {
						behind, _ := strconv.Atoi(parts[0])
						ahead, _ := strconv.Atoi(parts[1])
						diff.CommitsBehind = behind
						diff.CommitsAhead = ahead

						if behind == 0 && ahead > 0 {
							diff.Status = StatusLocalAhead
							diff.Summary = fmt.Sprintf("Local is %d commit(s) ahead of remote (%s -> %s).",
								ahead, ShortCommit(diff.RemoteCommit), ShortCommit(diff.LocalCommit))
						} else if behind > 0 && ahead == 0 {
							diff.Status = StatusLocalBehind
							diff.Summary = fmt.Sprintf("Local is %d commit(s) behind remote (%s -> %s).",
								behind, ShortCommit(diff.LocalCommit), ShortCommit(diff.RemoteCommit))
						} else {
							diff.Status = StatusDiverged
							diff.Summary = fmt.Sprintf("Local and remote have diverged (Ahead: %d, Behind: %d).", ahead, behind)
						}
					}
				} else {
					diff.Status = StatusDiverged
					diff.Summary = fmt.Sprintf("Commits differ (Local: %s, Remote: %s).", ShortCommit(diff.LocalCommit), ShortCommit(diff.RemoteCommit))
				}

				if diff.LocalDirty {
					diff.Summary += fmt.Sprintf(" Also has %d uncommitted change(s).", diff.LocalUncommittedCount)
				}
			}
		} else {
			diff.Status = StatusDiverged
			diff.Summary = "Git metadata incomplete on remote."
		}
	} else {
		// Non-Git directory
		var localBytes int64
		_ = filepath.Walk(targetPath, func(_ string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				localBytes += info.Size()
			}
			return nil
		})
		diff.LocalBytes = localBytes

		if localBytes == diff.RemoteBytes {
			diff.Status = StatusSynced
			diff.Summary = fmt.Sprintf("Directory size in sync (%d bytes).", localBytes)
		} else {
			diff.Status = StatusDirtyDrift
			diff.Summary = fmt.Sprintf("Directory size differs (Local: %d bytes, Remote: %d bytes).", localBytes, diff.RemoteBytes)
		}
	}

	return diff, nil
}

// ShortCommit truncates a SHA to 7 characters for display
func ShortCommit(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
