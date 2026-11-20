package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/coder/websocket"
)

// writeWSFrame 给客户端发一帧文本，只受 wsWriteTimeout 限制。
// 不跟着调用方的 ctx 取消：ctx 过期时（比如连接到了时限）这个库会直接断开连接，最后的错误事件就发不出去了。
func writeWSFrame(ctx context.Context, conn *websocket.Conn, payload []byte) error {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), wsWriteTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, payload)
}

func writeWSEvent(ctx context.Context, conn *websocket.Conn, event interface{}) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal ws event: %w", err)
	}
	if err := writeWSFrame(ctx, conn, data); err != nil {
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
