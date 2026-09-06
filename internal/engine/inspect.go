package engine

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/ostamand/castor/internal/crypto"
	"github.com/ostamand/castor/internal/storage"
)

// ArchiveEntry represents metadata for a single file/directory inside a remote archive
type ArchiveEntry struct {
	Name        string      `json:"name"`
	Size        int64       `json:"size"`
	Mode        os.FileMode `json:"mode"`
	ModTime     time.Time   `json:"mod_time"`
	IsDir       bool        `json:"is_dir"`
	IsGitBundle bool        `json:"is_git_bundle"`
}

// locateRemoteArchive finds the remote archive and metadata object paths for a canonical key
func locateRemoteArchive(ctx context.Context, prov storage.Provider, canonicalKey string) (string, string, error) {
	dirScope := path.Dir(canonicalKey)
	baseName := path.Base(canonicalKey)

	objects, err := prov.List(ctx, canonicalKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to locate archive '%s': %w", canonicalKey, err)
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
		archiveObj = path.Join(dirScope, baseName+".tar.zst.age")
		metaObj = path.Join(dirScope, baseName+".meta.json.age")
	}

	return archiveObj, metaObj, nil
}

// openArchiveStream creates a decrypted, decompressed tar reader over the remote archive
func openArchiveStream(ctx context.Context, prov storage.Provider, archiveObj string, secretKey string) (*tar.Reader, io.Closer, error) {
	rawReader, err := prov.NewReader(ctx, archiveObj)
	if err != nil {
		// Fallback: try without .age if looking for .age, or with .age if not
		altObj := archiveObj
		if strings.HasSuffix(archiveObj, ".age") {
			altObj = strings.TrimSuffix(archiveObj, ".age")
		} else {
			altObj = archiveObj + ".age"
		}
		var err2 error
		rawReader, err2 = prov.NewReader(ctx, altObj)
		if err2 != nil {
			return nil, nil, fmt.Errorf("failed to open archive stream for '%s': %w", archiveObj, err)
		}
		archiveObj = altObj
	}

	var closers []io.Closer
	closers = append(closers, rawReader)

	var decompressReader io.Reader = rawReader
	if strings.HasSuffix(archiveObj, ".age") {
		if secretKey == "" {
			_ = rawReader.Close()
			return nil, nil, fmt.Errorf("archive '%s' is encrypted with Age: private secret key required", archiveObj)
		}
		decReader, err := crypto.DecryptStream(rawReader, secretKey)
		if err != nil {
			_ = rawReader.Close()
			return nil, nil, fmt.Errorf("decryption failed for '%s': %w", archiveObj, err)
		}
		closers = append(closers, decReader)
		decompressReader = decReader
	}

	zstdReader, err := zstd.NewReader(decompressReader)
	if err != nil {
		for i := len(closers) - 1; i >= 0; i-- {
			_ = closers[i].Close()
		}
		return nil, nil, fmt.Errorf("failed to initialize zstd decompressor: %w", err)
	}

	cleanup := &multiCloser{closers: closers, zstdDec: zstdReader}
	return tar.NewReader(zstdReader), cleanup, nil
}

type multiCloser struct {
	closers []io.Closer
	zstdDec *zstd.Decoder
}

func (m *multiCloser) Close() error {
	if m.zstdDec != nil {
		m.zstdDec.Close()
	}
	var firstErr error
	for i := len(m.closers) - 1; i >= 0; i-- {
		if m.closers[i] != nil {
			if err := m.closers[i].Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// InspectArchive inspects the contents of a remote archive in memory without extracting to disk
func InspectArchive(ctx context.Context, prov storage.Provider, canonicalKey string, secretKey string, pattern string) ([]ArchiveEntry, *ArchiveMetadata, error) {
	archiveObj, metaObj, err := locateRemoteArchive(ctx, prov, canonicalKey)
	if err != nil {
		return nil, nil, err
	}

	// Fetch sidecar metadata if available
	var meta *ArchiveMetadata
	if metaObj != "" {
		if metaR, err := prov.NewReader(ctx, metaObj); err == nil {
			meta, _ = ReadMetadata(metaR, secretKey, strings.HasSuffix(metaObj, ".age"))
			_ = metaR.Close()
		}
	}

	tr, closer, err := openArchiveStream(ctx, prov, archiveObj, secretKey)
	if err != nil {
		return nil, meta, err
	}
	defer closer.Close()

	var entries []ArchiveEntry
	cleanPattern := strings.TrimSpace(pattern)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, meta, fmt.Errorf("corrupt tar stream in '%s': %w", archiveObj, err)
		}

		cleanName := filepath.ToSlash(filepath.Clean(header.Name))
		cleanName = strings.TrimPrefix(cleanName, "./")

		if cleanPattern != "" {
			matched, err := path.Match(cleanPattern, cleanName)
			if err == nil && !matched {
				matchedBase, _ := path.Match(cleanPattern, path.Base(cleanName))
				if !matchedBase {
					continue
				}
			}
		}

		entry := ArchiveEntry{
			Name:        cleanName,
			Size:        header.Size,
			Mode:        header.FileInfo().Mode(),
			ModTime:     header.ModTime,
			IsDir:       header.Typeflag == tar.TypeDir || strings.HasSuffix(cleanName, "/"),
			IsGitBundle: cleanName == ".castor/repo.bundle",
		}
		entries = append(entries, entry)
	}

	return entries, meta, nil
}

// CatFileFromArchive streams a single file from the remote archive directly to the provided writer,
// terminating the network stream immediately once the file is fully transferred.
func CatFileFromArchive(ctx context.Context, prov storage.Provider, canonicalKey string, targetFilePath string, secretKey string, w io.Writer) (*tar.Header, error) {
	archiveObj, _, err := locateRemoteArchive(ctx, prov, canonicalKey)
	if err != nil {
		return nil, err
	}

	tr, closer, err := openArchiveStream(ctx, prov, archiveObj, secretKey)
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	normTarget := filepath.ToSlash(filepath.Clean(targetFilePath))
	normTarget = strings.TrimPrefix(normTarget, "./")
	normTarget = strings.TrimPrefix(normTarget, "/")

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading archive stream: %w", err)
		}

		cleanName := filepath.ToSlash(filepath.Clean(header.Name))
		cleanName = strings.TrimPrefix(cleanName, "./")
		cleanName = strings.TrimPrefix(cleanName, "/")

		if cleanName == normTarget {
			if _, err := io.Copy(w, tr); err != nil {
				return nil, fmt.Errorf("failed to stream file content: %w", err)
			}
			return header, nil
		}
	}

	return nil, fmt.Errorf("file '%s' not found in archive '%s'", targetFilePath, canonicalKey)
}
