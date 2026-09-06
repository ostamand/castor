package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/ostamand/castor/internal/crypto"
	"github.com/ostamand/castor/internal/sysinfo"
)

// ArchiveMetadata represents the schema v1.0 sidecar metadata manifest
type ArchiveMetadata struct {
	Version      string      `json:"version"`
	Namespace    string      `json:"namespace"`
	ArchiveName  string      `json:"archive_name"`
	CanonicalKey string      `json:"canonical_key"`
	CreatedAt    time.Time   `json:"created_at"`
	Host         HostMeta    `json:"host"`
	Origin       OriginMeta  `json:"origin"`
	Git          *GitMeta    `json:"git,omitempty"`
	Payload      PayloadMeta `json:"payload"`
}

type HostMeta struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	User     string `json:"user"`
}

type OriginMeta struct {
	Path  string `json:"path"`
	Scope string `json:"scope"`
	Type  string `json:"type"`
}

type GitMeta struct {
	Commit            string `json:"commit"`
	Branch            string `json:"branch"`
	HasStash          bool   `json:"has_stash"`
	Dirty             bool   `json:"dirty"`
	UncommittedCount  int    `json:"uncommitted_files_count"`
}

type PayloadMeta struct {
	Compression       string `json:"compression"`
	CompressionLevel  int    `json:"compression_level"`
	Encrypted         bool   `json:"encrypted"`
	Cipher            string `json:"cipher"`
	UncompressedBytes int64  `json:"uncompressed_bytes"`
	ArchiveSHA256     string `json:"archive_sha256"`
}

// BuildMetadata creates a populated ArchiveMetadata instance
func BuildMetadata(
	namespace, archiveName, canonicalKey, originPath, originType, scope string,
	gitMeta *GitMeta,
	compression string, compressionLevel int,
	encrypted bool, uncompressedBytes int64, sha256Sum string,
) ArchiveMetadata {
	host := sysinfo.GetHostInfo()

	cipher := "none"
	if encrypted {
		cipher = "age-x25519"
	}

	return ArchiveMetadata{
		Version:      "1.0",
		Namespace:    namespace,
		ArchiveName:  archiveName,
		CanonicalKey: canonicalKey,
		CreatedAt:    time.Now().UTC(),
		Host: HostMeta{
			Hostname: host.Hostname,
			OS:       host.OS,
			Arch:     host.Arch,
			User:     host.Username,
		},
		Origin: OriginMeta{
			Path:  originPath,
			Scope: scope,
			Type:  originType,
		},
		Git: gitMeta,
		Payload: PayloadMeta{
			Compression:       compression,
			CompressionLevel:  compressionLevel,
			Encrypted:         encrypted,
			Cipher:            cipher,
			UncompressedBytes: uncompressedBytes,
			ArchiveSHA256:     sha256Sum,
		},
	}
}

// PushMetadata serializes and optionally encrypts metadata into the destination writer
func PushMetadata(ctx context.Context, w io.Writer, meta ArchiveMetadata, agePublicKeys []string) error {
	rawJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode metadata to JSON: %w", err)
	}

	if meta.Payload.Encrypted && len(agePublicKeys) > 0 {
		ageWriter, err := crypto.EncryptStream(w, agePublicKeys)
		if err != nil {
			return fmt.Errorf("failed to wrap metadata in Age encryption stream: %w", err)
		}
		if _, err := ageWriter.Write(rawJSON); err != nil {
			_ = ageWriter.Close()
			return err
		}
		return ageWriter.Close()
	}

	_, err = w.Write(rawJSON)
	return err
}

// ReadMetadata reads and optionally decrypts metadata from an io.Reader
func ReadMetadata(r io.Reader, secretKey string, encrypted bool) (*ArchiveMetadata, error) {
	reader := r
	if encrypted && secretKey != "" {
		decReader, err := crypto.DecryptStream(r, secretKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt metadata: %w", err)
		}
		defer decReader.Close()
		reader = decReader
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata stream: %w", err)
	}

	var meta ArchiveMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("failed to parse metadata JSON: %w", err)
	}

	return &meta, nil
}
