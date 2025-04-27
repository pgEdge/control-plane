package utils

import (
	"bytes"
	"sync"
)

// LineWriter is an io.Writer that buffers input until a newline is seen. When a
// newline is encountered, it calls the provided emit function with the line
// (without the newline).
type LineWriter struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	emit func([]byte) error
}

// NewLineWriter creates a new LineWriter with the given emit function. The emit
// function is called whenever a full line is buffered (newline is removed).
func NewLineWriter(emit func([]byte) error) *LineWriter {
	return &LineWriter{
		emit: emit,
	}
}

// Write implements io.Writer. It buffers data until a newline is encountered.
func (w *LineWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n = len(p)

	for len(p) > 0 {
		// Search for newline
		idx := bytes.IndexByte(p, '\n')
		if idx == -1 {
			// No newline found; just buffer
			w.buf.Write(p)
			return n, nil
		}

		// Newline found; buffer up to the newline
		w.buf.Write(p[:idx])

		// Emit the buffered line (without the newline)
		if err := w.emit(w.buf.Bytes()); err != nil {
			return n, err
		}

		// Reset buffer
		w.buf.Reset()

		// Skip past the newline
		p = p[idx+1:]
	}

	return n, nil
}

// Close forces the buffered data to be emitted, even if no newline is present.
func (w *LineWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.buf.Len() > 0 {
		if err := w.emit(w.buf.Bytes()); err != nil {
			return err
		}
		w.buf.Reset()
	}

	return nil
}
