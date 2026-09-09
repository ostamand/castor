package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
)

const (
	defaultDropboxAPIURL     = "https://api.dropboxapi.com/2"
	defaultDropboxContentURL = "https://content.dropboxapi.com/2"
	defaultDropboxChunkSize  = 8 * 1024 * 1024 // 8 MB chunks for streaming upload sessions
)

// DropboxProvider implements storage.Provider for Dropbox v2 REST API
type DropboxProvider struct {
	name       string
	folderPath string
	client     *http.Client
	apiURL     string
	contentURL string
	chunkSize  int
}

// NewDropboxProvider creates a new Dropbox storage provider
func NewDropboxProvider(name, folderPath string, client *http.Client) *DropboxProvider {
	return &DropboxProvider{
		name:       name,
		folderPath: strings.Trim(folderPath, "/"),
		client:     client,
		apiURL:     defaultDropboxAPIURL,
		contentURL: defaultDropboxContentURL,
		chunkSize:  defaultDropboxChunkSize,
	}
}

func (d *DropboxProvider) Name() string { return d.name }
func (d *DropboxProvider) Type() string { return "dropbox" }
func (d *DropboxProvider) Close() error { return nil }

// resolvePath normalizes objectName into a valid Dropbox absolute path starting with /
func (d *DropboxProvider) resolvePath(objectName string) string {
	cleanName := strings.Trim(objectName, "/")
	if d.folderPath == "" {
		return "/" + cleanName
	}
	return "/" + path.Join(d.folderPath, cleanName)
}

// listFolderPath returns the path string used for list_folder (empty string for root)
func (d *DropboxProvider) listFolderPath(prefix string) string {
	cleanPrefix := strings.Trim(prefix, "/")
	var combined string
	if d.folderPath == "" {
		combined = cleanPrefix
	} else if cleanPrefix == "" {
		combined = d.folderPath
	} else {
		combined = path.Join(d.folderPath, cleanPrefix)
	}

	if combined == "" || combined == "." {
		return ""
	}
	return "/" + combined
}

type dropboxWriter struct {
	ctx        context.Context
	provider   *DropboxProvider
	targetPath string
	buf        *bytes.Buffer
	sessionID  string
	offset     int64
	chunkSize  int
	closed     bool
}

func (w *dropboxWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, fmt.Errorf("write on closed Dropbox writer")
	}

	total := len(p)
	w.buf.Write(p)

	for w.buf.Len() >= w.chunkSize {
		chunk := w.buf.Next(w.chunkSize)
		if err := w.flushChunk(chunk); err != nil {
			return 0, err
		}
	}

	return total, nil
}

func (w *dropboxWriter) flushChunk(chunk []byte) error {
	if w.sessionID == "" {
		// Start a new upload session
		type startArgs struct {
			Close bool `json:"close"`
		}
		argsJSON, _ := json.Marshal(startArgs{Close: false})

		req, err := http.NewRequestWithContext(
			w.ctx,
			http.MethodPost,
			w.provider.contentURL+"/files/upload_session/start",
			bytes.NewReader(chunk),
		)
		if err != nil {
			return fmt.Errorf("failed to create upload_session/start request: %w", err)
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Dropbox-API-Arg", string(argsJSON))

		resp, err := w.provider.client.Do(req)
		if err != nil {
			return fmt.Errorf("upload_session/start failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("upload_session/start returned HTTP %d: %s", resp.StatusCode, string(body))
		}

		var res struct {
			SessionID string `json:"session_id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			return fmt.Errorf("failed to decode session_id: %w", err)
		}
		w.sessionID = res.SessionID
		w.offset += int64(len(chunk))
		return nil
	}

	// Append to existing upload session
	type appendCursor struct {
		SessionID string `json:"session_id"`
		Offset    int64  `json:"offset"`
	}
	type appendArgs struct {
		Cursor appendCursor `json:"cursor"`
		Close  bool         `json:"close"`
	}
	argsJSON, _ := json.Marshal(appendArgs{
		Cursor: appendCursor{SessionID: w.sessionID, Offset: w.offset},
		Close:  false,
	})

	req, err := http.NewRequestWithContext(
		w.ctx,
		http.MethodPost,
		w.provider.contentURL+"/files/upload_session/append_v2",
		bytes.NewReader(chunk),
	)
	if err != nil {
		return fmt.Errorf("failed to create upload_session/append request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Dropbox-API-Arg", string(argsJSON))

	resp, err := w.provider.client.Do(req)
	if err != nil {
		return fmt.Errorf("upload_session/append failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload_session/append returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	w.offset += int64(len(chunk))
	return nil
}

func (w *dropboxWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true

	remaining := w.buf.Bytes()

	// If no session was started, upload entire file directly via /files/upload
	if w.sessionID == "" {
		type uploadArgs struct {
			Path       string `json:"path"`
			Mode       string `json:"mode"`
			Autorename bool   `json:"autorename"`
			Mute       bool   `json:"mute"`
		}
		argsJSON, _ := json.Marshal(uploadArgs{
			Path:       w.targetPath,
			Mode:       "overwrite",
			Autorename: false,
			Mute:       false,
		})

		req, err := http.NewRequestWithContext(
			w.ctx,
			http.MethodPost,
			w.provider.contentURL+"/files/upload",
			bytes.NewReader(remaining),
		)
		if err != nil {
			return fmt.Errorf("failed to create upload request: %w", err)
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Dropbox-API-Arg", string(argsJSON))

		resp, err := w.provider.client.Do(req)
		if err != nil {
			return fmt.Errorf("upload failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("upload returned HTTP %d: %s", resp.StatusCode, string(body))
		}
		return nil
	}

	// Finish upload session
	type finishCursor struct {
		SessionID string `json:"session_id"`
		Offset    int64  `json:"offset"`
	}
	type commitMeta struct {
		Path       string `json:"path"`
		Mode       string `json:"mode"`
		Autorename bool   `json:"autorename"`
		Mute       bool   `json:"mute"`
	}
	type finishArgs struct {
		Cursor finishCursor `json:"cursor"`
		Commit commitMeta   `json:"commit"`
	}

	argsJSON, _ := json.Marshal(finishArgs{
		Cursor: finishCursor{SessionID: w.sessionID, Offset: w.offset},
		Commit: commitMeta{
			Path:       w.targetPath,
			Mode:       "overwrite",
			Autorename: false,
			Mute:       false,
		},
	})

	req, err := http.NewRequestWithContext(
		w.ctx,
		http.MethodPost,
		w.provider.contentURL+"/files/upload_session/finish",
		bytes.NewReader(remaining),
	)
	if err != nil {
		return fmt.Errorf("failed to create upload_session/finish request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Dropbox-API-Arg", string(argsJSON))

	resp, err := w.provider.client.Do(req)
	if err != nil {
		return fmt.Errorf("upload_session/finish failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload_session/finish returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// NewWriter streams an object directly into Dropbox with zero local disk staging
func (d *DropboxProvider) NewWriter(ctx context.Context, objectName string) (io.WriteCloser, error) {
	targetPath := d.resolvePath(objectName)
	return &dropboxWriter{
		ctx:        ctx,
		provider:   d,
		targetPath: targetPath,
		buf:        bytes.NewBuffer(make([]byte, 0, d.chunkSize)),
		chunkSize:  d.chunkSize,
	}, nil
}

// NewReader downloads an object stream from Dropbox
func (d *DropboxProvider) NewReader(ctx context.Context, objectName string) (io.ReadCloser, error) {
	targetPath := d.resolvePath(objectName)
	type downloadArgs struct {
		Path string `json:"path"`
	}
	argsJSON, _ := json.Marshal(downloadArgs{Path: targetPath})

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		d.contentURL+"/files/download",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}
	req.Header.Set("Dropbox-API-Arg", string(argsJSON))

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request failed: %w", err)
	}

	if resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, fmt.Errorf("file '%s' not found on Dropbox", objectName)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("download returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	return resp.Body, nil
}

// List returns objects inside the Castor root hierarchy matching prefix
func (d *DropboxProvider) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	folderPath := d.listFolderPath(prefix)

	type listFolderArgs struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	type entryItem struct {
		Tag            string `json:".tag"`
		Name           string `json:"name"`
		PathDisplay    string `json:"path_display"`
		Size           int64  `json:"size"`
		ServerModified string `json:"server_modified"`
	}
	type listFolderResponse struct {
		Entries []entryItem `json:"entries"`
		Cursor  string      `json:"cursor"`
		HasMore bool        `json:"has_more"`
	}

	reqBody, _ := json.Marshal(listFolderArgs{
		Path:      folderPath,
		Recursive: true,
	})

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		d.apiURL+"/files/list_folder",
		bytes.NewReader(reqBody),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create list_folder request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list_folder request failed: %w", err)
	}
	defer resp.Body.Close()

	// If the folder does not exist yet, return empty list cleanly
	if resp.StatusCode == http.StatusConflict {
		return []ObjectInfo{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list_folder returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var listRes listFolderResponse
	if err := json.NewDecoder(resp.Body).Decode(&listRes); err != nil {
		return nil, fmt.Errorf("failed to parse list_folder response: %w", err)
	}

	var results []ObjectInfo
	stripRoot := "/" + strings.Trim(d.folderPath, "/")
	if stripRoot == "/" {
		stripRoot = ""
	}

	addEntries := func(entries []entryItem) {
		for _, e := range entries {
			if e.Tag != "file" {
				continue
			}
			objName := e.PathDisplay
			if stripRoot != "" && strings.HasPrefix(objName, stripRoot) {
				objName = strings.TrimPrefix(objName, stripRoot)
			}
			objName = strings.TrimPrefix(objName, "/")

			var updated time.Time
			if e.ServerModified != "" {
				parsed, err := time.Parse(time.RFC3339, e.ServerModified)
				if err == nil {
					updated = parsed
				}
			}

			results = append(results, ObjectInfo{
				Name:         objName,
				Size:         e.Size,
				Updated:      updated,
				StorageClass: "STANDARD",
			})
		}
	}

	addEntries(listRes.Entries)

	// Handle pagination if more pages exist
	for listRes.HasMore {
		type continueArgs struct {
			Cursor string `json:"cursor"`
		}
		contBody, _ := json.Marshal(continueArgs{Cursor: listRes.Cursor})

		contReq, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			d.apiURL+"/files/list_folder/continue",
			bytes.NewReader(contBody),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create list_folder/continue request: %w", err)
		}
		contReq.Header.Set("Content-Type", "application/json")

		contResp, err := d.client.Do(contReq)
		if err != nil {
			return nil, fmt.Errorf("list_folder/continue request failed: %w", err)
		}

		if contResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(contResp.Body)
			contResp.Body.Close()
			return nil, fmt.Errorf("list_folder/continue returned HTTP %d: %s", contResp.StatusCode, string(body))
		}

		var contRes listFolderResponse
		err = json.NewDecoder(contResp.Body).Decode(&contRes)
		contResp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to parse list_folder/continue response: %w", err)
		}

		addEntries(contRes.Entries)
		listRes = contRes
	}

	return results, nil
}

// Delete removes an object from Dropbox
func (d *DropboxProvider) Delete(ctx context.Context, objectName string) error {
	targetPath := d.resolvePath(objectName)

	type deleteArgs struct {
		Path string `json:"path"`
	}
	bodyJSON, _ := json.Marshal(deleteArgs{Path: targetPath})

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		d.apiURL+"/files/delete_v2",
		bytes.NewReader(bodyJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to create delete request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("delete request failed: %w", err)
	}
	defer resp.Body.Close()

	// Idempotent: 409 conflict usually means path not found
	if resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusNotFound {
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Move renames or moves an object server-side in Dropbox
func (d *DropboxProvider) Move(ctx context.Context, oldName, newName string) error {
	fromPath := d.resolvePath(oldName)
	toPath := d.resolvePath(newName)

	type moveArgs struct {
		FromPath   string `json:"from_path"`
		ToPath     string `json:"to_path"`
		Autorename bool   `json:"autorename"`
	}
	bodyJSON, _ := json.Marshal(moveArgs{
		FromPath:   fromPath,
		ToPath:     toPath,
		Autorename: false,
	})

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		d.apiURL+"/files/move_v2",
		bytes.NewReader(bodyJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to create move request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("move request failed from '%s' to '%s': %w", oldName, newName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("move returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
