package engine

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyWithBoundNormal(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "normal.txt")
	content := []byte("Hello, world! Normal file content.")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	var buf bytes.Buffer
	n, err := copyWithBound(&buf, filePath, int64(len(content)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != int64(len(content)) {
		t.Errorf("copied %d bytes, expected %d", n, len(content))
	}
	if !bytes.Equal(buf.Bytes(), content) {
		t.Errorf("content mismatch")
	}
}

func TestCopyWithBoundFileGrows(t *testing.T) {
	// A file that was recorded as 10 bytes in tar header, but grew to 30 bytes before read
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "growing.txt")
	content := []byte("0123456789EXTRA_BYTES_WRITTEN_LATER")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	var buf bytes.Buffer
	headerSize := int64(10) // Only first 10 bytes should be copied
	n, err := copyWithBound(&buf, filePath, headerSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != headerSize {
		t.Errorf("copied %d bytes, expected clamp at %d", n, headerSize)
	}
	if !bytes.Equal(buf.Bytes(), []byte("0123456789")) {
		t.Errorf("content not clamped: %s", buf.String())
	}
}

func TestCopyWithBoundFileShrinks(t *testing.T) {
	// A file that was recorded as 20 bytes in tar header, but shrank to 5 bytes
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "shrinking.txt")
	content := []byte("short")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	var buf bytes.Buffer
	headerSize := int64(20) // Should zero-pad remaining 15 bytes
	n, err := copyWithBound(&buf, filePath, headerSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != headerSize {
		t.Errorf("copied %d bytes, expected padded to %d", n, headerSize)
	}

	result := buf.Bytes()
	if !bytes.Equal(result[:5], []byte("short")) {
		t.Errorf("initial content mismatch")
	}
	// Check remaining 15 bytes are zero bytes
	for i := 5; i < 20; i++ {
		if result[i] != 0 {
			t.Errorf("byte at index %d is %d, expected 0", i, result[i])
		}
	}
}

func TestCopyWithBoundFileVanished(t *testing.T) {
	// A file that was deleted between os.Walk and copy
	var buf bytes.Buffer
	headerSize := int64(16)
	n, err := copyWithBound(&buf, "/nonexistent/path/deleted.log", headerSize)
	if err != nil {
		t.Fatalf("unexpected error on vanished file: %v", err)
	}
	if n != headerSize {
		t.Errorf("copied %d bytes, expected %d zero-padded bytes", n, headerSize)
	}
	for i, b := range buf.Bytes() {
		if b != 0 {
			t.Errorf("byte at %d is %d, expected 0", i, b)
		}
	}
}

func TestTarIntegrityWithShiftingFiles(t *testing.T) {
	tmpDir := t.TempDir()
	growFile := filepath.Join(tmpDir, "grow.txt")
	shrinkFile := filepath.Join(tmpDir, "shrink.txt")

	_ = os.WriteFile(growFile, []byte("GROWING_FILE_EXTRA"), 0644)
	_ = os.WriteFile(shrinkFile, []byte("S"), 0644)

	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)

	// Entry 1: Grew
	growHeader := &tar.Header{Name: "grow.txt", Mode: 0644, Size: 7}
	if err := tw.WriteHeader(growHeader); err != nil {
		t.Fatal(err)
	}
	if _, err := copyWithBound(tw, growFile, growHeader.Size); err != nil {
		t.Fatal(err)
	}

	// Entry 2: Shrank
	shrinkHeader := &tar.Header{Name: "shrink.txt", Mode: 0644, Size: 10}
	if err := tw.WriteHeader(shrinkHeader); err != nil {
		t.Fatal(err)
	}
	if _, err := copyWithBound(tw, shrinkFile, shrinkHeader.Size); err != nil {
		t.Fatal(err)
	}

	// Entry 3: Vanished
	vanishHeader := &tar.Header{Name: "vanish.txt", Mode: 0644, Size: 5}
	if err := tw.WriteHeader(vanishHeader); err != nil {
		t.Fatal(err)
	}
	if _, err := copyWithBound(tw, "/nonexistent", vanishHeader.Size); err != nil {
		t.Fatal(err)
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar.Writer.Close failed: %v", err)
	}

	// Verify standard tar reader reads all 3 entries without errors
	tr := tar.NewReader(&tarBuf)
	entriesRead := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar reader encountered corrupt stream on entry %d: %v", entriesRead, err)
		}
		buf := make([]byte, hdr.Size)
		_, err = io.ReadFull(tr, buf)
		if err != nil {
			t.Fatalf("failed reading content for %s: %v", hdr.Name, err)
		}
		entriesRead++
	}

	if entriesRead != 3 {
		t.Errorf("expected 3 entries read, got %d", entriesRead)
	}
}
