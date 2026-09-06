package sysinfo

import (
	"os"
	"os/user"
	"regexp"
	"runtime"
	"strings"
)

// HostInfo encapsulates details about the local machine environment
type HostInfo struct {
	Hostname         string `json:"hostname"`
	DefaultNamespace string `json:"default_namespace"`
	OS               string `json:"os"`
	Arch             string `json:"arch"`
	Username         string `json:"username"`
	HomeDir          string `json:"home_dir"`
}

var nonAlphanumericRegex = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// GetHostInfo inspects the system and returns normalized host properties
func GetHostInfo() HostInfo {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown-host"
	}

	// Clean hostname for default namespace suggestion
	cleanNamespace := strings.ToLower(hostname)
	cleanNamespace = nonAlphanumericRegex.ReplaceAllString(cleanNamespace, "-")
	cleanNamespace = strings.Trim(cleanNamespace, "-_")
	if cleanNamespace == "" {
		cleanNamespace = "default"
	}

	username := "unknown"
	homeDir, _ := os.UserHomeDir()
	if u, err := user.Current(); err == nil {
		username = u.Username
		if homeDir == "" {
			homeDir = u.HomeDir
		}
	}

	return HostInfo{
		Hostname:         hostname,
		DefaultNamespace: cleanNamespace,
		OS:               runtime.GOOS,
		Arch:             runtime.GOARCH,
		Username:         username,
		HomeDir:          homeDir,
	}
}

// ExpandHome resolves leading ~/ to the user's home directory
func ExpandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return home + path[1:]
		}
	}
	return path
}
