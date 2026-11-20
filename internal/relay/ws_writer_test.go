package relay

import (
	"context"
	"testing"
	"time"
)

// 直通读到的数据块可以从任何位置切开，冒号后面也不一定有空格。
func TestWSStreamWriterJoinsLinesSplitAcrossWrites(t *testing.T) {
	clientConn, serverConn := newTestWSConnPair(t)
	writer := NewWSStreamWriter(context.Background(), serverConn)

	for _, chunk := range []string{
		"event: response.created\ndata: {\"type\":\"respo",
		"nse.created\"}\n\nevent: response.output_text.delta\nda",
		"ta:{\"type\":\"response.output_text.delta\"}\n",
		"\n",
	} {
		if _, err := writer.Write([]byte(chunk)); err != nil {
			t.Fatalf("Write(%q) failed: %v", chunk, err)
		}
	}

	for _, want := range []string{`{"type":"response.created"}`, `{"type":"response.output_text.delta"}`} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, got, err := clientConn.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("expected frame %s, got error: %v", want, err)
		}
		if string(got) != want {
			t.Fatalf("frame = %s, want %s", got, want)
		}
	}
}

// 一直不换行的流不能无限攒下去：超过单个 SSE 事件的上限就报错。
func TestWSStreamWriterBoundsUnfinishedLine(t *testing.T) {
	limit := maxSSEEventSize
	maxSSEEventSize = 16
	t.Cleanup(func() { maxSSEEventSize = limit })
	writer := NewWSStreamWriter(context.Background(), nil) // 攒半行时不碰连接

	if _, err := writer.Write([]byte("data: {\"a\":")); err != nil {
		t.Fatalf("a short unfinished line must be kept, got %v", err)
	}
	if _, err := writer.Write([]byte("\"0123456789\"")); err == nil {
		t.Fatalf("expected an error once the unfinished line exceeds the limit")
	}
}
