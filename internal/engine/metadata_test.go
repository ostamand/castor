package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ostamand/castor/internal/crypto"
)

func TestMetadataPushAndRead(t *testing.T) {
	kp, err := crypto.GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair failed: %v", err)
	}

	gitMeta := &GitMeta{
		Commit:           "a4f91c3d987e02b21c45f8a9e7d82b01c38e92fa",
		Branch:           "main",
		HasStash:         true,
		Dirty:            false,
		UncommittedCount: 0,
	}

	meta := BuildMetadata(
		"workstation", "rollmind.tar.zst.age", "workstation/user/projects/rollmind",
		"/home/developer/projects/rollmind", "git", "user",
		gitMeta, "zstd", 19, true, 88592384, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	)

	// 1. Push encrypted metadata
	var buf bytes.Buffer
	if err := PushMetadata(nil, &buf, meta, []string{kp.PublicKey}); err != nil {
		t.Fatalf("PushMetadata failed: %v", err)
	}

	// 2. Read and decrypt metadata
	recovered, err := ReadMetadata(&buf, kp.SecretKey, true)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}

	if recovered.Namespace != "workstation" {
		t.Errorf("Namespace = %s, want workstation", recovered.Namespace)
	}
	if recovered.CanonicalKey != "workstation/user/projects/rollmind" {
		t.Errorf("CanonicalKey = %s, want workstation/user/projects/rollmind", recovered.CanonicalKey)
	}
	if recovered.Git == nil || recovered.Git.Commit != gitMeta.Commit {
		t.Errorf("Git commit mismatch: got %+v, want %s", recovered.Git, gitMeta.Commit)
	}
	if !recovered.Git.HasStash {
		t.Errorf("Git HasStash = false, want true")
	}
	if recovered.Payload.ArchiveSHA256 != meta.Payload.ArchiveSHA256 {
		t.Errorf("ArchiveSHA256 mismatch")
	}
}

func TestDirectoryFingerprint(t *testing.T) {
	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")
	_ = os.WriteFile(file1, []byte("alpha"), 0644)
	_ = os.WriteFile(file2, []byte("beta"), 0644)

	fp1, size1, err := DirectoryFingerprint(tmpDir, nil)
	if err != nil {
		t.Fatalf("DirectoryFingerprint failed: %v", err)
	}
	if fp1 == "" || size1 != 9 {
		t.Errorf("Fingerprint unexpected: fp=%s, size=%d", fp1, size1)
	}

	// Second run without modifications should yield identical fingerprint
	fp2, size2, err := DirectoryFingerprint(tmpDir, nil)
	if err != nil {
		t.Fatalf("Second DirectoryFingerprint failed: %v", err)
	}
	if fp1 != fp2 || size1 != size2 {
		t.Errorf("Fingerprints differ without file changes: %s vs %s", fp1, fp2)
	}

	// Modify file
	_ = os.WriteFile(file1, []byte("alphamodified"), 0644)
	fp3, _, _ := DirectoryFingerprint(tmpDir, nil)
	if fp1 == fp3 {
		t.Errorf("Fingerprint did not change after file edit!")
	}
}
