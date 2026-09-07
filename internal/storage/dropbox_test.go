package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestDropboxProviderMetadata(t *testing.T) {
	p := NewDropboxProvider("my-dbx", "CastorLodge", http.DefaultClient)
	if p.Name() != "my-dbx" {
		t.Errorf("expected name 'my-dbx', got '%s'", p.Name())
	}
	if p.Type() != "dropbox" {
		t.Errorf("expected type 'dropbox', got '%s'", p.Type())
	}
	if err := p.Close(); err != nil {
		t.Errorf("unexpected close error: %v", err)
	}
}

func TestDropboxSingleChunkUploadAndDownload(t *testing.T) {
	files := make(map[string][]byte)
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch r.URL.Path {
		case "/files/upload":
			apiArg := r.Header.Get("Dropbox-API-Arg")
			var args struct {
				Path string `json:"path"`
			}
			_ = json.Unmarshal([]byte(apiArg), &args)
			data, _ := io.ReadAll(r.Body)
			files[args.Path] = data
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"name": "test.tar.zst.age"}`))

		case "/files/download":
			apiArg := r.Header.Get("Dropbox-API-Arg")
			var args struct {
				Path string `json:"path"`
			}
			_ = json.Unmarshal([]byte(apiArg), &args)
			content, ok := files[args.Path]
			if !ok {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error": "path/not_found"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)

		case "/files/delete_v2":
			var args struct {
				Path string `json:"path"`
			}
			_ = json.NewDecoder(r.Body).Decode(&args)
			delete(files, args.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"metadata": {"name": "deleted"}}`))

		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	ctx := context.Background()
	provider := NewDropboxProvider("test-dbx", "CastorLodge", ts.Client())
	provider.apiURL = ts.URL
	provider.contentURL = ts.URL

	// 1. Write small content
	testPayload := []byte("hello world zero-disk streaming on dropbox")
	writer, err := provider.NewWriter(ctx, "workstation/test.tar.zst.age")
	if err != nil {
		t.Fatalf("NewWriter failed: %v", err)
	}

	n, err := writer.Write(testPayload)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(testPayload) {
		t.Fatalf("expected to write %d bytes, wrote %d", len(testPayload), n)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify file was saved at /CastorLodge/workstation/test.tar.zst.age
	mu.Lock()
	savedData, exists := files["/CastorLodge/workstation/test.tar.zst.age"]
	mu.Unlock()
	if !exists {
		t.Fatalf("expected file to be uploaded to /CastorLodge/workstation/test.tar.zst.age")
	}
	if !bytes.Equal(savedData, testPayload) {
		t.Fatalf("content mismatch: expected %q, got %q", testPayload, savedData)
	}

	// 2. Read content back
	reader, err := provider.NewReader(ctx, "workstation/test.tar.zst.age")
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}
	defer reader.Close()

	readData, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading failed: %v", err)
	}
	if !bytes.Equal(readData, testPayload) {
		t.Fatalf("read mismatch: expected %q, got %q", testPayload, readData)
	}

	// 3. Delete file
	if err := provider.Delete(ctx, "workstation/test.tar.zst.age"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	mu.Lock()
	_, existsAfterDelete := files["/CastorLodge/workstation/test.tar.zst.age"]
	mu.Unlock()
	if existsAfterDelete {
		t.Fatalf("expected file to be deleted")
	}
}

func TestDropboxChunkedSessionUpload(t *testing.T) {
	sessions := make(map[string][]byte)
	finalFiles := make(map[string][]byte)
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch r.URL.Path {
		case "/files/upload_session/start":
			data, _ := io.ReadAll(r.Body)
			sessionID := fmt.Sprintf("sess-%d", len(sessions)+1)
			sessions[sessionID] = append([]byte(nil), data...)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"session_id": "%s"}`, sessionID)))

		case "/files/upload_session/append_v2":
			apiArg := r.Header.Get("Dropbox-API-Arg")
			var args struct {
				Cursor struct {
					SessionID string `json:"session_id"`
					Offset    int64  `json:"offset"`
				} `json:"cursor"`
			}
			_ = json.Unmarshal([]byte(apiArg), &args)
			data, _ := io.ReadAll(r.Body)
			sessions[args.Cursor.SessionID] = append(sessions[args.Cursor.SessionID], data...)
			w.WriteHeader(http.StatusOK)

		case "/files/upload_session/finish":
			apiArg := r.Header.Get("Dropbox-API-Arg")
			var args struct {
				Cursor struct {
					SessionID string `json:"session_id"`
					Offset    int64  `json:"offset"`
				} `json:"cursor"`
				Commit struct {
					Path string `json:"path"`
				} `json:"commit"`
			}
			_ = json.Unmarshal([]byte(apiArg), &args)
			data, _ := io.ReadAll(r.Body)
			fullContent := append(sessions[args.Cursor.SessionID], data...)
			finalFiles[args.Commit.Path] = fullContent
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"name": "finished"}`))

		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	ctx := context.Background()
	provider := NewDropboxProvider("test-dbx", "CastorLodge", ts.Client())
	provider.apiURL = ts.URL
	provider.contentURL = ts.URL
	// Set small chunk size (100 bytes) to force multi-chunk streaming
	provider.chunkSize = 100

	writer, err := provider.NewWriter(ctx, "laptop/large-target.tar.zst.age")
	if err != nil {
		t.Fatalf("NewWriter failed: %v", err)
	}

	// Generate 350 bytes of data (spans 3 chunks + 50 trailing bytes in finish)
	largePayload := make([]byte, 350)
	for i := range largePayload {
		largePayload[i] = byte(i % 256)
	}

	n, err := writer.Write(largePayload)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(largePayload) {
		t.Fatalf("expected to write %d bytes, wrote %d", len(largePayload), n)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	mu.Lock()
	savedData, exists := finalFiles["/CastorLodge/laptop/large-target.tar.zst.age"]
	mu.Unlock()

	if !exists {
		t.Fatalf("expected file to be committed via upload_session/finish")
	}
	if !bytes.Equal(savedData, largePayload) {
		t.Fatalf("chunked stream content mismatch: expected length %d, got %d", len(largePayload), len(savedData))
	}
}

func TestDropboxListFolderWithPagination(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/files/list_folder":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"entries": [
					{
						".tag": "file",
						"name": "target1.tar.zst.age",
						"path_display": "/CastorLodge/box1/target1.tar.zst.age",
						"size": 1024,
						"server_modified": "2026-09-07T12:00:00Z"
					},
					{
						".tag": "folder",
						"name": "box1",
						"path_display": "/CastorLodge/box1"
					}
				],
				"cursor": "cursor-page-2",
				"has_more": true
			}`))

		case "/files/list_folder/continue":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"entries": [
					{
						".tag": "file",
						"name": "target2.tar.zst.age",
						"path_display": "/CastorLodge/box1/target2.tar.zst.age",
						"size": 2048,
						"server_modified": "2026-09-07T13:00:00Z"
					}
				],
				"cursor": "",
				"has_more": false
			}`))

		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	ctx := context.Background()
	provider := NewDropboxProvider("test-dbx", "CastorLodge", ts.Client())
	provider.apiURL = ts.URL
	provider.contentURL = ts.URL

	items, err := provider.List(ctx, "")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 files listed, got %d", len(items))
	}

	if items[0].Name != "box1/target1.tar.zst.age" {
		t.Errorf("expected name 'box1/target1.tar.zst.age', got '%s'", items[0].Name)
	}
	if items[0].Size != 1024 {
		t.Errorf("expected size 1024, got %d", items[0].Size)
	}
	if items[0].Updated.IsZero() {
		t.Errorf("expected non-zero timestamp")
	}

	if items[1].Name != "box1/target2.tar.zst.age" {
		t.Errorf("expected name 'box1/target2.tar.zst.age', got '%s'", items[1].Name)
	}
	if items[1].Size != 2048 {
		t.Errorf("expected size 2048, got %d", items[1].Size)
	}
}

func TestDropboxListFolderEmptyOrNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error": "path/not_found"}`))
	}))
	defer ts.Close()

	ctx := context.Background()
	provider := NewDropboxProvider("test-dbx", "EmptyFolder", ts.Client())
	provider.apiURL = ts.URL
	provider.contentURL = ts.URL

	items, err := provider.List(ctx, "")
	if err != nil {
		t.Fatalf("expected nil error on uninitialized folder, got: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
}
