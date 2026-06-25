package stream

import (
	"context"
	"io"
	"sync"

	"github.com/tmaxmax/go-sse"
)

// SSESource wraps an SSE event stream (tmaxmax/go-sse).
type SSESource struct {
	reader    io.ReadCloser
	cfg       *sse.ReadConfig
	events    chan sseReadResult
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

type sseReadResult struct {
	event sse.Event
	err   error
}

// NewSSESource creates a source from an HTTP response body.
func NewSSESource(reader io.ReadCloser, maxEventSize int) *SSESource {
	cfg := &sse.ReadConfig{MaxEventSize: maxEventSize}
	if maxEventSize <= 0 {
		cfg.MaxEventSize = 32 * 1024 * 1024 // 32MB default
	}

	s := &SSESource{
		reader: reader,
		cfg:    cfg,
		events: make(chan sseReadResult, 1),
		done:   make(chan struct{}),
	}

	// Start reading in background
	go s.readLoop()

	return s
}

func (s *SSESource) readLoop() {
	defer close(s.events)
	for ev, err := range sse.Read(s.reader, s.cfg) {
		select {
		case s.events <- sseReadResult{event: ev, err: err}:
			if err != nil {
				return
			}
		case <-s.done:
			return
		}
	}
}

// ReadEvent reads the next SSE event data.
func (s *SSESource) ReadEvent(ctx context.Context) ([]byte, error) {
	select {
	case result, ok := <-s.events:
		if !ok {
			return nil, io.EOF
		}
		if result.err != nil {
			return nil, result.err
		}
		return []byte(result.event.Data), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close releases the underlying reader.
func (s *SSESource) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		if s.reader != nil {
			s.closeErr = s.reader.Close()
		}
	})
	return s.closeErr
}
