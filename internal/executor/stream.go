package executor

import "io"

// TruncatingWriter wraps a writer with size limit tracking.
type TruncatingWriter struct {
	w         io.Writer
	maxBytes  int
	written   int
	truncated bool
}

// NewTruncatingWriter creates a writer that limits output size.
func NewTruncatingWriter(w io.Writer, maxBytes int) *TruncatingWriter {
	return &TruncatingWriter{
		w:        w,
		maxBytes: maxBytes,
	}
}

// Write writes data up to the maximum limit.
func (tw *TruncatingWriter) Write(p []byte) (n int, err error) {
	if tw.truncated {
		return len(p), nil // Silently discard
	}

	remaining := tw.maxBytes - tw.written
	if remaining <= 0 {
		tw.truncated = true
		return len(p), nil
	}

	toWrite := p
	if len(p) > remaining {
		toWrite = p[:remaining]
		tw.truncated = true
	}

	written, err := tw.w.Write(toWrite)
	tw.written += written

	return len(p), err // Report full write to avoid breaking callers
}

// Truncated returns true if output was truncated.
func (tw *TruncatingWriter) Truncated() bool {
	return tw.truncated
}

// Written returns the number of bytes written.
func (tw *TruncatingWriter) Written() int {
	return tw.written
}
