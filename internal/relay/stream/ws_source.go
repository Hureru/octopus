package stream

import (
	"context"
)

// WSUpstreamReader abstracts WebSocket upstream reader interface.
// This avoids circular dependency with internal/relay package.
type WSUpstreamReader interface {
	ReadEvent(ctx context.Context) ([]byte, error)
	Close() error
	CloseWithError()
	StatusCode() int
}

// WSSource wraps a WebSocket upstream reader.
type WSSource struct {
	reader WSUpstreamReader
}

// NewWSSource creates a source from a WebSocket reader.
func NewWSSource(reader WSUpstreamReader) *WSSource {
	return &WSSource{reader: reader}
}

// ReadEvent reads the next WebSocket event.
func (s *WSSource) ReadEvent(ctx context.Context) ([]byte, error) {
	return s.reader.ReadEvent(ctx)
}

// Close releases the WebSocket connection.
func (s *WSSource) Close() error {
	if s.reader != nil {
		s.reader.Close()
	}
	return nil
}
