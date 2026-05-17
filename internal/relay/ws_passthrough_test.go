package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

func TestForwardViaWSPassthroughNormalizesPayloadAndRecordsMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	if err := op.SettingSetString(model.SettingKeyResponsesWSEnabled, "true"); err != nil {
		t.Fatalf("SettingSetString responses ws enabled failed: %v", err)
	}
	if err := op.SettingSetString(model.SettingKeyResponsesWSDefaultMode, "passthrough"); err != nil {
		t.Fatalf("SettingSetString responses ws mode failed: %v", err)
	}

	payloadCh := make(chan map[string]json.RawMessage, 1)
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("OpenAI-Beta"); got != "responses_websockets=2026-02-06" {
			t.Errorf("expected OpenAI-Beta header, got %q", got)
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(data, &payload); err == nil {
			payloadCh <- payload
		}
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"response.created","response":{"id":"resp_passthrough","model":"gpt-4o"}}`))
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"response.output_text.delta","response":{"id":"resp_passthrough","model":"gpt-4o"},"delta":"ok"}`))
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_passthrough","model":"gpt-4o","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":3,"input_tokens_details":{"cached_tokens":1},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":5}}}`))
	}))
	defer wsServer.Close()

	channel := &model.Channel{
		Name:     "relay-ws-passthrough",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: wsServer.URL + "/v1"}},
		Model:    "gpt-4o",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "passthrough-key"}},
		WSMode:   model.ChannelWSModePassthrough,
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	clientConn, serverConn := newTestWSConnPair(t)
	defer clientConn.Close(websocket.StatusNormalClosure, "")
	defer serverConn.Close(websocket.StatusNormalClosure, "")

	rawBody := []byte(`{"type":"response.create","model":"client-model","input":"hello","stream":false,"background":true}`)
	internalReq := &transformerModel.InternalLLMRequest{Model: "gpt-4o", Stream: boolPtr(true), RawAPIFormat: transformerModel.APIFormatOpenAIResponse}
	req := &relayRequest{
		c:               nil,
		ctx:             context.Background(),
		inAdapter:       inbound.Get(inbound.InboundTypeOpenAIResponse),
		internalRequest: internalReq,
		metrics:         NewRelayMetrics(1, "client-model", rawBody, internalReq),
		apiKeyID:        1,
		requestModel:    "client-model",
		groupID:         1,
		groupSessionTTL: 60,
		rawBody:         rawBody,
		streamWriter:    NewWSStreamWriter(context.Background(), serverConn),
	}
	ra := &relayAttempt{relayRequest: req, outAdapter: outbound.Get(channel.Type), channel: channel, usedKey: channel.Keys[0]}

	status, err := ra.forwardViaWS(context.Background())
	if err != nil || status != http.StatusOK {
		t.Fatalf("forwardViaWS passthrough failed status=%d err=%v", status, err)
	}

	payload := <-payloadCh
	if got := string(payload["type"]); got != `"response.create"` {
		t.Fatalf("expected response.create type, got %s", got)
	}
	if got := string(payload["stream"]); got != `true` {
		t.Fatalf("expected stream true, got %s", got)
	}
	if _, ok := payload["background"]; ok {
		t.Fatalf("expected background to be removed: %#v", payload)
	}
	if got := string(payload["model"]); got != `"gpt-4o"` {
		t.Fatalf("expected upstream model rewrite, got %s", got)
	}

	var downstreamModels []string
	for i := 0; i < 3; i++ {
		_, data, err := clientConn.Read(context.Background())
		if err != nil {
			t.Fatalf("client read downstream event %d failed: %v", i, err)
		}
		if strings.Contains(string(data), "gpt-4o") {
			t.Fatalf("expected downstream model replacement, got %s", data)
		}
		if strings.Contains(string(data), "client-model") {
			downstreamModels = append(downstreamModels, "client-model")
		}
	}
	if len(downstreamModels) == 0 {
		t.Fatalf("expected at least one downstream model replacement")
	}
	if req.metrics.WSExecMode == nil || *req.metrics.WSExecMode != model.RelayLogWSExecModePassthrough {
		t.Fatalf("expected passthrough ws exec mode, got %#v", req.metrics.WSExecMode)
	}
	if req.metrics.InternalResponse == nil || req.metrics.InternalResponse.ID != "resp_passthrough" {
		t.Fatalf("expected internal response id to be captured, got %#v", req.metrics.InternalResponse)
	}
	if req.metrics.InternalResponse.Usage == nil || req.metrics.InternalResponse.Usage.PromptTokens != 3 || req.metrics.InternalResponse.Usage.CompletionTokens != 2 {
		t.Fatalf("expected usage to be captured, got %#v", req.metrics.InternalResponse.Usage)
	}
	if _, ok := getWSResponseConn("resp_passthrough"); !ok {
		t.Fatalf("expected local response->conn affinity")
	}
	if _, ok := getWSAffinityStore().Get(context.Background(), wsAffinityScope{APIKeyID: 1, GroupID: 1, RequestModel: "client-model", ResponseID: "resp_passthrough"}); !ok {
		t.Fatalf("expected db response affinity")
	}
}

func newTestWSConnPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	serverConnCh := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		serverConnCh <- conn
		<-r.Context().Done()
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}))
	t.Cleanup(server.Close)
	clientConn, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial test ws pair failed: %v", err)
	}
	select {
	case serverConn := <-serverConnCh:
		return clientConn, serverConn
	case <-time.After(5 * time.Second):
		clientConn.Close(websocket.StatusNormalClosure, "")
		t.Fatalf("timed out waiting for server side websocket")
		return nil, nil
	}
}
