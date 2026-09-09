package engine

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/crypto"
	"github.com/ostamand/castor/internal/storage"
	"github.com/ostamand/castor/internal/sysinfo"
)

// ArchiveResult contains metrics and status of an archive operation
type ArchiveResult struct {
	TargetName        string
	CanonicalKey      string
	OriginPath        string
	Fingerprint       string
	Skipped           bool
	UncompressedBytes int64
	CipherBytes       int64
	ArchiveSHA256     string
	Duration          time.Duration
	DestErrors        map[string]error
	StreamedDests     []string
}

// ProgressFunc reports live bytes streamed to the dashboard
type ProgressFunc func(targetName string, bytesStreamed int64, done bool)

// StreamArchive streams a target workspace in memory directly to all active destinations
func StreamArchive(
	ctx context.Context,
	target config.TargetConfig,
	cfg *config.Config,
	providers map[string]storage.Provider,
	progressFn ProgressFunc,
) (*ArchiveResult, error) {
	start := time.Now()
	targetPath := sysinfo.ExpandHome(target.Path)

	// 1. Resolve canonical cloud key
	canonicalKey := config.CanonicalCloudKey(cfg.Namespace, target.Name)
	targetName := target.Name
	if targetName == "" {
		targetName = path.Base(canonicalKey)
	}

	// 2. Compute fingerprint
	var fingerprint string
	var gitMeta *GitMeta
	var err error

	if target.Type == "git" {
		fingerprint, gitMeta, err = GitFingerprint(targetPath)
	} else {
		fingerprint, _, err = DirectoryFingerprint(targetPath, cfg.Rules.Generic.Excludes)
	}
	if err != nil {
		return nil, fmt.Errorf("fingerprinting failed for target '%s': %w", targetName, err)
	}

	// 3. Filter active destinations
	activeDests := cfg.ActiveDestinations()
	if len(target.Destinations) > 0 {
		var filtered []config.DestinationConfig
		targetDestMap := make(map[string]bool)
		for _, d := range target.Destinations {
			targetDestMap[d] = true
		}
		for _, d := range cfg.ActiveDestinations() {
			if targetDestMap[d.Name] {
				filtered = append(filtered, d)
			}
		}
		activeDests = filtered
	}

	if len(activeDests) == 0 {
		return nil, fmt.Errorf("no active storage destinations configured for target '%s' (all destinations disabled or unrouted)", targetName)
	}

	// 4. Resolve destination object names
	compression := target.Compression
	if compression == "" {
		compression = "zstd"
	}
	archiveFileName := config.ArchiveFileName(path.Base(canonicalKey), compression, cfg.Security.Encrypt)
	metaFileName := config.MetadataFileName(path.Base(canonicalKey), cfg.Security.Encrypt)

	dirScope := path.Dir(canonicalKey)
	archiveRemotePath := path.Join(dirScope, archiveFileName)
	metaRemotePath := path.Join(dirScope, metaFileName)

	// 5. Open writers across all providers
	var namedWriters []storage.NamedWriter
	for _, dest := range activeDests {
		prov, ok := providers[dest.Name]
		if !ok {
			return nil, fmt.Errorf("destination provider '%s' not found", dest.Name)
		}
		w, err := prov.NewWriter(ctx, archiveRemotePath)
		if err != nil {
			return nil, fmt.Errorf("failed to open destination writer for '%s': %w", dest.Name, err)
		}
		namedWriters = append(namedWriters, storage.NamedWriter{Name: dest.Name, Writer: w})
	}

	multiWriter := storage.NewMultiDestinationWriter(namedWriters)
	hasher := crypto.NewStreamHasher(multiWriter)

	// 6. Pipeline: Archive -> Tar -> Zstd -> Age -> MultiWriter
	var ageWriter io.Closer = nil
	currentWriter := io.Writer(hasher)

	if cfg.Security.Encrypt && len(cfg.Security.AgePublicKeys) > 0 {
		aw, err := crypto.EncryptStream(hasher, cfg.Security.AgePublicKeys)
		if err != nil {
			_ = multiWriter.Close()
			return nil, fmt.Errorf("failed to initialize Age encryption: %w", err)
		}
		ageWriter = aw
		currentWriter = aw
	}

	// Zstd encoder
	zLevel := zstd.SpeedBetterCompression
	if cfg.Performance.CompressionLevel >= 19 {
		zLevel = zstd.SpeedBestCompression
	}
	zstdWriter, err := zstd.NewWriter(currentWriter, zstd.WithEncoderLevel(zLevel))
	if err != nil {
		if ageWriter != nil {
			_ = ageWriter.Close()
		}
		_ = multiWriter.Close()
		return nil, fmt.Errorf("failed to initialize zstd compressor: %w", err)
	}

	// Progress reporter goroutine
	doneProgress := make(chan struct{})
	if progressFn != nil {
		go func() {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-doneProgress:
					progressFn(targetName, hasher.BytesWritten(), true)
					return
				case <-ticker.C:
					progressFn(targetName, hasher.BytesWritten(), false)
				}
			}
		}()
	}

	// Pack files into the compression pipeline
	uncompressedBytes, packedGitMeta, archiveErr := ArchiveTarget(zstdWriter, target, cfg.Rules)
	if packedGitMeta != nil && gitMeta == nil {
		gitMeta = packedGitMeta
	}

	// Flush and close compression and encryption streams
	closeZstdErr := zstdWriter.Close()
	var closeAgeErr error
	if ageWriter != nil {
		closeAgeErr = ageWriter.Close()
	}
	closeMultiErr := multiWriter.Close()

	close(doneProgress)

	if archiveErr != nil {
		return nil, fmt.Errorf("archiving failed: %w", archiveErr)
	}
	if closeZstdErr != nil {
		return nil, fmt.Errorf("failed to flush compression: %w", closeZstdErr)
	}
	if closeAgeErr != nil {
		return nil, fmt.Errorf("failed to finalize Age encryption: %w", closeAgeErr)
	}
	if closeMultiErr != nil {
		// All destinations failed to write/close the archive payload
		return nil, fmt.Errorf("all destinations failed payload streaming: %w", closeMultiErr)
	}

	cipherBytes := hasher.BytesWritten()
	sha256Sum := hasher.SHA256()

	// Circuit breaker check
	maxGB := target.MaxSizeGB
	if maxGB <= 0 {
		maxGB = cfg.Safety.MaxArchiveSizeGB
	}
	if maxGB > 0 {
		maxBytes := int64(maxGB * 1024 * 1024 * 1024)
		if cipherBytes > maxBytes {
			return nil, fmt.Errorf("circuit breaker tripped: archive size (%.2f GB) exceeds ceiling (%.2f GB)",
				float64(cipherBytes)/(1024*1024*1024), maxGB)
		}
	}

	// 7. Write Sidecar Metadata Manifest (.meta.json.age)
	// Only attempt writing metadata to destinations that successfully received the archive payload
	destErrors := multiWriter.Errors()
	var metaNamedWriters []storage.NamedWriter
	for _, dest := range activeDests {
		if destErrors[dest.Name] != nil {
			continue
		}
		prov := providers[dest.Name]
		mw, err := prov.NewWriter(ctx, metaRemotePath)
		if err != nil {
			destErrors[dest.Name] = fmt.Errorf("failed to open metadata writer: %w", err)
			continue
		}
		metaNamedWriters = append(metaNamedWriters, storage.NamedWriter{Name: dest.Name, Writer: mw})
	}

	if len(metaNamedWriters) > 0 {
		metaMulti := storage.NewMultiDestinationWriter(metaNamedWriters)
		scope := "user"
		if strings.Contains(canonicalKey, "/system/") {
			scope = "system"
		}
		meta := BuildMetadata(
			cfg.Namespace, archiveFileName, canonicalKey, targetPath, target.Type, scope,
			gitMeta, compression, cfg.Performance.CompressionLevel,
			cfg.Security.Encrypt, uncompressedBytes, sha256Sum,
		)
		if pushErr := PushMetadata(ctx, metaMulti, meta, cfg.Security.AgePublicKeys); pushErr != nil {
			_ = metaMulti.Close()
			for _, nw := range metaNamedWriters {
				if destErrors[nw.Name] == nil {
					destErrors[nw.Name] = fmt.Errorf("failed to write metadata manifest: %w", pushErr)
				}
			}
		} else {
			_ = metaMulti.Close()
			for k, v := range metaMulti.Errors() {
				destErrors[k] = v
			}
		}
	}

	// Check if at least one destination succeeded both payload and manifest
	var succeededDests []string
	for _, dest := range activeDests {
		if destErrors[dest.Name] == nil {
			succeededDests = append(succeededDests, dest.Name)
		}
	}

	if len(succeededDests) == 0 && len(activeDests) > 0 {
		var errList []string
		for d, e := range destErrors {
			errList = append(errList, fmt.Sprintf("%s: %v", d, e))
		}
		return nil, fmt.Errorf("all destinations failed: %s", strings.Join(errList, "; "))
	}

	var streamedNames []string
	for _, dest := range activeDests {
		streamedNames = append(streamedNames, dest.Name)
	}

	return &ArchiveResult{
		TargetName:        targetName,
		CanonicalKey:      canonicalKey,
		OriginPath:        targetPath,
		Fingerprint:       fingerprint,
		Skipped:           false,
		UncompressedBytes: uncompressedBytes,
		CipherBytes:       cipherBytes,
		ArchiveSHA256:     sha256Sum,
		Duration:          time.Since(start),
		DestErrors:        destErrors,
		StreamedDests:     streamedNames,
	}, nil
}
