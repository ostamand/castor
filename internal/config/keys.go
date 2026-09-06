package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ostamand/castor/internal/sysinfo"
)

// CanonicalCloudKey resolves a deterministic, collision-free remote storage path
// formatted as: <namespace>/<scope>/<relative-path>
func CanonicalCloudKey(namespace string, targetPath string, explicitSubNamespace string) string {
	namespace = strings.Trim(namespace, "/")

	// If explicit sub-namespace override provided, use it directly
	if explicitSubNamespace != "" {
		sub := strings.Trim(explicitSubNamespace, "/")
		if namespace != "" {
			return filepath.ToSlash(filepath.Join(namespace, sub))
		}
		return filepath.ToSlash(sub)
	}

	raw := targetPath
	home, _ := os.UserHomeDir()
	cleanHome := filepath.Clean(home)

	// User home scope
	if strings.HasPrefix(raw, "~/") {
		rel := strings.TrimPrefix(raw, "~/")
		if namespace != "" {
			return filepath.ToSlash(filepath.Join(namespace, "user", rel))
		}
		return filepath.ToSlash(filepath.Join("user", rel))
	}

	cleanPath := filepath.Clean(sysinfo.ExpandHome(raw))
	if cleanHome != "" && strings.HasPrefix(cleanPath, cleanHome+string(filepath.Separator)) {
		rel := strings.TrimPrefix(cleanPath, cleanHome)
		rel = strings.TrimPrefix(rel, string(filepath.Separator))
		if namespace != "" {
			return filepath.ToSlash(filepath.Join(namespace, "user", rel))
		}
		return filepath.ToSlash(filepath.Join("user", rel))
	}

	// System root scope (outside of user home)
	rel := strings.TrimPrefix(cleanPath, string(filepath.Separator))
	if namespace != "" {
		return filepath.ToSlash(filepath.Join(namespace, "system", rel))
	}
	return filepath.ToSlash(filepath.Join("system", rel))
}

// ArchiveFileName returns the full archive file name, e.g. rollmind.tar.zst.age
func ArchiveFileName(targetName string, compression string, encrypted bool) string {
	ext := compression
	if ext == "zstd" || ext == "" {
		ext = "zst"
	}
	base := targetName + ".tar." + ext
	if encrypted {
		base += ".age"
	}
	return base
}

// MetadataFileName returns the sidecar metadata file name, e.g. rollmind.meta.json.age
func MetadataFileName(targetName string, encrypted bool) string {
	base := targetName + ".meta.json"
	if encrypted {
		base += ".age"
	}
	return base
}

// ResolveCanonicalKey maps a target identifier (name, path, or key) to its canonical cloud key
func ResolveCanonicalKey(identifier string, namespace string, targets []TargetConfig) string {
	cleanID := strings.TrimSpace(identifier)
	for _, t := range targets {
		if t.Name == cleanID || filepath.Clean(sysinfo.ExpandHome(t.Path)) == filepath.Clean(sysinfo.ExpandHome(cleanID)) {
			return CanonicalCloudKey(namespace, t.Path, t.Namespace)
		}
	}
	if strings.HasPrefix(cleanID, namespace+"/user/") || strings.HasPrefix(cleanID, namespace+"/system/") {
		return cleanID
	}
	if !strings.HasPrefix(cleanID, namespace+"/") {
		return filepath.ToSlash(filepath.Join(namespace, cleanID))
	}
	return cleanID
}
