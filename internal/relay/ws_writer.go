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
	pending []byte // 还没收到行尾的半行
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

	// 直通转发时，上游的数据块可能从一行中间切开：没收完的半行留到下次再发。
	lineEnd := bytes.LastIndexByte(data, '\n') + 1
	if lineEnd == 0 {
		w.pending = append(w.pending, data...)
		return len(data), nil
	}
	complete := data[:lineEnd]
	if len(w.pending) > 0 {
		complete = append(w.pending, complete...)
	}
	// complete 可能和 pending 共用内存，所以帧发完才能覆盖 pending
	defer func() { w.pending = append(w.pending[:0], data[lineEnd:]...) }()

	// Extract JSON data from SSE format "data: {...}\n\n"
	lines := extractSSEDataLines(complete)
	wroteFrame := false
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		writeCtx, cancel := context.WithTimeout(w.ctx, wsWriteTimeout)
		err := w.conn.Write(writeCtx, websocket.MessageText, line)
		cancel()
		if err != nil {
			return 0, err
		}
		wroteFrame = true
	}
	if wroteFrame {
		w.written = true
	}
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
	prefix := []byte("data:") // SSE 允许冒号后面不带空格

	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if bytes.HasPrefix(line, prefix) {
			payload := bytes.TrimSpace(line[len(prefix):])
			if len(payload) > 0 && !bytes.Equal(payload, []byte("[DONE]")) {
				results = append(results, payload)
			}
		}
	}

	return results
}
