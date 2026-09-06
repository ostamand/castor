package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ostamand/castor/internal/config"
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
		return NewGCSProvider(ctx, dest.Name, dest.Bucket, dest.Prefix)
	case "gdrive":
		credsPath := config.DefaultCredentialsPath()
		var opts []option.ClientOption
		if _, err := os.Stat(credsPath); err == nil {
			opts = append(opts, option.WithCredentialsFile(credsPath))
		}
		return NewGDriveProvider(ctx, dest.Name, dest.Folder, opts...)
	case "local", "file", "fs":
		return NewLocalProvider(dest.Name, dest.Path)
	case "memory":
		return NewMemoryProvider(dest.Name), nil
	default:
		return nil, fmt.Errorf("unknown or unsupported storage provider '%s'", dest.Provider)
	}
}
