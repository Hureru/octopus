package relay

import (
	"context"
	"encoding/json"

	"github.com/coder/websocket"
)

func writeWSEvent(ctx context.Context, conn *websocket.Conn, event interface{}) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	_ = conn.Write(writeCtx, websocket.MessageText, data)
}
