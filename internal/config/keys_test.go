package config

import (
	"testing"
)

func TestCanonicalCloudKey(t *testing.T) {
	tests := []struct {
		name       string
		namespace  string
		targetName string
		want       string
	}{
		{"Simple target", "test", "castor", "test/castor"},
		{"Hierarchical target", "test", "git/castor", "test/git/castor"},
		{"Trimmed slashes", "test/", "/castor/", "test/castor"},
		{"Empty namespace", "", "castor", "castor"},
		{"Empty target", "test", "", "test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanonicalCloudKey(tt.namespace, tt.targetName)
			if got != tt.want {
				t.Errorf("CanonicalCloudKey(%q, %q) = %q, want %q", tt.namespace, tt.targetName, got, tt.want)
			}
		})
	}
}

func TestArchiveFileName(t *testing.T) {
	tests := []struct {
		name        string
		compression string
		encrypted   bool
		want        string
	}{
		{"castor", "zstd", true, "castor.tar.zst.age"},
		{"docs", "xz", false, "docs.tar.xz"},
		{"app", "", true, "app.tar.zst.age"},
	}
	for _, tt := range tests {
		got := ArchiveFileName(tt.name, tt.compression, tt.encrypted)
		if got != tt.want {
			t.Errorf("ArchiveFileName(%q, %q, %v) = %q, want %q", tt.name, tt.compression, tt.encrypted, got, tt.want)
		}
	}
}

func TestCleanArchiveKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"archives/test/castor.tar.zst.age", "test/castor"},
		{"test/castor.tar.zst.age", "test/castor"},
		{"test/castor", "test/castor"},
		{"archives/test/git/castor.tar.xz.age", "test/git/castor"},
	}
	for _, tt := range tests {
		got := CleanArchiveKey(tt.input)
		if got != tt.want {
			t.Errorf("CleanArchiveKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestResolveCanonicalKey(t *testing.T) {
	ns := "test"
	targets := []TargetConfig{
		{Name: "castor", Path: "~/Work/git/castor"},
		{Name: "git/private", Path: "~/Work/git/private"},
	}

	tests := []struct {
		name       string
		identifier string
		want       string
	}{
		{"Resolve by name", "castor", "test/castor"},
		{"Resolve hierarchical name", "git/private", "test/git/private"},
		{"Resolve by canonical key", "test/castor", "test/castor"},
		{"Resolve with archive extension", "test/castor.tar.zst.age", "test/castor"},
		{"Resolve unknown target", "unknown", "test/unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveCanonicalKey(tt.identifier, ns, targets)
			if got != tt.want {
				t.Errorf("ResolveCanonicalKey(%q, %q) = %q, want %q", tt.identifier, ns, got, tt.want)
			}
		})
	}
}
