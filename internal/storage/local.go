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
	return filepath.Join(l.basePath, "archives", clean)
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
		// Fallback without archives/ subfolder
		alt := filepath.Join(l.basePath, objectName)
		if _, err := os.Stat(alt); err == nil {
			target = alt
		} else {
			return nil, fmt.Errorf("object '%s' not found in local provider '%s'", objectName, l.name)
		}
	}
	return os.Open(target)
}

func (l *LocalProvider) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	archivesDir := filepath.Join(l.basePath, "archives")
	if _, err := os.Stat(archivesDir); os.IsNotExist(err) {
		return nil, nil
	}

	cleanPrefix := filepath.ToSlash(strings.TrimPrefix(prefix, "archives/"))
	var result []ObjectInfo

	err := filepath.WalkDir(archivesDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(archivesDir, p)
		if err != nil {
			return nil
		}
		slashRel := filepath.ToSlash(rel)
		if cleanPrefix == "" || strings.HasPrefix(slashRel, cleanPrefix) {
			info, err := d.Info()
			if err != nil {
				return nil
			}
			result = append(result, ObjectInfo{
				Name:         "archives/" + slashRel,
				Size:         info.Size(),
				Updated:      info.ModTime(),
				StorageClass: "LOCAL",
			})
		}
		return nil
	})

	return result, err
}

func (l *LocalProvider) Delete(ctx context.Context, objectName string) error {
	target := l.resolvePath(objectName)
	err := os.Remove(target)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (l *LocalProvider) Close() error {
	return nil
}
