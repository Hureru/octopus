package stream

import (
	"context"
	"io"
)

// RawSource reads raw bytes in fixed-size chunks (for passthrough).
type RawSource struct {
	reader  io.ReadCloser
	bufSize int
}

// NewRawSource creates a source that reads raw chunks.
func NewRawSource(reader io.ReadCloser, bufSize int) *RawSource {
	if bufSize <= 0 {
		bufSize = 32 * 1024 // 32KB default
	}
	return &RawSource{
		reader:  reader,
		bufSize: bufSize,
	}
}

// ReadEvent reads the next chunk of raw bytes.
func (s *RawSource) ReadEvent(ctx context.Context) ([]byte, error) {
	buf := make([]byte, s.bufSize)
	n, err := s.reader.Read(buf)
	if n > 0 {
		// Return a copy to avoid buffer reuse issues
		chunk := make([]byte, n)
		copy(chunk, buf[:n])
		return chunk, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

// Close releases the underlying reader.
func (s *RawSource) Close() error {
	if s.reader != nil {
		return s.reader.Close()
	}
	return nil
}
