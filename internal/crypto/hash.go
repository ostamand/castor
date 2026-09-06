package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"sync/atomic"
)

// StreamHasher wraps an io.Writer, computing SHA-256 and counting written bytes
type StreamHasher struct {
	writer      io.Writer
	hasher      hash.Hash
	bytesWritten int64
}

// NewStreamHasher creates a StreamHasher wrapping the provided target writer
func NewStreamHasher(w io.Writer) *StreamHasher {
	return &StreamHasher{
		writer: w,
		hasher: sha256.New(),
	}
}

// Write writes data to both the underlying writer and the SHA-256 hasher
func (s *StreamHasher) Write(p []byte) (int, error) {
	n, err := s.writer.Write(p)
	if n > 0 {
		s.hasher.Write(p[:n])
		atomic.AddInt64(&s.bytesWritten, int64(n))
	}
	return n, err
}

// BytesWritten returns total bytes streamed through this hasher
func (s *StreamHasher) BytesWritten() int64 {
	return atomic.LoadInt64(&s.bytesWritten)
}

// SHA256 returns the final hex-encoded SHA-256 checksum
func (s *StreamHasher) SHA256() string {
	return hex.EncodeToString(s.hasher.Sum(nil))
}
