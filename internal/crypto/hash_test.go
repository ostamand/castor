package crypto

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestStreamHasher(t *testing.T) {
	var buf bytes.Buffer
	hasher := NewStreamHasher(&buf)

	data := []byte("Nature's engineer: Castor canadensis")
	n, err := hasher.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("Bytes written mismatch: got %d, want %d", n, len(data))
	}

	if hasher.BytesWritten() != int64(len(data)) {
		t.Errorf("hasher.BytesWritten() = %d, want %d", hasher.BytesWritten(), len(data))
	}

	expectedSHA := sha256.Sum256(data)
	expectedHex := hex.EncodeToString(expectedSHA[:])

	if hasher.SHA256() != expectedHex {
		t.Errorf("SHA256 mismatch: got %s, want %s", hasher.SHA256(), expectedHex)
	}
}
