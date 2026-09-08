package storage

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// GCSProvider implements storage.Provider for Google Cloud Storage
type GCSProvider struct {
	name       string
	bucketName string
	prefix     string
	client     *storage.Client
}

// NewGCSProvider creates an initialized GCS storage provider
func NewGCSProvider(ctx context.Context, name, bucketName, prefix string, opts ...option.ClientOption) (*GCSProvider, error) {
	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GCS client for '%s': %w", name, err)
	}

	return &GCSProvider{
		name:       name,
		bucketName: bucketName,
		prefix:     strings.Trim(prefix, "/"),
		client:     client,
	}, nil
}

func (g *GCSProvider) Name() string { return g.name }
func (g *GCSProvider) Type() string { return "gcs" }

// fullPath prefixes the object name with the configured prefix
func (g *GCSProvider) fullPath(objectName string) string {
	clean := strings.TrimPrefix(objectName, "/")
	if g.prefix != "" {
		return path.Join(g.prefix, clean)
	}
	return clean
}

// NewWriter streams an object directly into GCS
func (g *GCSProvider) NewWriter(ctx context.Context, objectName string) (io.WriteCloser, error) {
	objPath := g.fullPath(objectName)
	w := g.client.Bucket(g.bucketName).Object(objPath).NewWriter(ctx)
	w.ContentType = "application/octet-stream"
	return w, nil
}

// NewReader streams an object down from GCS
func (g *GCSProvider) NewReader(ctx context.Context, objectName string) (io.ReadCloser, error) {
	objPath := g.fullPath(objectName)
	r, err := g.client.Bucket(g.bucketName).Object(objPath).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to open GCS reader for '%s': %w", objPath, err)
	}
	return r, nil
}

// List lists all objects matching prefix in the GCS bucket
func (g *GCSProvider) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	searchPrefix := g.fullPath(prefix)
	if searchPrefix != "" && !strings.HasSuffix(searchPrefix, "/") && (prefix == "" || strings.HasSuffix(prefix, "/")) {
		searchPrefix += "/"
	}
	it := g.client.Bucket(g.bucketName).Objects(ctx, &storage.Query{
		Prefix: searchPrefix,
	})

	var results []ObjectInfo
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error listing GCS bucket '%s': %w", g.bucketName, err)
		}

		// Skip 0-byte virtual folder placeholder objects (keys ending with '/')
		if strings.HasSuffix(attrs.Name, "/") {
			continue
		}

		relName := attrs.Name
		if g.prefix != "" {
			relName = strings.TrimPrefix(relName, g.prefix+"/")
		}
		if relName == "" {
			continue
		}

		results = append(results, ObjectInfo{
			Name:         relName,
			Size:         attrs.Size,
			Updated:      attrs.Updated,
			StorageClass: attrs.StorageClass,
		})
	}

	return results, nil
}

// Delete removes an object from GCS
func (g *GCSProvider) Delete(ctx context.Context, objectName string) error {
	objPath := g.fullPath(objectName)
	return g.client.Bucket(g.bucketName).Object(objPath).Delete(ctx)
}

// Close closes the GCS client
func (g *GCSProvider) Close() error {
	return g.client.Close()
}
