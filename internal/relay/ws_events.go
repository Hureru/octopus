package relay

import (
	"context"
	"encoding/json"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/coder/websocket"
)

func writeWSEvent(ctx context.Context, conn *websocket.Conn, event interface{}) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Warnf("ws event marshal failed: %v", err)
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	if err := conn.Write(writeCtx, websocket.MessageText, data); err != nil {
		log.Debugf("ws event write failed: %v", err)
	}
}
