package engine

import (
	"archive/tar"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/sysinfo"
)

// ArchiveTarget packs target files into the provided tar stream
func ArchiveTarget(w io.Writer, target config.TargetConfig, rules config.RulesConfig) (int64, *GitMeta, error) {
	tw := tar.NewWriter(w)
	defer tw.Close()

	targetPath := sysinfo.ExpandHome(target.Path)
	var totalUncompressed int64
	var gitMeta *GitMeta

	// 1. If Git target with bundle enabled, generate .castor/repo.bundle
	if target.Type == "git" && target.CreateGitBundle {
		var err error
		var bundleBytes int64
		bundleBytes, gitMeta, err = writeGitBundle(tw, targetPath)
		if err != nil {
			return 0, nil, fmt.Errorf("failed to bundle git repository: %w", err)
		}
		totalUncompressed += bundleBytes
	}

	// 2. Select exclusion rules
	var excludes []string
	switch target.Type {
	case "git":
		excludes = rules.Git.Excludes
	case "documents":
		excludes = rules.Documents.Excludes
	case "media":
		excludes = rules.Media.Excludes
	default:
		excludes = rules.Generic.Excludes
	}

	// 3. Walk and pack working tree files
	err := filepath.WalkDir(targetPath, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // Skip unreadable file
		}

		rel, err := filepath.Rel(targetPath, p)
		if err != nil || rel == "." {
			return nil
		}

		name := d.Name()

		// If git repo, always skip .git directory since bundle holds all git objects
		if target.Type == "git" && (name == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator))) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Apply exclusion rules
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

		info, err := os.Lstat(p)
		if err != nil {
			return nil
		}

		// Skip sockets, named pipes, device nodes
		mode := info.Mode()
		if mode&os.ModeSocket != 0 || mode&os.ModeNamedPipe != 0 || mode&os.ModeDevice != 0 {
			return nil
		}

		// Handle symlinks
		if mode&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(p)
			if err != nil {
				return nil
			}
			header := &tar.Header{
				Name:     filepath.ToSlash(rel),
				Mode:     0777,
				Typeflag: tar.TypeSymlink,
				Linkname: linkTarget,
				ModTime:  info.ModTime(),
			}
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			return nil
		}

		// Handle directories
		if d.IsDir() {
			header := &tar.Header{
				Name:     filepath.ToSlash(rel) + "/",
				Mode:     0755,
				Typeflag: tar.TypeDir,
				ModTime:  info.ModTime(),
			}
			return tw.WriteHeader(header)
		}

		// Handle regular files with shifting-file safety (clamp / zero-pad)
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return nil
		}
		header.Name = filepath.ToSlash(rel)
		// Ensure standard safe permission bits
		if mode&0111 != 0 {
			header.Mode = 0755
		} else {
			header.Mode = 0644
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		fileBytes, err := copyWithBound(tw, p, header.Size)
		if err != nil {
			return err
		}
		totalUncompressed += fileBytes

		return nil
	})

	if err != nil {
		return 0, nil, fmt.Errorf("failed archiving files in '%s': %w", targetPath, err)
	}

	return totalUncompressed, gitMeta, nil
}

// writeGitBundle creates a complete git bundle including stashes and writes it into tar
func writeGitBundle(tw *tar.Writer, repoPath string) (int64, *GitMeta, error) {
	_, gitMeta, err := GitFingerprint(repoPath)
	if err != nil {
		return 0, nil, err
	}
	if gitMeta.Commit == "" {
		return 0, gitMeta, nil
	}

	// Create temp file for git bundle
	tmpFile, err := os.CreateTemp("", "castor-bundle-*.bundle")
	if err != nil {
		return 0, nil, err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	args := []string{"bundle", "create", tmpPath, "--all"}
	if gitMeta.HasStash {
		args = append(args, "refs/stash")
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return 0, nil, fmt.Errorf("git bundle failed: %s: %w", string(out), err)
	}

	fi, err := os.Stat(tmpPath)
	if err != nil {
		return 0, nil, err
	}

	header := &tar.Header{
		Name:     ".castor/repo.bundle",
		Mode:     0644,
		Size:     fi.Size(),
		Typeflag: tar.TypeReg,
		ModTime:  fi.ModTime(),
	}

	if err := tw.WriteHeader(header); err != nil {
		return 0, nil, err
	}

	bundleFile, err := os.Open(tmpPath)
	if err != nil {
		return 0, nil, err
	}
	defer bundleFile.Close()

	n, err := io.Copy(tw, bundleFile)
	return n, gitMeta, err
}

// copyWithBound reads a file up to expectedSize; if smaller, zero-pads to avoid tar errors
func copyWithBound(dst io.Writer, srcPath string, expectedSize int64) (int64, error) {
	f, err := os.Open(srcPath)
	if err != nil {
		// File vanished between Walk and Open: write zeros to satisfy header size
		return zeroPad(dst, expectedSize)
	}
	defer f.Close()

	// Limit reader to expectedSize in case file grew
	lr := io.LimitReader(f, expectedSize)
	n, err := io.Copy(dst, lr)
	if err != nil {
		return n, err
	}

	// If file shrank, zero-pad remaining bytes
	if n < expectedSize {
		diff := expectedSize - n
		padded, padErr := zeroPad(dst, diff)
		return n + padded, padErr
	}

	return n, nil
}

func zeroPad(dst io.Writer, count int64) (int64, error) {
	if count <= 0 {
		return 0, nil
	}
	zeros := make([]byte, 4096)
	var written int64
	for written < count {
		toWrite := int64(len(zeros))
		if count-written < toWrite {
			toWrite = count - written
		}
		n, err := dst.Write(zeros[:toWrite])
		written += int64(n)
		if err != nil {
			return written, err
		}
	}
	return written, nil
}
