package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalCloudKey(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		name                 string
		namespace            string
		targetPath           string
		explicitSubNamespace string
		expected             string
	}{
		{
			name:                 "User home path with tilde",
			namespace:            "workstation",
			targetPath:           "~/projects/rollmind",
			explicitSubNamespace: "",
			expected:             "workstation/user/projects/rollmind",
		},
		{
			name:                 "User home absolute path",
			namespace:            "laptop",
			targetPath:           filepath.Join(home, "Documents", "Vault"),
			explicitSubNamespace: "",
			expected:             "laptop/user/Documents/Vault",
		},
		{
			name:                 "System root path",
			namespace:            "workstation",
			targetPath:           "/etc/caddy",
			explicitSubNamespace: "",
			expected:             "workstation/system/etc/caddy",
		},
		{
			name:                 "System mount path",
			namespace:            "server",
			targetPath:           "/mnt/storage/media",
			explicitSubNamespace: "",
			expected:             "server/system/mnt/storage/media",
		},
		{
			name:                 "Explicit sub-namespace override",
			namespace:            "workstation",
			targetPath:           "/var/lib/docker",
			explicitSubNamespace: "docker/app",
			expected:             "workstation/docker/app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanonicalCloudKey(tt.namespace, tt.targetPath, tt.explicitSubNamespace)
			if got != tt.expected {
				t.Errorf("CanonicalCloudKey() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestArchiveFileName(t *testing.T) {
	got := ArchiveFileName("rollmind", "zstd", true)
	if got != "rollmind.tar.zst.age" {
		t.Errorf("ArchiveFileName() = %v, want rollmind.tar.zst.age", got)
	}

	gotUnenc := ArchiveFileName("rollmind", "xz", false)
	if gotUnenc != "rollmind.tar.xz" {
		t.Errorf("ArchiveFileName() = %v, want rollmind.tar.xz", gotUnenc)
	}
}
