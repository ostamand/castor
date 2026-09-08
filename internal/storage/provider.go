package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ostamand/castor/internal/auth"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/sysinfo"
	"google.golang.org/api/option"
)

// ObjectInfo holds remote storage metadata for an object
type ObjectInfo struct {
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	Updated      time.Time `json:"updated"`
	StorageClass string    `json:"storage_class,omitempty"`
}

// Provider defines the interface for all cloud storage destinations
type Provider interface {
	Name() string
	Type() string
	NewWriter(ctx context.Context, objectName string) (io.WriteCloser, error)
	NewReader(ctx context.Context, objectName string) (io.ReadCloser, error)
	List(ctx context.Context, prefix string) ([]ObjectInfo, error)
	Delete(ctx context.Context, objectName string) error
	Close() error
}

// NewProviderFromConfig instantiates any supported storage provider from its configuration
func NewProviderFromConfig(ctx context.Context, dest config.DestinationConfig) (Provider, error) {
	switch dest.Provider {
	case "gcs":
		if dest.CredentialsFile == "" {
			return nil, fmt.Errorf("GCS destination '%s' requires an explicit service account key file. Specify 'credentials_file' in config.toml or run 'castor provider add gcs <bucket> --credentials <path>'", dest.Name)
		}
		keyPath := sysinfo.ExpandHome(dest.CredentialsFile)
		if _, err := os.Stat(keyPath); err != nil {
			return nil, fmt.Errorf("GCS destination '%s': service account key '%s' not found: %w", dest.Name, keyPath, err)
		}
		opts := []option.ClientOption{option.WithCredentialsFile(keyPath)}
		return NewGCSProvider(ctx, dest.Name, dest.Bucket, dest.Prefix, opts...)
	case "gdrive":
		opts, _ := auth.GetGoogleClientOptions(ctx)
		return NewGDriveProvider(ctx, dest.Name, dest.Folder, opts...)
	case "dropbox", "dbx":
		client, err := auth.GetDropboxClient(ctx)
		if err != nil {
			return nil, err
		}
		return NewDropboxProvider(dest.Name, dest.Folder, client), nil
	case "local", "file", "fs":
		return NewLocalProvider(dest.Name, dest.Path)
	case "memory":
		return NewMemoryProvider(dest.Name), nil
	default:
		return nil, fmt.Errorf("unknown or unsupported storage provider '%s'", dest.Provider)
	}
}
