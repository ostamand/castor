package storage

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"time"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

// GDriveProvider implements storage.Provider for Google Drive REST API v3
type GDriveProvider struct {
	name         string
	folderPath   string
	rootFolderID string
	service      *drive.Service
	folderCache  map[string]string
	cacheMu      sync.RWMutex
	hierarchyMu  sync.Mutex
}

// NewGDriveProvider initializes the Google Drive provider and resolves the root folder
func NewGDriveProvider(ctx context.Context, name string, folderPath string, opts ...option.ClientOption) (*GDriveProvider, error) {
	srv, err := drive.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Google Drive service for '%s': %w", name, err)
	}

	p := &GDriveProvider{
		name:        name,
		folderPath:  strings.Trim(folderPath, "/"),
		service:     srv,
		folderCache: make(map[string]string),
	}

	rootID, err := p.resolveOrCreateHierarchy(ctx, "root", p.folderPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve Google Drive folder hierarchy '%s': %w", folderPath, err)
	}
	p.rootFolderID = rootID

	return p, nil
}

func (g *GDriveProvider) Name() string { return g.name }
func (g *GDriveProvider) Type() string { return "gdrive" }

// resolveOrCreateHierarchy resolves or creates nested folders relative to parentID
func (g *GDriveProvider) resolveOrCreateHierarchy(ctx context.Context, parentID, relPath string) (string, error) {
	clean := strings.Trim(relPath, "/")
	if clean == "" || clean == "." {
		return parentID, nil
	}

	cacheKey := parentID + ":" + clean
	g.cacheMu.RLock()
	if cachedID, ok := g.folderCache[cacheKey]; ok {
		g.cacheMu.RUnlock()
		return cachedID, nil
	}
	g.cacheMu.RUnlock()

	// Acquire hierarchyMu so concurrent workers don't race on folder creation
	g.hierarchyMu.Lock()
	defer g.hierarchyMu.Unlock()

	// Re-check cache after acquiring lock
	g.cacheMu.RLock()
	if cachedID, ok := g.folderCache[cacheKey]; ok {
		g.cacheMu.RUnlock()
		return cachedID, nil
	}
	g.cacheMu.RUnlock()

	parts := strings.Split(clean, "/")
	currentParent := parentID

	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}

		stepKey := currentParent + ":" + part
		g.cacheMu.RLock()
		cachedStepID, ok := g.folderCache[stepKey]
		g.cacheMu.RUnlock()
		if ok {
			currentParent = cachedStepID
			continue
		}

		q := fmt.Sprintf("'%s' in parents and name = '%s' and mimeType = 'application/vnd.google-apps.folder' and trashed = false", currentParent, part)
		res, err := g.service.Files.List().Q(q).Fields("files(id)").Context(ctx).Do()
		if err != nil {
			return "", err
		}

		if len(res.Files) > 0 {
			currentParent = res.Files[0].Id
			// If duplicate folders exist under this parent, consolidate them into the canonical folder
			if len(res.Files) > 1 {
				for _, dup := range res.Files[1:] {
					children, err := g.service.Files.List().Q(fmt.Sprintf("'%s' in parents and trashed = false", dup.Id)).Fields("files(id)").Context(ctx).Do()
					if err == nil {
						for _, child := range children.Files {
							_, _ = g.service.Files.Update(child.Id, nil).AddParents(currentParent).RemoveParents(dup.Id).Context(ctx).Do()
						}
					}
					_ = g.service.Files.Delete(dup.Id).Context(ctx).Do()
				}
			}
		} else {
			folder, err := g.service.Files.Create(&drive.File{
				Name:     part,
				MimeType: "application/vnd.google-apps.folder",
				Parents:  []string{currentParent},
			}).Fields("id").Context(ctx).Do()
			if err != nil {
				return "", err
			}
			currentParent = folder.Id
		}

		g.cacheMu.Lock()
		g.folderCache[stepKey] = currentParent
		g.cacheMu.Unlock()
	}

	g.cacheMu.Lock()
	g.folderCache[cacheKey] = currentParent
	g.cacheMu.Unlock()

	return currentParent, nil
}

// resolveTargetFolder resolves the target's subfolder structure under rootFolderID
func (g *GDriveProvider) resolveTargetFolder(ctx context.Context, objectName string) (parentFolderID string, fileName string, err error) {
	clean := strings.Trim(objectName, "/")
	dirPart := path.Dir(clean)
	fileName = path.Base(clean)

	if dirPart == "" || dirPart == "." {
		return g.rootFolderID, fileName, nil
	}

	folderID, err := g.resolveOrCreateHierarchy(ctx, g.rootFolderID, dirPart)
	if err != nil {
		return "", "", err
	}
	return folderID, fileName, nil
}

// findFileID looks for a non-folder file with the given name inside parentFolderID
func (g *GDriveProvider) findFileID(ctx context.Context, parentID, name string) (string, error) {
	q := fmt.Sprintf("'%s' in parents and name = '%s' and mimeType != 'application/vnd.google-apps.folder' and trashed = false", parentID, name)
	res, err := g.service.Files.List().Q(q).Fields("files(id)").Context(ctx).Do()
	if err != nil {
		return "", err
	}
	if len(res.Files) == 0 {
		return "", nil
	}
	return res.Files[0].Id, nil
}

type gdriveWriter struct {
	pw       *io.PipeWriter
	doneChan chan error
}

func (w *gdriveWriter) Write(p []byte) (int, error) {
	return w.pw.Write(p)
}

func (w *gdriveWriter) Close() error {
	if err := w.pw.Close(); err != nil {
		return err
	}
	return <-w.doneChan
}

// NewWriter streams an object directly into Google Drive via an in-memory pipe
func (g *GDriveProvider) NewWriter(ctx context.Context, objectName string) (io.WriteCloser, error) {
	parentID, fileName, err := g.resolveTargetFolder(ctx, objectName)
	if err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()
	doneChan := make(chan error, 1)

	go func() {
		// Deduplication check: update if already exists, else create new
		existingID, _ := g.findFileID(ctx, parentID, fileName)

		var uploadErr error
		if existingID != "" {
			_, uploadErr = g.service.Files.Update(existingID, nil).
				Media(pr, googleapi.ContentType("application/octet-stream")).
				Context(ctx).
				Do()
		} else {
			fileMeta := &drive.File{
				Name:    fileName,
				Parents: []string{parentID},
			}
			_, uploadErr = g.service.Files.Create(fileMeta).
				Media(pr, googleapi.ContentType("application/octet-stream")).
				Context(ctx).
				Do()
		}

		_ = pr.CloseWithError(uploadErr)
		doneChan <- uploadErr
	}()

	return &gdriveWriter{pw: pw, doneChan: doneChan}, nil
}

// NewReader downloads an object stream from Google Drive
func (g *GDriveProvider) NewReader(ctx context.Context, objectName string) (io.ReadCloser, error) {
	parentID, fileName, err := g.resolveTargetFolder(ctx, objectName)
	if err != nil {
		return nil, err
	}

	fileID, err := g.findFileID(ctx, parentID, fileName)
	if err != nil {
		return nil, err
	}
	if fileID == "" {
		return nil, fmt.Errorf("file '%s' not found on Google Drive", objectName)
	}

	resp, err := g.service.Files.Get(fileID).Context(ctx).Download()
	if err != nil {
		return nil, fmt.Errorf("failed to download '%s' from Google Drive: %w", objectName, err)
	}
	return resp.Body, nil
}

// findFolderID looks for a subfolder with the given name inside parentID without creating it
func (g *GDriveProvider) findFolderID(ctx context.Context, parentID, name string) (string, error) {
	stepKey := parentID + ":" + name
	g.cacheMu.RLock()
	if cachedID, ok := g.folderCache[stepKey]; ok {
		g.cacheMu.RUnlock()
		return cachedID, nil
	}
	g.cacheMu.RUnlock()

	q := fmt.Sprintf("'%s' in parents and name = '%s' and mimeType = 'application/vnd.google-apps.folder' and trashed = false", parentID, name)
	res, err := g.service.Files.List().Q(q).Fields("files(id)").Context(ctx).Do()
	if err != nil {
		return "", err
	}
	if len(res.Files) == 0 {
		return "", nil
	}
	folderID := res.Files[0].Id
	g.cacheMu.Lock()
	g.folderCache[stepKey] = folderID
	g.cacheMu.Unlock()
	return folderID, nil
}

// listDirectFiles returns non-folder files directly inside parentID matching namePrefix
func (g *GDriveProvider) listDirectFiles(ctx context.Context, parentID, currentRelPath, namePrefix string) ([]ObjectInfo, error) {
	q := fmt.Sprintf("'%s' in parents and mimeType != 'application/vnd.google-apps.folder' and trashed = false", parentID)
	var results []ObjectInfo
	pageToken := ""

	for {
		call := g.service.Files.List().
			Q(q).
			Fields("nextPageToken, files(id, name, size, modifiedTime)").
			Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		res, err := call.Do()
		if err != nil {
			return nil, err
		}

		for _, f := range res.Files {
			if namePrefix == "" || strings.HasPrefix(f.Name, namePrefix) {
				modTime, _ := time.Parse(time.RFC3339, f.ModifiedTime)
				results = append(results, ObjectInfo{
					Name:         path.Join(currentRelPath, f.Name),
					Size:         f.Size,
					Updated:      modTime,
					StorageClass: "STANDARD",
				})
			}
		}

		pageToken = res.NextPageToken
		if pageToken == "" {
			break
		}
	}

	return results, nil
}

// List returns objects inside the Castor root hierarchy matching prefix
func (g *GDriveProvider) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	cleanPrefix := strings.Trim(prefix, "/")
	if cleanPrefix == "" {
		var results []ObjectInfo
		if err := g.walkFolder(ctx, g.rootFolderID, "", &results); err != nil {
			return nil, err
		}
		return results, nil
	}

	parts := strings.Split(cleanPrefix, "/")
	currentParent := g.rootFolderID
	currentRelPath := ""

	for i, part := range parts {
		if part == "" || part == "." {
			continue
		}
		folderID, err := g.findFolderID(ctx, currentParent, part)
		if err != nil {
			return nil, err
		}
		if folderID != "" {
			currentParent = folderID
			currentRelPath = path.Join(currentRelPath, part)
			continue
		}

		// 'part' is not a folder. If this is not the last component, nothing can match.
		if i < len(parts)-1 {
			return nil, nil
		}

		// This is the last component and it is not a folder: search files directly in currentParent
		return g.listDirectFiles(ctx, currentParent, currentRelPath, part)
	}

	// Entire cleanPrefix is a folder! Walk ONLY this folder subtree.
	var results []ObjectInfo
	if err := g.walkFolder(ctx, currentParent, currentRelPath, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// Move renames an object and/or moves it to a new parent folder on Google Drive
func (g *GDriveProvider) Move(ctx context.Context, oldName, newName string) error {
	oldParentID, oldFileName, err := g.resolveTargetFolder(ctx, oldName)
	if err != nil {
		return fmt.Errorf("failed to resolve source folder for '%s': %w", oldName, err)
	}

	fileID, err := g.findFileID(ctx, oldParentID, oldFileName)
	if err != nil {
		return fmt.Errorf("failed to find source file '%s': %w", oldName, err)
	}
	if fileID == "" {
		return fmt.Errorf("file '%s' not found on Google Drive", oldName)
	}

	newParentID, newFileName, err := g.resolveTargetFolder(ctx, newName)
	if err != nil {
		return fmt.Errorf("failed to resolve destination folder for '%s': %w", newName, err)
	}

	updateCall := g.service.Files.Update(fileID, &drive.File{Name: newFileName})
	if newParentID != oldParentID {
		updateCall = updateCall.AddParents(newParentID).RemoveParents(oldParentID)
	}

	_, err = updateCall.Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to move file from '%s' to '%s' on Google Drive: %w", oldName, newName, err)
	}
	return nil
}

// walkFolder recursively gathers file metadata under parentID
func (g *GDriveProvider) walkFolder(ctx context.Context, parentID, currentRelPath string, results *[]ObjectInfo) error {
	q := fmt.Sprintf("'%s' in parents and trashed = false", parentID)
	pageToken := ""

	for {
		call := g.service.Files.List().
			Q(q).
			Fields("nextPageToken, files(id, name, size, modifiedTime, mimeType)").
			Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		res, err := call.Do()
		if err != nil {
			return err
		}

		for _, f := range res.Files {
			relPath := path.Join(currentRelPath, f.Name)
			if f.MimeType == "application/vnd.google-apps.folder" {
				if err := g.walkFolder(ctx, f.Id, relPath, results); err != nil {
					return err
				}
			} else {
				modTime, _ := time.Parse(time.RFC3339, f.ModifiedTime)
				*results = append(*results, ObjectInfo{
					Name:         relPath,
					Size:         f.Size,
					Updated:      modTime,
					StorageClass: "STANDARD",
				})
			}
		}

		pageToken = res.NextPageToken
		if pageToken == "" {
			break
		}
	}

	return nil
}

// Delete deletes a file on Google Drive
func (g *GDriveProvider) Delete(ctx context.Context, objectName string) error {
	parentID, fileName, err := g.resolveTargetFolder(ctx, objectName)
	if err != nil {
		return err
	}

	fileID, err := g.findFileID(ctx, parentID, fileName)
	if err != nil {
		return err
	}
	if fileID == "" {
		return nil // Already deleted
	}

	return g.service.Files.Delete(fileID).Context(ctx).Do()
}

// Close closes provider resources
func (g *GDriveProvider) Close() error {
	return nil
}
