package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
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

func newTestWSConnPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	serverConnCh := make(chan *websocket.Conn, 1)
	// 连接被接管之后，请求的 context 要等处理函数返回才取消，所以不能靠它来结束处理函数
	testDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		serverConnCh <- conn
		<-testDone
		_ = conn.CloseNow()
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(testDone) })
	clientConn, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial test ws pair failed: %v", err)
	}
	t.Cleanup(func() { _ = clientConn.CloseNow() })
	select {
	case serverConn := <-serverConnCh:
		return clientConn, serverConn
	case <-time.After(5 * time.Second):
		clientConn.Close(websocket.StatusNormalClosure, "")
		t.Fatalf("timed out waiting for server side websocket")
		return nil, nil
	}
}
