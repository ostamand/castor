package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// MemoryProvider is a thread-safe in-memory storage provider for testing
type MemoryProvider struct {
	name    string
	mu      sync.RWMutex
	objects map[string][]byte
	updated map[string]time.Time
}

// NewMemoryProvider creates a new in-memory storage provider
func NewMemoryProvider(name string) *MemoryProvider {
	return &MemoryProvider{
		name:    name,
		objects: make(map[string][]byte),
		updated: make(map[string]time.Time),
	}
}

func (m *MemoryProvider) Name() string {
	return m.name
}

func (m *MemoryProvider) Type() string {
	return "memory"
}

type memWriter struct {
	buf  bytes.Buffer
	name string
	mp   *MemoryProvider
}

func (w *memWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *memWriter) Close() error {
	w.mp.mu.Lock()
	defer w.mp.mu.Unlock()
	data := make([]byte, w.buf.Len())
	copy(data, w.buf.Bytes())
	w.mp.objects[w.name] = data
	w.mp.updated[w.name] = time.Now()
	return nil
}

func (m *MemoryProvider) NewWriter(ctx context.Context, objectName string) (io.WriteCloser, error) {
	return &memWriter{
		name: objectName,
		mp:   m,
	}, nil
}

type memReader struct {
	*bytes.Reader
}

func (r *memReader) Close() error {
	return nil
}

func (m *MemoryProvider) NewReader(ctx context.Context, objectName string) (io.ReadCloser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, exists := m.objects[objectName]
	if !exists {
		// Also check with / without archives/ prefix
		for k, v := range m.objects {
			if strings.TrimPrefix(k, "archives/") == strings.TrimPrefix(objectName, "archives/") {
				data = v
				exists = true
				break
			}
		}
	}

	if !exists {
		return nil, fmt.Errorf("object '%s' not found in memory provider", objectName)
	}

	return &memReader{Reader: bytes.NewReader(data)}, nil
}

func (m *MemoryProvider) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []ObjectInfo
	cleanPrefix := strings.TrimPrefix(prefix, "archives/")

	for k, v := range m.objects {
		cleanK := strings.TrimPrefix(k, "archives/")
		if strings.HasPrefix(cleanK, cleanPrefix) || cleanPrefix == "" {
			result = append(result, ObjectInfo{
				Name:         k,
				Size:         int64(len(v)),
				Updated:      m.updated[k],
				StorageClass: "STANDARD",
			})
		}
	}
	return result, nil
}

func (m *MemoryProvider) Delete(ctx context.Context, objectName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, objectName)
	delete(m.updated, objectName)
	return nil
}

func (m *MemoryProvider) Close() error {
	return nil
}

// GetData retrieves raw object bytes (useful in tests)
func (m *MemoryProvider) GetData(objectName string) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, exists := m.objects[objectName]
	return data, exists
}

// SetData overrides raw object bytes (useful to simulate corrupted data in tests)
func (m *MemoryProvider) SetData(objectName string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[objectName] = data
	m.updated[objectName] = time.Now()
}
