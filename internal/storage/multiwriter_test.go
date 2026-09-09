package storage

import (
	"bytes"
	"errors"
	"sync"
	"testing"
)

type testWriter struct {
	mu          sync.Mutex
	buf         bytes.Buffer
	failOnWrite bool
	failOnClose bool
	closed      bool
}

func (w *testWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failOnWrite {
		return 0, errors.New("simulated network drop")
	}
	return w.buf.Write(p)
}

func (w *testWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if w.failOnClose {
		return errors.New("simulated close error")
	}
	return nil
}

func TestMultiDestinationWriterBroadcast(t *testing.T) {
	w1 := &testWriter{}
	w2 := &testWriter{}

	mw := NewMultiDestinationWriter([]NamedWriter{
		{Name: "dest-1", Writer: w1},
		{Name: "dest-2", Writer: w2},
	})

	payload := []byte("Hello, Castor streaming cold storage!")
	n, err := mw.Write(payload)
	if err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("expected written bytes %d, got %d", len(payload), n)
	}

	if err := mw.Close(); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}

	if !bytes.Equal(w1.buf.Bytes(), payload) {
		t.Errorf("w1 received mismatch: got %q, want %q", w1.buf.String(), string(payload))
	}
	if !bytes.Equal(w2.buf.Bytes(), payload) {
		t.Errorf("w2 received mismatch: got %q, want %q", w2.buf.String(), string(payload))
	}
	if !w1.closed || !w2.closed {
		t.Errorf("expected both writers to be closed")
	}
}

func TestMultiDestinationWriterPartialFailureIsolation(t *testing.T) {
	healthy := &testWriter{}
	failing := &testWriter{failOnWrite: true}

	mw := NewMultiDestinationWriter([]NamedWriter{
		{Name: "gcs", Writer: healthy},
		{Name: "gdrive", Writer: failing},
	})

	payload := []byte("Broadcast data with one failing destination")
	n, err := mw.Write(payload)
	// Write must NOT fail if at least one destination succeeds
	if err != nil {
		t.Fatalf("expected write to succeed despite partial failure, got: %v", err)
	}
	if n != len(payload) {
		t.Errorf("expected %d bytes, got %d", len(payload), n)
	}

	errs := mw.Errors()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error in Errors(), got %d", len(errs))
	}
	if _, ok := errs["gdrive"]; !ok {
		t.Errorf("expected gdrive in error map, got: %v", errs)
	}

	if !bytes.Equal(healthy.buf.Bytes(), payload) {
		t.Errorf("healthy writer did not receive payload: %s", healthy.buf.String())
	}

	// Close must succeed because 'gcs' closed cleanly, even though 'gdrive' failed
	if err := mw.Close(); err != nil {
		t.Fatalf("expected Close to succeed with partial destination healthy, got: %v", err)
	}

	succeeded := mw.SucceededDestinations()
	if len(succeeded) != 1 || succeeded[0] != "gcs" {
		t.Errorf("expected succeeded [gcs], got: %v", succeeded)
	}

	failed := mw.FailedDestinations()
	if len(failed) != 1 || failed["gdrive"] == nil {
		t.Errorf("expected failed [gdrive], got: %v", failed)
	}
}

func TestMultiDestinationWriterTotalFailure(t *testing.T) {
	failing1 := &testWriter{failOnWrite: true}
	failing2 := &testWriter{failOnWrite: true}

	mw := NewMultiDestinationWriter([]NamedWriter{
		{Name: "dest-1", Writer: failing1},
		{Name: "dest-2", Writer: failing2},
	})

	payload := []byte("Data destined to fail everywhere")
	_, err := mw.Write(payload)
	if err == nil {
		t.Fatalf("expected error when all destinations fail, got nil")
	}

	// Close must return an error because all destinations failed
	if err := mw.Close(); err == nil {
		t.Fatalf("expected error on Close when all destinations fail, got nil")
	}
}
