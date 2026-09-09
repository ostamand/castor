package storage

import (
	"fmt"
	"io"
	"sync"
)

// NamedWriter pairs a destination name with an active io.WriteCloser
type NamedWriter struct {
	Name   string
	Writer io.WriteCloser
}

// MultiDestinationWriter broadcasts bytes concurrently to multiple named destinations
type MultiDestinationWriter struct {
	writers []NamedWriter
	errs    map[string]error
	mu      sync.Mutex
}

// NewMultiDestinationWriter creates a multi-destination streaming fan-out writer
func NewMultiDestinationWriter(writers []NamedWriter) *MultiDestinationWriter {
	return &MultiDestinationWriter{
		writers: writers,
		errs:    make(map[string]error),
	}
}

// Write broadcasts the byte buffer concurrently to all destinations
func (m *MultiDestinationWriter) Write(p []byte) (int, error) {
	if len(m.writers) == 0 {
		return len(p), nil
	}

	var wg sync.WaitGroup
	type writeResult struct {
		name string
		err  error
	}
	results := make(chan writeResult, len(m.writers))

	for _, nw := range m.writers {
		// Skip writers that previously errored
		m.mu.Lock()
		if m.errs[nw.Name] != nil {
			m.mu.Unlock()
			continue
		}
		m.mu.Unlock()

		wg.Add(1)
		go func(target NamedWriter) {
			defer wg.Done()
			_, err := target.Writer.Write(p)
			results <- writeResult{name: target.Name, err: err}
		}(nw)
	}

	wg.Wait()
	close(results)

	hasErrors := false
	m.mu.Lock()
	for res := range results {
		if res.err != nil {
			m.errs[res.name] = res.err
			hasErrors = true
		}
	}
	m.mu.Unlock()

	if hasErrors && len(m.errs) == len(m.writers) {
		return 0, fmt.Errorf("all destinations failed write operation")
	}

	return len(p), nil
}

// Close flushes and closes all underlying destination writers.
// It returns an error only if all destinations failed to write or close.
// Individual destination errors are preserved in m.Errors().
func (m *MultiDestinationWriter) Close() error {
	var wg sync.WaitGroup
	for _, nw := range m.writers {
		wg.Add(1)
		go func(target NamedWriter) {
			defer wg.Done()
			if err := target.Writer.Close(); err != nil {
				m.mu.Lock()
				if m.errs[target.Name] == nil {
					m.errs[target.Name] = err
				}
				m.mu.Unlock()
			}
		}(nw)
	}
	wg.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Only return error if every single destination failed
	if len(m.errs) == len(m.writers) && len(m.writers) > 0 {
		var errMsgs []string
		for name, err := range m.errs {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: %v", name, err))
		}
		return fmt.Errorf("all destinations failed: %s", fmt.Sprintf("[%v]", errMsgs))
	}

	return nil
}

// Errors returns a map of destination names to their respective stream errors
func (m *MultiDestinationWriter) Errors() map[string]error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copyMap := make(map[string]error, len(m.errs))
	for k, v := range m.errs {
		copyMap[k] = v
	}
	return copyMap
}

// SucceededDestinations returns the names of all destinations that had no errors
func (m *MultiDestinationWriter) SucceededDestinations() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var succeeded []string
	for _, nw := range m.writers {
		if m.errs[nw.Name] == nil {
			succeeded = append(succeeded, nw.Name)
		}
	}
	return succeeded
}

// FailedDestinations returns a map of destination names to their respective stream errors
func (m *MultiDestinationWriter) FailedDestinations() map[string]error {
	return m.Errors()
}
