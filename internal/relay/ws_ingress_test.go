package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

const wsIngressModel = "ws-ingress-model"

type wsIngressUpstream func(n int, body []byte, w http.ResponseWriter, r *http.Request)

// wsIngressEnv 把假的 HTTP 上游、真实的 HandleWSResponse 和一个 WS 客户端接在一起，
// 端到端测试 WS 入站。
type wsIngressEnv struct {
	t       *testing.T
	ctx     context.Context
	channel *model.Channel
	client  *websocket.Conn

	mu       sync.Mutex
	requests [][]byte
	headers  []http.Header // 上游收到的请求头，和 requests 一一对应
}

// wsIngressClientHeaders 是客户端握手时带的头，照着 Codex 的样子。
var wsIngressClientHeaders = http.Header{
	"User-Agent":    {"codex_cli_rs/0.0.0"},
	"Originator":    {"codex_cli_rs"},
	"Session_id":    {"sess_1"},
	"Openai-Beta":   {"responses_websockets=2026-02-06"},
	"Authorization": {"Bearer sk-octopus-client-key"},
}

func newWSIngressEnv(t *testing.T, upstream wsIngressUpstream) *wsIngressEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	env := &wsIngressEnv{t: t, ctx: ctx}

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		env.mu.Lock()
		env.requests = append(env.requests, body)
		env.headers = append(env.headers, r.Header.Clone())
		n := len(env.requests)
		env.mu.Unlock()
		upstream(n, body, w, r)
	}))
	t.Cleanup(upstreamServer.Close)

	channel := &model.Channel{
		Name:     "ws-ingress-channel",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: upstreamServer.URL + "/v1"}},
		Model:    "gpt-4o",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "upstream-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	env.channel = channel
	group := &model.Group{Name: wsIngressModel, Mode: model.GroupModeFailover, SessionKeepTime: 60}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "gpt-4o", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}

	engine := gin.New()
	handlerDone := make(chan struct{})
	engine.GET("/v1/responses", func(c *gin.Context) {
		defer close(handlerDone)
		c.Set("api_key_id", 77)
		HandleWSResponse(c)
	})
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	client, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses",
		&websocket.DialOptions{HTTPHeader: wsIngressClientHeaders})
	if err != nil {
		t.Fatalf("ws dial failed: %v", err)
	}
	client.SetReadLimit(1 << 20)
	// 等处理函数退出再做后面的清理：客户端读到结束事件之后它还在记账，不能和重置熔断器、关数据库同时跑
	t.Cleanup(func() {
		client.CloseNow()
		select {
		case <-handlerDone:
		case <-time.After(5 * time.Second):
			t.Errorf("HandleWSResponse did not return after the client closed the connection")
		}
	})
	env.client = client
	return env
}

// turn 发一条消息，读到结束事件为止；读失败时返回已收到的事件和错误。
func (e *wsIngressEnv) turn(timeout time.Duration, payload string) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := e.client.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
		return nil, err
	}
	var events []map[string]any
	for {
		_, data, err := e.client.Read(ctx)
		if err != nil {
			return events, err
		}
		var event map[string]any
		if err := json.Unmarshal(data, &event); err != nil {
			return events, fmt.Errorf("non-JSON frame %q: %w", data, err)
		}
		events = append(events, event)
		switch event["type"] {
		case "response.completed", "response.failed", "response.incomplete", "error":
			return events, nil
		}
	}
}

// mustComplete 跑一个回合，要求以 response.completed 结束，返回回复 ID。
func (e *wsIngressEnv) mustComplete(payload string) string {
	e.t.Helper()
	events, err := e.turn(10*time.Second, payload)
	if err != nil {
		e.t.Fatalf("turn failed after %d events: %v", len(events), err)
	}
	last := events[len(events)-1]
	if last["type"] != "response.completed" {
		e.t.Fatalf("expected response.completed, got %v", last)
	}
	response, _ := last["response"].(map[string]any)
	id, _ := response["id"].(string)
	if id == "" {
		e.t.Fatalf("response.completed carries no response id: %v", last)
	}
	return id
}

// post 走 HTTP 入站发一个 Responses 请求，用的是同一个渠道和上游。
func (e *wsIngressEnv) post(body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("api_key_id", 77)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeOpenAIResponse, c)
	return recorder
}

// upstreamRequests 返回上游收到的请求体，按到达顺序。
func (e *wsIngressEnv) upstreamRequests() []map[string]json.RawMessage {
	e.t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	requests := make([]map[string]json.RawMessage, len(e.requests))
	for i, body := range e.requests {
		if err := json.Unmarshal(body, &requests[i]); err != nil {
			e.t.Fatalf("upstream request %d is not a JSON object: %v", i+1, err)
		}
	}
	return requests
}

func wsIngressInputTexts(t *testing.T, request map[string]json.RawMessage) []string {
	t.Helper()
	var items []json.RawMessage
	if err := json.Unmarshal(request["input"], &items); err != nil {
		t.Fatalf("upstream input is not an array: %s", request["input"])
	}
	texts := make([]string, len(items))
	for i, item := range items {
		var parsed struct {
			Type    string `json:"type"`
			CallID  string `json:"call_id"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(item, &parsed); err != nil {
			t.Fatalf("upstream input item %d is not an object: %s", i, item)
		}
		texts[i] = parsed.Type + ":" + parsed.CallID
		if len(parsed.Content) > 0 {
			texts[i] = parsed.Type + ":" + parsed.Content[0].Text
		}
	}
	return texts
}

// wsIngressSameJSON 按内容比较 JSON：转发时键的顺序可以变，内容不能变。
func wsIngressSameJSON(t *testing.T, what string, got json.RawMessage, want string) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("%s is not JSON: %s", what, got)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("bad expectation for %s: %v", what, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("%s = %s, want %s", what, got, want)
	}
}

func wsIngressUserItem(text string) string {
	return fmt.Sprintf(`{"type":"message","role":"user","content":[{"type":"input_text","text":%q}]}`, text)
}

func wsIngressCreate(extra string, items ...string) string {
	return fmt.Sprintf(`{"type":"response.create","model":%q,%s"input":[%s]}`, wsIngressModel, extra, strings.Join(items, ","))
}

// wsIngressSSE 是第 n 个回复的完整 SSE 流。
func wsIngressSSE(n int) string {
	item := fmt.Sprintf(`{"id":"msg_%d","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"answer %d","annotations":[]}]}`, n, n)
	var b strings.Builder
	for _, event := range [][2]string{
		{"response.created", fmt.Sprintf(`"response":{"id":"resp_%d","object":"response","model":"gpt-4o","created_at":1,"status":"in_progress","output":[]}`, n)},
		{"response.output_item.added", fmt.Sprintf(`"output_index":0,"item":{"id":"msg_%d","type":"message","role":"assistant","status":"in_progress","content":[]}`, n)},
		{"response.output_text.delta", fmt.Sprintf(`"item_id":"msg_%d","output_index":0,"content_index":0,"delta":"answer %d"`, n, n)},
		{"response.output_item.done", `"output_index":0,"item":` + item},
		{"response.completed", fmt.Sprintf(`"response":{"id":"resp_%d","object":"response","model":"gpt-4o","created_at":1,"status":"completed","output":[%s],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`, n, item)},
	} {
		fmt.Fprintf(&b, "event: %s\ndata: {\"type\":%q,%s}\n\n", event[0], event[0], event[1])
	}
	return b.String()
}

// wsIngressStatelessUpstream 模仿 store=false 的 Responses 上游：
// 不认识任何 previous_response_id，其余请求正常回复。
func wsIngressStatelessUpstream(n int, body []byte, w http.ResponseWriter, _ *http.Request) {
	var request struct {
		PreviousResponseID string `json:"previous_response_id"`
	}
	_ = json.Unmarshal(body, &request)
	if request.PreviousResponseID != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":{"message":"Previous response with id '%s' not found.","type":"invalid_request_error","param":"previous_response_id","code":"previous_response_not_found"}}`, request.PreviousResponseID)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, wsIngressSSE(n))
}

func TestWSIngressContinuationSendsOneSelfContainedRequest(t *testing.T) {
	env := newWSIngressEnv(t, wsIngressStatelessUpstream)

	previousID := env.mustComplete(wsIngressCreate("", wsIngressUserItem("first")))
	for _, text := range []string{"second", "third"} {
		previousID = env.mustComplete(wsIngressCreate(fmt.Sprintf(`"previous_response_id":%q,`, previousID), wsIngressUserItem(text)))
	}

	requests := env.upstreamRequests()
	if len(requests) != 3 {
		t.Fatalf("expected one upstream request per turn, got %d requests", len(requests))
	}
	for i, want := range []string{
		"message:first",
		"message:first | message:answer 1 | message:second",
		"message:first | message:answer 1 | message:second | message:answer 2 | message:third",
	} {
		if _, ok := requests[i]["previous_response_id"]; ok {
			t.Fatalf("request %d must not send previous_response_id upstream: %s", i+1, requests[i]["previous_response_id"])
		}
		if got := strings.Join(wsIngressInputTexts(t, requests[i]), " | "); got != want {
			t.Fatalf("request %d input = %q, want %q", i+1, got, want)
		}
	}
}

// 没有 previous_response_id 的请求是完整请求，不能再和本地存档拼接。
func TestWSIngressFullRequestIsNotMergedWithStoredTurns(t *testing.T) {
	env := newWSIngressEnv(t, wsIngressStatelessUpstream)

	env.mustComplete(wsIngressCreate("", wsIngressUserItem("first")))
	env.mustComplete(wsIngressCreate("",
		wsIngressUserItem("first"),
		`{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"}`,
		`{"type":"function_call_output","call_id":"call_1","output":"ok"}`,
	))

	requests := env.upstreamRequests()
	if len(requests) != 2 {
		t.Fatalf("expected one upstream request per turn, got %d requests", len(requests))
	}
	got := strings.Join(wsIngressInputTexts(t, requests[1]), " | ")
	if want := "message:first | function_call:call_1 | function_call_output:call_1"; got != want {
		t.Fatalf("full request input = %q, want %q", got, want)
	}
}

// 客户端握手时带的头要跟着每个请求到上游（有的上游靠 User-Agent、originator 认客户端）。
// 握手专用的头和客户端给 Octopus 的凭据不能带过去。
func TestWSIngressForwardsClientHandshakeHeaders(t *testing.T) {
	env := newWSIngressEnv(t, wsIngressStatelessUpstream)
	env.mustComplete(wsIngressCreate("", wsIngressUserItem("hello")))

	env.mu.Lock()
	got := env.headers[0]
	env.mu.Unlock()
	for name, want := range map[string]string{
		"User-Agent":            "codex_cli_rs/0.0.0",
		"Originator":            "codex_cli_rs",
		"Session_id":            "sess_1",
		"Authorization":         "Bearer upstream-key",
		"Openai-Beta":           "",
		"Upgrade":               "",
		"Sec-Websocket-Key":     "",
		"Sec-Websocket-Version": "",
	} {
		if got.Get(name) != want {
			t.Errorf("upstream header %s = %q, want %q", name, got.Get(name), want)
		}
	}
}

// 存档是每个模型一份：一条连接上换了模型再换回来，原来那个模型的对话还接得上。
func TestWSIngressKeepsOneConversationPerModel(t *testing.T) {
	env := newWSIngressEnv(t, wsIngressStatelessUpstream)
	other := &model.Group{Name: "ws-ingress-other-model", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(other, env.ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: other.ID, ChannelID: env.channel.ID, ModelName: "gpt-4o", Priority: 1, Weight: 1}, env.ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}

	firstID := env.mustComplete(wsIngressCreate("", wsIngressUserItem("first")))
	env.mustComplete(strings.Replace(wsIngressCreate("", wsIngressUserItem("other")), wsIngressModel, other.Name, 1))
	env.mustComplete(wsIngressCreate(fmt.Sprintf(`"previous_response_id":%q,`, firstID), wsIngressUserItem("second")))

	requests := env.upstreamRequests()
	if len(requests) != 3 {
		t.Fatalf("expected one upstream request per turn, got %d requests", len(requests))
	}
	if got, want := strings.Join(wsIngressInputTexts(t, requests[2]), " | "), "message:first | message:answer 1 | message:second"; got != want {
		t.Fatalf("continued request input = %q, want %q", got, want)
	}
}

func TestWSIngressUnknownPreviousResponseIDIsReported(t *testing.T) {
	for name, tc := range map[string]struct {
		extra            string
		upstreamRequests int
	}{
		"普通请求原样转发一次，上游说找不到":   {`"previous_response_id":"resp_unknown",`, 1},
		"预热不在本地应答：它前面的历史本地没有": {`"previous_response_id":"resp_unknown","generate":false,`, 0},
	} {
		t.Run(name, func(t *testing.T) {
			env := newWSIngressEnv(t, wsIngressStatelessUpstream)

			events, err := env.turn(10*time.Second, wsIngressCreate(tc.extra, wsIngressUserItem("hello")))
			if err != nil {
				t.Fatalf("turn failed after %d events: %v", len(events), err)
			}
			last := events[len(events)-1]
			errorBody, _ := last["error"].(map[string]any)
			if len(events) != 1 || last["type"] != "error" || errorBody["code"] != "previous_response_not_found" || errorBody["type"] != "invalid_request_error" {
				t.Fatalf("expected only a previous_response_not_found error event, got %v", events)
			}
			if requests := env.upstreamRequests(); len(requests) != tc.upstreamRequests {
				t.Fatalf("expected %d upstream requests, got %d", tc.upstreamRequests, len(requests))
			}
		})
	}
}

// wsIngressChatUpstream 是 Chat Completions 上游：第 n 个回复的内容是 "answer n"。
// usage 为空时模仿不理会 include_usage 的上游：结束之后直接发 [DONE]。
func wsIngressChatUpstream(usage string) wsIngressUpstream {
	return func(n int, _ []byte, w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-%d\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"answer %d\"},\"finish_reason\":null}]}\n\n", n, n)
		fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-%d\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]%s}\n\n", n, usage)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
}

// 非 Responses 渠道从 Messages 生成请求：续传回合也要带上完整对话，WS 和 HTTP 入站都一样。
// 上游发不发用量，回合都要以 response.completed 结束。
func TestContinuationOnChatChannelCarriesConversation(t *testing.T) {
	for _, tc := range []struct{ ingress, usage string }{
		{"ws", `,"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}`},
		{"http", `,"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}`},
		{"ws", ""},
		{"http", ""},
	} {
		ingress := tc.ingress
		t.Run(fmt.Sprintf("%s，上游发用量=%t", ingress, tc.usage != ""), func(t *testing.T) {
			env := newWSIngressEnv(t, wsIngressChatUpstream(tc.usage))
			chatType := outbound.OutboundTypeOpenAIChat
			if _, err := op.ChannelUpdate(&model.ChannelUpdateRequest{ID: env.channel.ID, Type: &chatType}, env.ctx); err != nil {
				t.Fatalf("ChannelUpdate failed: %v", err)
			}

			for n, text := range []string{"first", "second", "third"} {
				extra := `"stream":true,"instructions":"be brief",`
				if n > 0 {
					extra += fmt.Sprintf(`"previous_response_id":"chatcmpl-%d",`, n)
				}
				payload := wsIngressCreate(extra, wsIngressUserItem(text))
				if ingress == "ws" {
					env.mustComplete(payload)
				} else if recorder := env.post(payload); recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"type":"response.completed"`) {
					t.Fatalf("turn %d status = %d, want 200 and a response.completed event; body %s", n+1, recorder.Code, recorder.Body.String())
				}
			}

			requests := env.upstreamRequests()
			if len(requests) != 3 {
				t.Fatalf("expected one upstream request per turn, got %d requests", len(requests))
			}
			var messages []struct{ Role, Content string }
			if err := json.Unmarshal(requests[2]["messages"], &messages); err != nil {
				t.Fatalf("upstream messages are not role/text pairs: %s", requests[2]["messages"])
			}
			var got []string
			for _, message := range messages {
				got = append(got, message.Role+":"+message.Content)
			}
			want := "system:be brief | user:first | assistant:answer 1 | user:second | assistant:answer 2 | user:third"
			if strings.Join(got, " | ") != want {
				t.Fatalf("third turn messages = %q, want %q", strings.Join(got, " | "), want)
			}
		})
	}
}

// 同协议渠道全程用原始 JSON：内部结构不认识的工具、字段和输出条目都不能丢。
func TestWSIngressKeepsRawRequestAndOutput(t *testing.T) {
	const (
		customTool   = `{"type":"custom","name":"apply_patch","description":"patch files","format":{"type":"grammar","syntax":"lark","definition":"start: /.+/"}}`
		customCall   = `{"id":"ctc_1","type":"custom_tool_call","status":"completed","call_id":"call_1","name":"apply_patch","input":"*** Begin Patch"}`
		customOutput = `{"type":"custom_tool_call_output","call_id":"call_1","output":"done"}`
		extra        = `"client_metadata":{"origin":"test"},"tools":[` + customTool + `],`
	)
	env := newWSIngressEnv(t, func(n int, _ []byte, w http.ResponseWriter, _ *http.Request) {
		// 有些上游的结束事件里 output 是空的，条目只出现在 output_item.done 里
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_%d\",\"object\":\"response\",\"model\":\"gpt-4o\",\"status\":\"in_progress\",\"output\":[]}}\n\n", n)
		fmt.Fprintf(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":%s}\n\n", customCall)
		fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_%d\",\"object\":\"response\",\"model\":\"gpt-4o\",\"status\":\"completed\",\"output\":[]}}\n\n", n)
	})

	firstID := env.mustComplete(wsIngressCreate(extra, wsIngressUserItem("patch it")))
	env.mustComplete(wsIngressCreate(extra+fmt.Sprintf(`"previous_response_id":%q,`, firstID), customOutput))

	requests := env.upstreamRequests()
	if len(requests) != 2 {
		t.Fatalf("expected one upstream request per turn, got %d requests", len(requests))
	}
	for i, request := range requests {
		wsIngressSameJSON(t, fmt.Sprintf("request %d tools", i+1), request["tools"], "["+customTool+"]")
		wsIngressSameJSON(t, fmt.Sprintf("request %d client_metadata", i+1), request["client_metadata"], `{"origin":"test"}`)
	}
	wsIngressSameJSON(t, "continuation input", requests[1]["input"],
		"["+wsIngressUserItem("patch it")+","+customCall+","+customOutput+"]")
}

// 带原生工具的请求只能走 OpenAI Responses 渠道，不能被转换成别的协议后丢掉工具。
func TestWSIngressNativeToolsNeedResponsesChannel(t *testing.T) {
	env := newWSIngressEnv(t, wsIngressStatelessUpstream)
	chatType := outbound.OutboundTypeOpenAIChat
	if _, err := op.ChannelUpdate(&model.ChannelUpdateRequest{ID: env.channel.ID, Type: &chatType}, env.ctx); err != nil {
		t.Fatalf("ChannelUpdate failed: %v", err)
	}

	events, err := env.turn(10*time.Second, wsIngressCreate(`"tools":[{"type":"custom","name":"apply_patch"}],`, wsIngressUserItem("hello")))
	if err != nil {
		t.Fatalf("turn failed after %d events: %v", len(events), err)
	}
	if last := events[len(events)-1]; last["type"] != "error" || last["status"] != float64(http.StatusBadRequest) {
		t.Fatalf("expected a 400 error event, got %v", last)
	}
	if requests := env.upstreamRequests(); len(requests) != 0 {
		t.Fatalf("expected the chat channel to be skipped, got %d upstream requests", len(requests))
	}
}

// HTTP 入站的回放也用原始请求：续传字段去掉，其余字段原样保留。
func TestHTTPReplayKeepsRawRequestFields(t *testing.T) {
	env := newWSIngressEnv(t, wsIngressStatelessUpstream)
	const extra = `"stream":true,"client_metadata":{"origin":"test"},`

	if first := env.post(wsIngressCreate(extra, wsIngressUserItem("first"))); first.Code != http.StatusOK {
		t.Fatalf("first turn status = %d, body %s", first.Code, first.Body.String())
	}
	if second := env.post(wsIngressCreate(extra+`"previous_response_id":"resp_1",`, wsIngressUserItem("second"))); second.Code != http.StatusOK {
		t.Fatalf("second turn status = %d, body %s", second.Code, second.Body.String())
	}

	requests := env.upstreamRequests()
	if len(requests) != 2 {
		t.Fatalf("expected one upstream request per turn, got %d requests", len(requests))
	}
	if _, ok := requests[1]["previous_response_id"]; ok {
		t.Fatalf("replay must not send previous_response_id upstream: %s", requests[1]["previous_response_id"])
	}
	wsIngressSameJSON(t, "replay client_metadata", requests[1]["client_metadata"], `{"origin":"test"}`)
	got := strings.Join(wsIngressInputTexts(t, requests[1]), " | ")
	if want := "message:first | message:answer 1 | message:second"; got != want {
		t.Fatalf("replay input = %q, want %q", got, want)
	}
}

// 预热（generate:false）在本地应答，不发给上游。它的输入记进存档：
// 下一轮带着预热 ID 来时，和别的续传一样拼成完整请求。
func TestWSIngressPrewarmIsAnsweredLocally(t *testing.T) {
	turnInput := []string{wsIngressUserItem("context"), wsIngressUserItem("hello")}
	for name, tc := range map[string]struct{ prewarmInput, firstTurnInput []string }{
		"预热不带输入，第一个请求带全部输入":      {nil, turnInput},
		"预热带了这一轮的输入，第一个请求的输入是空的": {turnInput, nil},
	} {
		t.Run(name, func(t *testing.T) {
			env := newWSIngressEnv(t, wsIngressStatelessUpstream)

			events, err := env.turn(3*time.Second, wsIngressCreate(`"generate":false,"instructions":"be brief",`, tc.prewarmInput...))
			if err != nil {
				t.Fatalf("prewarm got no terminal event after %d events: %v", len(events), err)
			}
			if len(events) != 2 || events[0]["type"] != "response.created" || events[1]["type"] != "response.completed" {
				t.Fatalf("prewarm events = %v, want response.created then response.completed", events)
			}
			response, _ := events[1]["response"].(map[string]any)
			prewarmID, _ := response["id"].(string)
			if !strings.HasPrefix(prewarmID, "resp_prewarm_") {
				t.Fatalf("prewarm response id = %q, want a resp_prewarm_ id", prewarmID)
			}
			if n := len(env.upstreamRequests()); n != 0 {
				t.Fatalf("prewarm must not reach the upstream, got %d requests", n)
			}

			previousID := env.mustComplete(wsIngressCreate(fmt.Sprintf(`"previous_response_id":%q,"instructions":"be brief",`, prewarmID), tc.firstTurnInput...))
			env.mustComplete(wsIngressCreate(fmt.Sprintf(`"previous_response_id":%q,"instructions":"be brief",`, previousID), wsIngressUserItem("again")))

			requests := env.upstreamRequests()
			if len(requests) != 2 {
				t.Fatalf("expected one upstream request per real turn, got %d requests", len(requests))
			}
			for i, want := range []string{
				"message:context | message:hello",
				"message:context | message:hello | message:answer 1 | message:again",
			} {
				for _, field := range []string{"previous_response_id", "generate"} {
					if value, ok := requests[i][field]; ok {
						t.Fatalf("request %d must not send %s upstream: %s", i+1, field, value)
					}
				}
				if got := strings.Join(wsIngressInputTexts(t, requests[i]), " | "); got != want {
					t.Fatalf("request %d input = %q, want %q", i+1, got, want)
				}
			}
		})
	}
}

// 回合失败时客户端一定收到 error 事件：WS 上没有“流关闭”这个信号，不发的话客户端会一直等。
func TestWSIngressFailedTurnEndsWithErrorEvent(t *testing.T) {
	full := wsIngressSSE(1)
	withoutTerminal := full[:strings.Index(full, "event: response.completed")]
	for name, tc := range map[string]struct {
		firstReply      func(w http.ResponseWriter)
		status          float64
		code, errorType string
		partialOutput   bool
	}{
		"上游中途断开": {func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, withoutTerminal[:strings.Index(withoutTerminal, "event: response.output_item.done")])
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}, 502, "upstream_stream_interrupted", "server_error", true},
		"上游的流没有结束事件就结束了": {func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, withoutTerminal)
		}, 502, "upstream_stream_interrupted", "server_error", true},
		"上游限流": {func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"Rate limit reached","type":"rate_limit_error","code":"rate_limit_exceeded"}}`)
		}, 429, "upstream_rate_limited", "rate_limit_error", false},
	} {
		t.Run(name, func(t *testing.T) {
			env := newWSIngressEnv(t, func(n int, body []byte, w http.ResponseWriter, r *http.Request) {
				if n == 1 {
					tc.firstReply(w)
					return
				}
				wsIngressStatelessUpstream(n, body, w, r)
			})

			request := wsIngressCreate("", wsIngressUserItem("hello"))
			events, err := env.turn(3*time.Second, request)
			if err != nil {
				t.Fatalf("failed turn got no terminal event after %d events: %v", len(events), err)
			}
			last := events[len(events)-1]
			errorBody, _ := last["error"].(map[string]any)
			if last["type"] != "error" || last["status"] != tc.status || errorBody["code"] != tc.code || errorBody["type"] != tc.errorType {
				t.Fatalf("last event = %v, want an error event with %v %s %s", last, tc.status, tc.code, tc.errorType)
			}
			if tc.partialOutput != (len(events) > 1) {
				t.Fatalf("partial output before the error = %t, want %t (%d events)", len(events) > 1, tc.partialOutput, len(events))
			}
			// 连接还能用：把同一个请求重发一次，能完成
			env.mustComplete(request)
		})
	}
}

// 连接到了时限：客户端先收到 error 事件（Codex 认识这个码，会换一条新连接），然后连接正常关闭。
func TestWSIngressConnectionLimitIsReported(t *testing.T) {
	limit := wsClientMaxAge
	wsClientMaxAge = 300 * time.Millisecond
	t.Cleanup(func() { wsClientMaxAge = limit })

	for name, midTurn := range map[string]bool{"空闲时": false, "回合进行中": true} {
		t.Run(name, func(t *testing.T) {
			env := newWSIngressEnv(t, func(_ int, _ []byte, _ http.ResponseWriter, r *http.Request) {
				<-r.Context().Done() // 一直不回复，直到 Octopus 取消这个请求
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if midTurn {
				if err := env.client.Write(ctx, websocket.MessageText, []byte(wsIngressCreate("", wsIngressUserItem("hello")))); err != nil {
					t.Fatalf("write failed: %v", err)
				}
			}

			_, data, err := env.client.Read(ctx)
			if err != nil {
				t.Fatalf("expected the connection limit error event, got %v", err)
			}
			var event struct {
				Type  string `json:"type"`
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if json.Unmarshal(data, &event) != nil || event.Type != "error" || event.Error.Code != "websocket_connection_limit_reached" {
				t.Fatalf("expected the connection limit error event, got %s", data)
			}
			if _, _, err := env.client.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				t.Fatalf("expected a normal close after the error event, got %v", err)
			}
		})
	}
}

// 上游自己用 response.failed 报告失败：客户端已经收到结束事件，不再补 error 事件；
// 这个回合也不记进存档，客户端带着原来的 previous_response_id 重试时还能接上。
func TestWSIngressUpstreamFailedTurnIsNotRecorded(t *testing.T) {
	env := newWSIngressEnv(t, func(n int, body []byte, w http.ResponseWriter, r *http.Request) {
		if n != 2 {
			wsIngressStatelessUpstream(n, body, w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, status := range []string{"created", "failed"} {
			fmt.Fprintf(w, "data: {\"type\":\"response.%s\",\"response\":{\"id\":\"resp_2\",\"object\":\"response\",\"model\":\"gpt-4o\",\"created_at\":1,\"output\":[]}}\n\n", status)
		}
	})

	firstID := env.mustComplete(wsIngressCreate("", wsIngressUserItem("first")))
	second := wsIngressCreate(fmt.Sprintf(`"previous_response_id":%q,`, firstID), wsIngressUserItem("second"))
	events, err := env.turn(3*time.Second, second)
	if err != nil || len(events) != 2 || events[1]["type"] != "response.failed" {
		t.Fatalf("expected the upstream's response.created and response.failed, got %v (err=%v)", events, err)
	}
	// 重试：如果上一轮多发了 error 事件，这里会先读到它；如果失败的回合进了存档，这里接不上
	env.mustComplete(second)

	requests := env.upstreamRequests()
	if len(requests) != 3 {
		t.Fatalf("expected one upstream request per turn, got %d requests", len(requests))
	}
	if got, want := strings.Join(wsIngressInputTexts(t, requests[2]), " | "), "message:first | message:answer 1 | message:second"; got != want {
		t.Fatalf("retried request input = %q, want %q", got, want)
	}
}
