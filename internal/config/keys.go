package config

import (
	"path/filepath"
	"strings"

	"github.com/ostamand/castor/internal/sysinfo"
)

// CanonicalCloudKey returns the deterministic remote storage path for a target.
// Format: <namespace>/<targetName>
// Examples: "test/castor", "test/work/private", "laptop/vo2"
func CanonicalCloudKey(namespace string, targetName string) string {
	namespace = strings.Trim(namespace, "/")
	targetName = strings.Trim(targetName, "/")
	if namespace != "" && targetName != "" {
		return namespace + "/" + targetName
	}
	if namespace != "" {
		return namespace
	}
	return targetName
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

// CleanArchiveKey strips known archive extensions and prefixes from an archive key or filename
func CleanArchiveKey(key string) string {
	clean := strings.TrimPrefix(key, "archives/")
	for _, ext := range []string{
		".tar.zst.age",
		".tar.xz.age",
		".tar.gz.age",
		".tar.age",
		".tar.zst",
		".tar.xz",
		".tar.gz",
		".tar",
	} {
		if strings.HasSuffix(clean, ext) {
			return strings.TrimSuffix(clean, ext)
		}
	}
	return clean
}

// ResolveCanonicalKey maps a target identifier (name, path, or key) to its canonical cloud key.
// Tries to match by target name first, then by local path, then by canonical key.
func ResolveCanonicalKey(identifier string, namespace string, targets []TargetConfig) string {
	cleanID := CleanArchiveKey(strings.TrimSpace(identifier))

	for _, t := range targets {
		// Match by target name
		if t.Name == cleanID {
			return CanonicalCloudKey(namespace, t.Name)
		}
		// Match by local path
		if filepath.Clean(sysinfo.ExpandHome(t.Path)) == filepath.Clean(sysinfo.ExpandHome(cleanID)) {
			return CanonicalCloudKey(namespace, t.Name)
		}
		// Match by canonical key
		if CanonicalCloudKey(namespace, t.Name) == cleanID {
			return cleanID
		}
	}

	// Match by target base name / leaf name (e.g. "cadence-agent" matches "git/cadence-agent")
	for _, t := range targets {
		if filepath.Base(t.Name) == cleanID || strings.HasSuffix(t.Name, "/"+cleanID) {
			return CanonicalCloudKey(namespace, t.Name)
		}
	}

	// If it already looks like a namespaced key, return as-is
	if strings.HasPrefix(cleanID, namespace+"/") {
		return cleanID
	}

	// Fallback: prepend namespace
	return CanonicalCloudKey(namespace, cleanID)
}
