package relay

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/coder/websocket"
)

func writeWSEvent(ctx context.Context, conn *websocket.Conn, event interface{}) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal ws event: %w", err)
	}
	writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	if err := conn.Write(writeCtx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("write ws event: %w", err)
	}
	return nil
}
