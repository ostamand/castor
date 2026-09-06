package engine

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/ostamand/castor/internal/crypto"
	"github.com/ostamand/castor/internal/storage"
)

// RestoreResult contains metrics of a completed restore
type RestoreResult struct {
	CanonicalKey string
	DestDir      string
	FilesPushed  int
	BytesPushed  int64
	Duration     time.Duration
	GitRestored  bool
	SHAMatched   bool
}

// RestoreArchive streams down, decrypts, and reconstitutes an archive into destDir
func RestoreArchive(
	ctx context.Context,
	prov storage.Provider,
	canonicalKey string,
	destDir string,
	secretKey string,
	overwrite bool,
) (*RestoreResult, error) {
	start := time.Now()

	// 1. Locate archive object
	dirScope := path.Dir(canonicalKey)
	baseName := path.Base(canonicalKey)

	objects, err := prov.List(ctx, canonicalKey)
	if err != nil {
		return nil, fmt.Errorf("failed to locate archive '%s': %w", canonicalKey, err)
	}

	var archiveObj, metaObj string
	for _, obj := range objects {
		cleanName := strings.TrimPrefix(obj.Name, "archives/")
		if strings.HasPrefix(cleanName, canonicalKey+".tar") {
			archiveObj = cleanName
		} else if strings.HasPrefix(cleanName, canonicalKey+".meta") {
			metaObj = cleanName
		}
	}

	if archiveObj == "" {
		// Fallback exact guess
		archiveObj = path.Join(dirScope, baseName+".tar.zst.age")
		metaObj = path.Join(dirScope, baseName+".meta.json.age")
	}

	// 2. Fetch and parse sidecar metadata if available
	var meta *ArchiveMetadata
	if metaObj != "" {
		if metaR, err := prov.NewReader(ctx, metaObj); err == nil {
			meta, _ = ReadMetadata(metaR, secretKey, strings.HasSuffix(metaObj, ".age"))
			_ = metaR.Close()
		}
	}

	// 3. Open archive reader from cloud
	rawReader, err := prov.NewReader(ctx, archiveObj)
	if err != nil {
		return nil, fmt.Errorf("failed to open archive stream for '%s': %w", archiveObj, err)
	}
	defer rawReader.Close()

	// Tee reader into sha256 hasher to verify ciphertext
	hasher := sha256.New()
	teeReader := io.TeeReader(rawReader, hasher)

	// 4. In-memory decryption & decompression pipeline
	var decompressReader io.Reader = teeReader
	isEncrypted := strings.HasSuffix(archiveObj, ".age")
	if isEncrypted {
		decReader, err := crypto.DecryptStream(teeReader, secretKey)
		if err != nil {
			return nil, fmt.Errorf("decryption failed: %w", err)
		}
		defer decReader.Close()
		decompressReader = decReader
	}

	zstdReader, err := zstd.NewReader(decompressReader)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize zstd decompressor: %w", err)
	}
	defer zstdReader.Close()

	// 5. Unpack tar archive into destDir
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, err
	}

	tr := tar.NewReader(zstdReader)
	var filesCount int
	var bytesCount int64
	var foundBundlePath string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("corrupt tar stream: %w", err)
		}

		cleanRel := filepath.Clean(header.Name)
		if strings.HasPrefix(cleanRel, "..") {
			continue // Prevent path traversal attack
		}
		targetPath := filepath.Join(destDir, cleanRel)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return nil, err
			}

		case tar.TypeReg:
			if !overwrite {
				if _, err := os.Stat(targetPath); err == nil {
					return nil, fmt.Errorf("destination file '%s' already exists (use --force to overwrite)", targetPath)
				}
			}
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return nil, err
			}
			f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return nil, err
			}
			n, err := io.Copy(f, tr)
			f.Close()
			if err != nil {
				return nil, err
			}
			filesCount++
			bytesCount += n

			if cleanRel == filepath.Join(".castor", "repo.bundle") {
				foundBundlePath = targetPath
			}

		case tar.TypeSymlink:
			_ = os.Remove(targetPath)
			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				return nil, err
			}
		}
	}

	// 6. If .castor/repo.bundle was found, reconstitute Git repository
	gitRestored := false
	if foundBundlePath != "" {
		gitDir := filepath.Join(destDir, ".git")
		_ = os.RemoveAll(gitDir)

		// Mirror clone the bundle directly into .git
		cloneCmd := exec.Command("git", "clone", "--mirror", foundBundlePath, gitDir)
		if err := cloneCmd.Run(); err == nil {
			// Convert bare mirror to working-tree repository
			cfgCmd := exec.Command("git", "-C", destDir, "config", "core.bare", "false")
			_ = cfgCmd.Run()

			// Align branch HEAD
			branch := "main"
			if meta != nil && meta.Git != nil && meta.Git.Branch != "" && meta.Git.Branch != "HEAD" {
				branch = meta.Git.Branch
			}
			checkoutCmd := exec.Command("git", "-C", destDir, "checkout", branch)
			_ = checkoutCmd.Run()

			// Reset index to HEAD without modifying unpacked files
			resetCmd := exec.Command("git", "-C", destDir, "reset", "--mixed")
			_ = resetCmd.Run()

			// Re-enable stash reflog if refs/stash was preserved
			stashVerify := exec.Command("git", "-C", destDir, "rev-parse", "--verify", "refs/stash")
			if stashOut, err := stashVerify.Output(); err == nil {
				stashOID := strings.TrimSpace(string(stashOut))
				if stashOID != "" {
					stashMsg := "restored stash"
					if msgOut, err := exec.Command("git", "-C", destDir, "log", "-1", "--format=%s", stashOID).Output(); err == nil {
						trimmed := strings.TrimSpace(string(msgOut))
						if trimmed != "" {
							stashMsg = trimmed
						}
					}
					_ = exec.Command("git", "-C", destDir, "update-ref", "-d", "refs/stash").Run()
					_ = exec.Command("git", "-C", destDir, "stash", "store", "-m", stashMsg, stashOID).Run()
				}
			}

			gitRestored = true
		}

		// Remove .castor directory
		_ = os.RemoveAll(filepath.Join(destDir, ".castor"))
	}

	// Drain remaining bytes for SHA verification
	_, _ = io.Copy(io.Discard, teeReader)
	calculatedSHA := hex.EncodeToString(hasher.Sum(nil))
	shaMatched := true
	if meta != nil && meta.Payload.ArchiveSHA256 != "" {
		shaMatched = strings.EqualFold(calculatedSHA, meta.Payload.ArchiveSHA256)
	}

	return &RestoreResult{
		CanonicalKey: canonicalKey,
		DestDir:      destDir,
		FilesPushed:  filesCount,
		BytesPushed:  bytesCount,
		Duration:     time.Since(start),
		GitRestored:  gitRestored,
		SHAMatched:   shaMatched,
	}, nil
}
