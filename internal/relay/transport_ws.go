package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/coder/websocket"
)

// wsUpstreamReader reads events from an upstream WebSocket connection.
type wsUpstreamReader struct {
	conn       *websocket.Conn
	pc         *pooledConn
	channelID  int
	keyID      int
	closed     bool
	done       bool // true after a terminal event has been returned
	statusCode int
}

func newWSUpstreamReader(pc *pooledConn, channelID, keyID int) *wsUpstreamReader {
	return &wsUpstreamReader{
		conn:       pc.conn,
		pc:         pc,
		channelID:  channelID,
		keyID:      keyID,
		statusCode: 200,
	}
}

func (r *wsUpstreamReader) ReadEvent(ctx context.Context) ([]byte, error) {
	if r.closed || r.done {
		return nil, io.EOF
	}

	msgType, data, err := r.conn.Read(ctx)
	if err != nil {
		// Check if it's a normal close
		closeStatus := websocket.CloseStatus(err)
		if closeStatus == websocket.StatusNormalClosure || closeStatus == websocket.StatusGoingAway {
			return nil, io.EOF
		}
		switch closeStatus {
		case websocket.StatusPolicyViolation:
			r.statusCode = http.StatusConflict
		case websocket.StatusTryAgainLater:
			r.statusCode = http.StatusServiceUnavailable
		default:
			if r.statusCode < 400 {
				r.statusCode = http.StatusBadGateway
			}
		}
		return nil, fmt.Errorf("ws read error: %w", err)
	}

	if msgType != websocket.MessageText {
		return nil, fmt.Errorf("unexpected ws message type: %d", msgType)
	}

	// Check for error and terminal events.
	var event struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
		Response *struct {
			Status string `json:"status"`
			Error  *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal(data, &event) == nil {
		if isWSStreamTerminalEvent(event.Type) {
			r.done = true
		}
		if event.Response != nil && (event.Response.Status == "failed" || event.Response.Status == "incomplete" || event.Response.Status == "cancelled" || event.Response.Status == "canceled") {
			r.done = true
		}
		if isWSStreamErrorEvent(event.Type) || event.Error != nil || (event.Response != nil && event.Response.Error != nil) {
			if event.Status > 0 {
				r.statusCode = event.Status
			} else if r.statusCode < 400 {
				r.statusCode = http.StatusBadGateway
			}
			errCode := ""
			errMsg := "upstream ws error"
			if event.Error != nil {
				errMsg = event.Error.Message
				errCode = event.Error.Code
			}
			if event.Response != nil && event.Response.Error != nil {
				errMsg = event.Response.Error.Message
				errCode = event.Response.Error.Code
			}
			return nil, fmt.Errorf("%s (code=%s, status=%d)", errMsg, errCode, r.statusCode)
		}
	}

	return data, nil
}

func (r *wsUpstreamReader) StatusCode() int {
	return r.statusCode
}

func (r *wsUpstreamReader) Headers() http.Header {
	return http.Header{
		"Content-Type": []string{"text/event-stream"},
	}
}

func (r *wsUpstreamReader) Body() io.ReadCloser {
	return nil // WS doesn't have a body
}

func (r *wsUpstreamReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	// Return connection to pool (don't close it)
	wsUpstreamPool.Put(r.pc)
	log.Debugf("upstream WS connection returned to pool (channel=%d, key=%d)", r.channelID, r.keyID)
	return nil
}

// CloseWithError closes the reader and removes the connection from pool.
func (r *wsUpstreamReader) CloseWithError() {
	if r.closed {
		return
	}
	r.closed = true
	wsUpstreamPool.RemoveConn(r.pc)
}
