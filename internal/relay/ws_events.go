package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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

// writeWSPrewarmAck 给预热请求回一个空的回合，返回它的回复 ID。
// 两条事件都要发：Codex 等的是 response.completed。
func writeWSPrewarmAck(ctx context.Context, conn *websocket.Conn, model string) (string, error) {
	now := time.Now()
	id := fmt.Sprintf("resp_prewarm_%d", now.UnixNano())
	response := map[string]any{
		"id":         id,
		"object":     "response",
		"created_at": now.Unix(),
		"status":     "in_progress",
		"model":      model,
		"output":     []any{},
	}
	if err := writeWSEvent(ctx, conn, map[string]any{"type": "response.created", "sequence_number": 0, "response": response}); err != nil {
		return "", err
	}
	response["status"] = "completed"
	response["usage"] = map[string]any{
		"input_tokens":          0,
		"input_tokens_details":  map[string]any{"cached_tokens": 0},
		"output_tokens":         0,
		"output_tokens_details": map[string]any{"reasoning_tokens": 0},
		"total_tokens":          0,
	}
	return id, writeWSEvent(ctx, conn, map[string]any{"type": "response.completed", "sequence_number": 1, "response": response})
}
