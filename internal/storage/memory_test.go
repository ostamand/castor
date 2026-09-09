package storage

import (
	"context"
	"testing"
)

func TestMemoryProviderMove(t *testing.T) {
	prov := NewMemoryProvider("test-mem")
	ctx := context.Background()

	w, err := prov.NewWriter(ctx, "old/path/obj")
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	_, _ = w.Write([]byte("mem content"))
	_ = w.Close()

	if err := prov.Move(ctx, "old/path/obj", "new/path/obj"); err != nil {
		t.Fatalf("failed to move object in memory: %v", err)
	}

	if _, ok := prov.GetData("old/path/obj"); ok {
		t.Errorf("expected old object to be gone")
	}

	data, ok := prov.GetData("new/path/obj")
	if !ok || string(data) != "mem content" {
		t.Errorf("expected new object with content 'mem content', got %q", string(data))
	}
}
