package storage

import (
	"context"
	"io"
	"time"
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
