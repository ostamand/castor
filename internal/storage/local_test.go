package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalProviderMove(t *testing.T) {
	tmpDir := t.TempDir()
	prov, err := NewLocalProvider("test-local", tmpDir)
	if err != nil {
		t.Fatalf("failed to create local provider: %v", err)
	}
	defer prov.Close()

	ctx := context.Background()

	// Write an initial file
	w, err := prov.NewWriter(ctx, "ns/old-target/archive.tar.zst")
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	content := []byte("archive data payload")
	if _, err := w.Write(content); err != nil {
		t.Fatalf("failed to write content: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close writer: %v", err)
	}

	// Move the file
	if err := prov.Move(ctx, "ns/old-target/archive.tar.zst", "ns/new-target/archive.tar.zst"); err != nil {
		t.Fatalf("failed to move object: %v", err)
	}

	// Verify old file no longer exists
	if _, err := prov.NewReader(ctx, "ns/old-target/archive.tar.zst"); err == nil {
		t.Errorf("expected old file to be gone, but reader succeeded")
	}

	// Verify new file exists and content matches
	r, err := prov.NewReader(ctx, "ns/new-target/archive.tar.zst")
	if err != nil {
		t.Fatalf("failed to open reader on new file: %v", err)
	}
	defer r.Close()

	buf := make([]byte, len(content))
	if _, err := r.Read(buf); err != nil {
		t.Fatalf("failed to read from new file: %v", err)
	}
	if string(buf) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", string(buf), string(content))
	}
}

func TestLocalProviderListPruning(t *testing.T) {
	tmpDir := t.TempDir()
	prov, err := NewLocalProvider("test-local", tmpDir)
	if err != nil {
		t.Fatalf("failed to create local provider: %v", err)
	}
	defer prov.Close()

	ctx := context.Background()

	// Create objects in different namespaces and targets
	files := []string{
		"ns/images/castor.tar.zst",
		"ns/images/castor.meta.json",
		"ns/git/castor.tar.zst",
		"ns/ai-images/photo.tar.zst",
		"other-ns/docs/doc.tar.zst",
	}

	for _, f := range files {
		w, err := prov.NewWriter(ctx, f)
		if err != nil {
			t.Fatalf("failed writing %s: %v", f, err)
		}
		_, _ = w.Write([]byte("data"))
		_ = w.Close()
	}

	// List with specific prefix
	items, err := prov.List(ctx, "ns/images")
	if err != nil {
		t.Fatalf("failed listing: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 items for prefix 'ns/images', got %d", len(items))
	}

	for _, item := range items {
		if filepath.Dir(item.Name) != "ns/images" {
			t.Errorf("unexpected item in filtered list: %s", item.Name)
		}
	}

	// Non-existent prefix returns empty list without error
	nonExistent, err := prov.List(ctx, "ns/nonexistent")
	if err != nil {
		t.Fatalf("unexpected error for nonexistent prefix: %v", err)
	}
	if len(nonExistent) != 0 {
		t.Errorf("expected 0 items for nonexistent prefix, got %d", len(nonExistent))
	}
}

func TestRemoveEmptyParents(t *testing.T) {
	tmpDir := t.TempDir()
	nested := filepath.Join(tmpDir, "a", "b", "c")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("failed creating nested dirs: %v", err)
	}

	if err := removeEmptyParents(nested, tmpDir); err != nil {
		t.Fatalf("removeEmptyParents error: %v", err)
	}

	// All empty children under tmpDir should have been removed
	if _, err := os.Stat(filepath.Join(tmpDir, "a")); !os.IsNotExist(err) {
		t.Errorf("expected directory 'a' to be removed, but it exists")
	}
}
