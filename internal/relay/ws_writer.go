package relay

import (
	"bytes"
	"context"
	"net/http"
	"sync"

	"github.com/coder/websocket"
)

// WSStreamWriter implements StreamWriter for WebSocket clients.
// It converts SSE "data: {...}\n\n" formatted bytes to bare JSON WebSocket text frames.
type WSStreamWriter struct {
	conn    *websocket.Conn
	ctx     context.Context
	written bool
	mu      sync.Mutex
}

func NewWSStreamWriter(ctx context.Context, conn *websocket.Conn) *WSStreamWriter {
	return &WSStreamWriter{
		conn: conn,
		ctx:  ctx,
	}
}

func (w *WSStreamWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Extract JSON data from SSE format "data: {...}\n\n"
	lines := extractSSEDataLines(data)
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		err := w.conn.Write(w.ctx, websocket.MessageText, line)
		if err != nil {
			return 0, err
		}
	}
	w.written = true
	return len(data), nil
}

func (w *WSStreamWriter) Flush() {
	// WebSocket sends are immediate, no buffering needed
}

func (w *WSStreamWriter) Written() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.written
}

func (w *WSStreamWriter) Header() http.Header {
	// WebSocket doesn't use HTTP headers for individual messages
	return http.Header{}
}

func (w *WSStreamWriter) WriteHeader(code int) {
	// WebSocket doesn't have per-message status codes
}

// extractSSEDataLines extracts the data payload from SSE formatted bytes.
// Input format: "data: {json}\n\n" or multiple such lines concatenated.
// Returns the raw JSON data for each line (without "data: " prefix).
func extractSSEDataLines(data []byte) [][]byte {
	var results [][]byte
	prefix := []byte("data: ")

	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if bytes.HasPrefix(line, prefix) {
			payload := line[len(prefix):]
			if len(payload) > 0 && !bytes.Equal(payload, []byte("[DONE]")) {
				results = append(results, payload)
			}
		}
	}

	return results
}
