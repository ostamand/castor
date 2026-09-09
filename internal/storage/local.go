package storage

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ostamand/castor/internal/sysinfo"
)

// LocalProvider implements Provider for local directories, NAS, and external drives
type LocalProvider struct {
	name     string
	basePath string
}

// NewLocalProvider creates a new local filesystem storage provider
func NewLocalProvider(name, basePath string) (*LocalProvider, error) {
	if basePath == "" {
		return nil, fmt.Errorf("local provider '%s' requires a valid 'path'", name)
	}
	expanded := filepath.Clean(sysinfo.ExpandHome(basePath))
	if err := os.MkdirAll(expanded, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory for local provider '%s': %w", name, err)
	}
	return &LocalProvider{
		name:     name,
		basePath: expanded,
	}, nil
}

func (l *LocalProvider) Name() string {
	return l.name
}

func (l *LocalProvider) Type() string {
	return "local"
}

func (l *LocalProvider) resolvePath(objectName string) string {
	clean := strings.TrimPrefix(objectName, "archives/")
	return filepath.Join(l.basePath, clean)
}

func (l *LocalProvider) NewWriter(ctx context.Context, objectName string) (io.WriteCloser, error) {
	target := l.resolvePath(objectName)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return nil, fmt.Errorf("failed to create parent directories for '%s': %w", target, err)
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create object file '%s': %w", target, err)
	}
	return f, nil
}

func (l *LocalProvider) NewReader(ctx context.Context, objectName string) (io.ReadCloser, error) {
	target := l.resolvePath(objectName)
	if _, err := os.Stat(target); os.IsNotExist(err) {
		// Fallback with legacy archives/ subfolder if exists
		clean := strings.TrimPrefix(objectName, "archives/")
		alt := filepath.Join(l.basePath, "archives", clean)
		if _, err := os.Stat(alt); err == nil {
			target = alt
		} else {
			return nil, fmt.Errorf("object '%s' not found in local provider '%s'", objectName, l.name)
		}
	}
	return os.Open(target)
}

func (l *LocalProvider) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	if _, err := os.Stat(l.basePath); os.IsNotExist(err) {
		return nil, nil
	}

	cleanPrefix := filepath.ToSlash(strings.TrimPrefix(prefix, "archives/"))
	var result []ObjectInfo

	// If cleanPrefix has directory segments, determine the best existing start directory
	walkRoot := l.basePath
	if cleanPrefix != "" {
		targetDir := filepath.Join(l.basePath, cleanPrefix)
		if fi, err := os.Stat(targetDir); err == nil && fi.IsDir() {
			walkRoot = targetDir
		} else {
			parentDir := filepath.Dir(targetDir)
			if fi, err := os.Stat(parentDir); err == nil && fi.IsDir() {
				walkRoot = parentDir
			}
		}
	}

	err := filepath.WalkDir(walkRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(l.basePath, p)
		if err != nil {
			return nil
		}
		slashRel := filepath.ToSlash(rel)
		normRel := strings.TrimPrefix(slashRel, "archives/")

		if d.IsDir() {
			// Prune directory branches that cannot possibly match cleanPrefix
			if cleanPrefix != "" && !strings.HasPrefix(cleanPrefix, normRel) && !strings.HasPrefix(normRel, cleanPrefix) {
				return filepath.SkipDir
			}
			return nil
		}

		if cleanPrefix == "" || strings.HasPrefix(normRel, cleanPrefix) || strings.HasPrefix(slashRel, cleanPrefix) {
			info, err := d.Info()
			if err != nil {
				return nil
			}
			result = append(result, ObjectInfo{
				Name:         normRel,
				Size:         info.Size(),
				Updated:      info.ModTime(),
				StorageClass: "LOCAL",
			})
		}
		return nil
	})

	return result, err
}

// Move renames an object on the local filesystem using os.Rename with fallback to copy+delete
func (l *LocalProvider) Move(ctx context.Context, oldName, newName string) error {
	src := l.resolvePath(oldName)
	if _, err := os.Stat(src); os.IsNotExist(err) {
		clean := strings.TrimPrefix(oldName, "archives/")
		alt := filepath.Join(l.basePath, "archives", clean)
		if _, err := os.Stat(alt); err == nil {
			src = alt
		} else {
			return fmt.Errorf("source object '%s' not found in local provider '%s'", oldName, l.name)
		}
	}

	dst := l.resolvePath(newName)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory '%s': %w", filepath.Dir(dst), err)
	}

	// Try atomic rename
	if err := os.Rename(src, dst); err == nil {
		_ = removeEmptyParents(filepath.Dir(src), l.basePath)
		return nil
	}

	// Fallback to copy + delete if cross-device link
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file for copy '%s': %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create destination file '%s': %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("failed copying '%s' to '%s': %w", src, dst, err)
	}
	_ = in.Close()
	_ = out.Close()

	_ = os.Remove(src)
	_ = removeEmptyParents(filepath.Dir(src), l.basePath)
	return nil
}

func removeEmptyParents(dir, stopAt string) error {
	for dir != stopAt && dir != "." && dir != "/" {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			break
		}
		if err := os.Remove(dir); err != nil {
			break
		}
		dir = filepath.Dir(dir)
	}
	return nil
}

func (l *LocalProvider) Delete(ctx context.Context, objectName string) error {
	target := l.resolvePath(objectName)
	err := os.Remove(target)
	if os.IsNotExist(err) {
		clean := strings.TrimPrefix(objectName, "archives/")
		alt := filepath.Join(l.basePath, "archives", clean)
		err = os.Remove(alt)
		if os.IsNotExist(err) {
			return nil
		}
	}
	return err
}

func (l *LocalProvider) Close() error {
	return nil
}
