package relay

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

const wsIngressModel = "ws-ingress-model"

type wsIngressUpstream func(n int, body []byte, w http.ResponseWriter, r *http.Request)

// wsIngressWSUpstream 是假上游的 WS 一侧：每收到一条 response.create 调一次。
// n 是第几个请求（HTTP 和 WS 一起数），handshake 是这条连接握手时的请求头。返回 false 表示关掉这条连接。
type wsIngressWSUpstream func(n int, handshake http.Header, conn *websocket.Conn) bool

type wsIngressWSRequest struct {
	dial int // 第几条连接，从 1 开始
	body map[string]json.RawMessage
}

// wsIngressEnv 把假的上游、真实的 HandleWSResponse 和一个 WS 客户端接在一起，
// 端到端测试 WS 入站。
type wsIngressEnv struct {
	t       *testing.T
	ctx     context.Context
	apiKey  *model.APIKey // 客户端用的 Key，就是 wsIngressClientHeaders 里那把
	channel *model.Channel
	group   *model.Group
	baseURL string
	url     string // 客户端连 Octopus 的地址
	client  *websocket.Conn

	mu       sync.Mutex
	requests [][]byte
	headers  []http.Header // 上游收到的请求头，和 requests 一一对应

	wsUpstream wsIngressWSUpstream  // nil：上游不支持 WS，握手回 404
	wsDials    []http.Header        // 上游收到的每次 WS 握手的请求头，被拒绝的也算
	wsRequests []wsIngressWSRequest // 上游从 WS 收到的 response.create，按到达顺序
}

// wsIngressClientHeaders 是客户端握手时带的头，照着 Codex 的样子。
var wsIngressClientHeaders = http.Header{
	"User-Agent":    {"codex_cli_rs/0.0.0"},
	"Originator":    {"codex_cli_rs"},
	"Session_id":    {"sess_1"},
	"Openai-Beta":   {"responses_websockets=2026-02-06"},
	"Authorization": {"Bearer sk-octopus-client-key"},
}

// keyOptions 在建客户端的 Key 之前改它（比如加上每分钟次数的限制）。
func newWSIngressEnv(t *testing.T, upstream wsIngressUpstream, keyOptions ...func(*model.APIKey)) *wsIngressEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	// 请求日志的内存缓存是全局的：清掉前面的测试留下的
	if err := op.RelayLogClear(ctx); err != nil {
		t.Fatalf("RelayLogClear failed: %v", err)
	}
	env := &wsIngressEnv{t: t, ctx: ctx}

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "" {
			env.serveUpstreamWS(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		env.mu.Lock()
		env.requests = append(env.requests, body)
		env.headers = append(env.headers, r.Header.Clone())
		n := len(env.requests) + len(env.wsRequests)
		env.mu.Unlock()
		upstream(n, body, w, r)
	}))
	t.Cleanup(upstreamServer.Close)
	env.baseURL = upstreamServer.URL + "/v1"

	env.group = &model.Group{Name: wsIngressModel, Mode: model.GroupModeFailover, SessionKeepTime: 60}
	if err := op.GroupCreate(env.group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	env.channel = env.addChannel("upstream-key")

	env.apiKey = &model.APIKey{Name: "ws-ingress-key", APIKey: "sk-octopus-client-key", Enabled: true}
	for _, option := range keyOptions {
		option(env.apiKey)
	}
	if err := op.APIKeyCreate(env.apiKey, ctx); err != nil {
		t.Fatalf("APIKeyCreate failed: %v", err)
	}
	// 每分钟次数按 Key ID 记在内存里，而每个测试的 Key 都是 ID 1：清掉前面的测试留下的
	op.RateLimitDel(env.apiKey.ID)

	engine := gin.New()
	handlerDone := make(chan struct{})
	// 握手走真实的鉴权中间件，和线上一样
	engine.GET("/v1/responses", middleware.APIKeyAuth(), func(c *gin.Context) {
		defer close(handlerDone)
		HandleWSResponse(c)
	})
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	env.url = "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/responses"
	client, _, err := websocket.Dial(context.Background(), env.url, &websocket.DialOptions{HTTPHeader: wsIngressClientHeaders})
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

// addChannel 在同一个假上游上加一个渠道，排在已有渠道后面。上游靠 Key 认出是哪个渠道。
func (e *wsIngressEnv) addChannel(key string) *model.Channel {
	e.t.Helper()
	channel := &model.Channel{
		Name:     "ws-ingress-channel-" + key,
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: e.baseURL}},
		Model:    "gpt-4o",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: key}},
	}
	if err := op.ChannelCreate(channel, e.ctx); err != nil {
		e.t.Fatalf("ChannelCreate failed: %v", err)
	}
	priority := 1
	if e.channel != nil {
		priority = 2
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: e.group.ID, ChannelID: channel.ID, ModelName: "gpt-4o", Priority: priority, Weight: 1}, e.ctx); err != nil {
		e.t.Fatalf("GroupItemAdd failed: %v", err)
	}
	return channel
}

// useUpstreamWS 设置假上游的 WS 一侧。不调用它时上游不支持 WS，握手回 404。
func (e *wsIngressEnv) useUpstreamWS(upstream wsIngressWSUpstream) {
	e.mu.Lock()
	e.wsUpstream = upstream
	e.mu.Unlock()
}

func (e *wsIngressEnv) serveUpstreamWS(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	e.wsDials = append(e.wsDials, r.Header.Clone())
	dial, upstream := len(e.wsDials), e.wsUpstream
	e.mu.Unlock()
	if upstream == nil {
		http.NotFound(w, r)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	for {
		// Octopus 关掉它这一头时结束
		_, data, err := conn.Read(context.Background())
		if err != nil {
			return
		}
		var body map[string]json.RawMessage
		_ = json.Unmarshal(data, &body)
		e.mu.Lock()
		e.wsRequests = append(e.wsRequests, wsIngressWSRequest{dial: dial, body: body})
		n := len(e.requests) + len(e.wsRequests)
		e.mu.Unlock()
		if !upstream(n, r.Header, conn) {
			return
		}
	}
}

// upstreamWS 返回假上游的 WS 一侧收到的握手和请求。
func (e *wsIngressEnv) upstreamWS() ([]http.Header, []wsIngressWSRequest) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]http.Header(nil), e.wsDials...), append([]wsIngressWSRequest(nil), e.wsRequests...)
}

// relayLogs 等到有 n 条请求日志再返回，按时间先后排。
// 客户端读到结束事件时，这个回合的日志可能还没记下来，所以要等。
func (e *wsIngressEnv) relayLogs(n int) []model.RelayLog {
	e.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		logs, err := op.RelayLogList(e.ctx, nil, nil, nil, 1, 100)
		if err != nil {
			e.t.Fatalf("RelayLogList failed: %v", err)
		}
		if len(logs) >= n {
			slices.SortFunc(logs, func(a, b model.RelayLog) int { return cmp.Compare(a.ID, b.ID) })
			return logs
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("expected %d relay logs, got %d", n, len(logs))
		}
	}
}

// wsIngressWSFrames 是第 n 个回复在 WS 上的全部事件。
func wsIngressWSFrames(n int) [][]byte {
	return extractSSEDataLines([]byte(wsIngressSSE(n)))
}

func wsIngressWSSend(conn *websocket.Conn, frames ...[]byte) {
	for _, frame := range frames {
		_ = conn.Write(context.Background(), websocket.MessageText, frame)
	}
}

// wsIngressWSAnswers 是正常的 WS 上游：每个请求都完整回复。
func wsIngressWSAnswers(n int, _ http.Header, conn *websocket.Conn) bool {
	wsIngressWSSend(conn, wsIngressWSFrames(n)...)
	return true
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
	c.Set("api_key_id", e.apiKey.ID)
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

// 上游支持 WS：一条客户端连接只用一条上游连接。请求原样发过去，只改 model 和 WS 协议要求的字段；
// 第二轮在同一条连接上发原生续传，也就是客户端的原始请求。
func TestWSIngressUpstreamWSContinuesOnTheSameConnection(t *testing.T) {
	env := newWSIngressEnv(t, wsIngressStatelessUpstream)
	env.useUpstreamWS(wsIngressWSAnswers)
	override := `{"service_tier":"priority"}`
	if _, err := op.ChannelUpdate(&model.ChannelUpdateRequest{ID: env.channel.ID, ParamOverride: &override}, env.ctx); err != nil {
		t.Fatalf("ChannelUpdate failed: %v", err)
	}

	events, err := env.turn(10*time.Second, wsIngressCreate(`"background":true,"client_metadata":{"origin":"test"},`, wsIngressUserItem("first")))
	if err != nil || len(events) != 5 || events[4]["type"] != "response.completed" {
		t.Fatalf("first turn: expected the upstream's five events, got %v (err=%v)", events, err)
	}
	// 客户端看到的是它自己用的模型名，不是上游的
	if response, _ := events[4]["response"].(map[string]any); response["model"] != wsIngressModel {
		t.Fatalf("client sees model %v, want %s", response["model"], wsIngressModel)
	}
	env.mustComplete(wsIngressCreate(`"previous_response_id":"resp_1",`, wsIngressUserItem("second")))

	dials, requests := env.upstreamWS()
	if len(dials) != 1 || len(requests) != 2 || requests[1].dial != 1 {
		t.Fatalf("expected both turns on one upstream connection, got %d connections and %d requests", len(dials), len(requests))
	}
	if n := len(env.upstreamRequests()); n != 0 {
		t.Fatalf("expected no HTTP requests to the upstream, got %d", n)
	}
	for name, want := range map[string]string{
		"User-Agent":    "codex_cli_rs/0.0.0",
		"Originator":    "codex_cli_rs",
		"Session_id":    "sess_1",
		"Authorization": "Bearer upstream-key",
		"Openai-Beta":   "responses_websockets=2026-02-06",
	} {
		if got := dials[0].Get(name); got != want {
			t.Errorf("upstream handshake header %s = %q, want %q", name, got, want)
		}
	}
	for i, request := range requests {
		for field, want := range map[string]string{"type": `"response.create"`, "stream": "true", "model": `"gpt-4o"`, "service_tier": `"priority"`} {
			if got := string(request.body[field]); got != want {
				t.Errorf("request %d %s = %s, want %s", i+1, field, got, want)
			}
		}
		for _, field := range []string{"background", "generate"} {
			if value, ok := request.body[field]; ok {
				t.Errorf("request %d must not send %s upstream: %s", i+1, field, value)
			}
		}
	}
	wsIngressSameJSON(t, "first request client_metadata", requests[0].body["client_metadata"], `{"origin":"test"}`)
	if got := string(requests[1].body["previous_response_id"]); got != `"resp_1"` {
		t.Fatalf("second request previous_response_id = %s, want the response this upstream connection just completed", got)
	}
	if got := strings.Join(wsIngressInputTexts(t, requests[1].body), " | "); got != "message:second" {
		t.Fatalf("second request input = %q, want only the new input", got)
	}

	for i, wantMode := range []model.RelayLogWSMode{model.RelayLogWSModeFresh, model.RelayLogWSModeContinuation} {
		log := env.relayLogs(2)[i]
		if !log.Success || !log.UsedWS || log.WSMode == nil || *log.WSMode != wantMode ||
			log.WSExecMode == nil || *log.WSExecMode != model.RelayLogWSExecModePassthrough {
			t.Errorf("turn %d log: success=%t used_ws=%t ws_mode=%v ws_exec_mode=%v, want a successful %s passthrough turn",
				i+1, log.Success, log.UsedWS, log.WSMode, log.WSExecMode, wantMode)
		}
		if log.InputTokens != 1 || log.OutputTokens != 1 || log.ActualModelName != "gpt-4o" {
			t.Errorf("turn %d log: input=%d output=%d model=%q, want the upstream's usage and model",
				i+1, log.InputTokens, log.OutputTokens, log.ActualModelName)
		}
	}
}

// 上游不支持 WS（握手回 404）：同一次尝试里改走 HTTP，客户端看不出区别。之后不再每个请求都去试握手。
func TestWSIngressFallsBackToHTTPWhenUpstreamHasNoWS(t *testing.T) {
	env := newWSIngressEnv(t, wsIngressStatelessUpstream)

	firstID := env.mustComplete(wsIngressCreate("", wsIngressUserItem("first")))
	env.mustComplete(wsIngressCreate(fmt.Sprintf(`"previous_response_id":%q,`, firstID), wsIngressUserItem("second")))

	dials, wsRequests := env.upstreamWS()
	if len(dials) != 1 || len(wsRequests) != 0 {
		t.Fatalf("expected one rejected handshake and no ws requests, got %d handshakes and %d requests", len(dials), len(wsRequests))
	}
	requests := env.upstreamRequests()
	if len(requests) != 2 {
		t.Fatalf("expected one HTTP request per turn, got %d", len(requests))
	}
	if got, want := strings.Join(wsIngressInputTexts(t, requests[1]), " | "), "message:first | message:answer 1 | message:second"; got != want {
		t.Fatalf("second request input = %q, want %q", got, want)
	}
	for i, log := range env.relayLogs(2) {
		if !log.Success || log.UsedWS || len(log.Attempts) != 1 {
			t.Errorf("turn %d log: success=%t used_ws=%t attempts=%d, want one successful attempt over HTTP", i+1, log.Success, log.UsedWS, len(log.Attempts))
		}
		// 只有真的试过握手的那一轮才标“降级”
		if downgraded := log.WSRecovery != nil && *log.WSRecovery == model.RelayLogWSRecoveryDowngrade; downgraded != (i == 0) {
			t.Errorf("turn %d log: ws_recovery=%v", i+1, log.WSRecovery)
		}
	}
}

// 渠道的“上游 WebSocket”设为关闭：上游支持 WS 也不去握手，始终用 HTTP。
func TestWSIngressChannelWithUpstreamWSOffUsesHTTP(t *testing.T) {
	env := newWSIngressEnv(t, wsIngressStatelessUpstream)
	env.useUpstreamWS(wsIngressWSAnswers)
	off := model.ChannelWSModeOff
	if _, err := op.ChannelUpdate(&model.ChannelUpdateRequest{ID: env.channel.ID, WSMode: &off}, env.ctx); err != nil {
		t.Fatalf("ChannelUpdate failed: %v", err)
	}

	env.mustComplete(wsIngressCreate("", wsIngressUserItem("hello")))

	if dials, _ := env.upstreamWS(); len(dials) != 0 || len(env.upstreamRequests()) != 1 {
		t.Fatalf("expected one HTTP request and no ws handshake, got %d handshakes and %d HTTP requests", len(dials), len(env.upstreamRequests()))
	}
	if log := env.relayLogs(1)[0]; !log.Success || log.UsedWS || log.WSRecovery != nil {
		t.Fatalf("log: success=%t used_ws=%t ws_recovery=%v, want a plain HTTP turn", log.Success, log.UsedWS, log.WSRecovery)
	}
}

// 上游在回复中途关闭连接：不管关闭码是什么都算失败。客户端收到 error 事件，它的连接还能接着用。
func TestWSIngressUpstreamWSClosedMidReplyIsAFailure(t *testing.T) {
	env := newWSIngressEnv(t, wsIngressStatelessUpstream)
	env.useUpstreamWS(func(n int, _ http.Header, conn *websocket.Conn) bool {
		wsIngressWSSend(conn, wsIngressWSFrames(n)[:3]...)
		_ = conn.Close(websocket.StatusNormalClosure, "")
		return false
	})

	request := wsIngressCreate("", wsIngressUserItem("hello"))
	events, err := env.turn(5*time.Second, request)
	if err != nil {
		t.Fatalf("failed turn got no terminal event after %d events: %v", len(events), err)
	}
	last := events[len(events)-1]
	errorBody, _ := last["error"].(map[string]any)
	if len(events) != 4 || last["type"] != "error" || last["status"] != float64(http.StatusBadGateway) || errorBody["code"] != "upstream_stream_interrupted" {
		t.Fatalf("expected three partial events and a 502 upstream_stream_interrupted error, got %v", events)
	}
	if attempts := env.relayLogs(1)[0].Attempts; len(attempts) != 1 || attempts[0].Status != model.AttemptFailed {
		t.Fatalf("expected the attempt to be recorded as failed, got %+v", attempts)
	}

	// 这个渠道的 WS 先歇一会儿：重发的请求走 HTTP
	env.mustComplete(request)
	if dials, _ := env.upstreamWS(); len(dials) != 1 || len(env.upstreamRequests()) != 1 {
		t.Fatalf("expected the retry over HTTP, got %d ws handshakes and %d HTTP requests", len(dials), len(env.upstreamRequests()))
	}
}

// 上游 WS 的第一个事件就是错误：客户端还什么都没收到，所以不转发它，直接换下一个渠道。
func TestWSIngressUpstreamWSFirstEventErrorFailsOver(t *testing.T) {
	env := newWSIngressEnv(t, wsIngressStatelessUpstream)
	env.addChannel("second-key")
	env.useUpstreamWS(func(n int, handshake http.Header, conn *websocket.Conn) bool {
		if handshake.Get("Authorization") == "Bearer upstream-key" {
			wsIngressWSSend(conn, []byte(`{"type":"error","status":429,"error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"Rate limit reached"}}`))
			return true
		}
		return wsIngressWSAnswers(n, handshake, conn)
	})

	events, err := env.turn(5*time.Second, wsIngressCreate("", wsIngressUserItem("hello")))
	if err != nil || len(events) != 5 || events[4]["type"] != "response.completed" {
		t.Fatalf("expected only the second channel's reply, got %v (err=%v)", events, err)
	}
	dials, requests := env.upstreamWS()
	if len(dials) != 2 || dials[1].Get("Authorization") != "Bearer second-key" || len(requests) != 2 {
		t.Fatalf("expected one request per channel, got %d handshakes and %d requests", len(dials), len(requests))
	}
}

// 上游把空闲的连接关掉了：下一轮换一条新连接、用完整请求重发，客户端看不出来。
func TestWSIngressUpstreamWSReconnectsAfterIdleClose(t *testing.T) {
	env := newWSIngressEnv(t, wsIngressStatelessUpstream)
	env.useUpstreamWS(func(n int, handshake http.Header, conn *websocket.Conn) bool {
		wsIngressWSAnswers(n, handshake, conn)
		return n != 1 // 回完第一轮就关掉这条连接
	})

	firstID := env.mustComplete(wsIngressCreate("", wsIngressUserItem("first")))
	env.mustComplete(wsIngressCreate(fmt.Sprintf(`"previous_response_id":%q,`, firstID), wsIngressUserItem("second")))

	dials, requests := env.upstreamWS()
	if len(dials) != 2 || len(requests) != 2 || requests[1].dial != 2 {
		t.Fatalf("expected the second turn on a new upstream connection, got %d connections and %d requests", len(dials), len(requests))
	}
	if value, ok := requests[1].body["previous_response_id"]; ok {
		t.Fatalf("a new upstream connection cannot continue the old one, but previous_response_id %s was sent", value)
	}
	if got, want := strings.Join(wsIngressInputTexts(t, requests[1].body), " | "), "message:first | message:answer 1 | message:second"; got != want {
		t.Fatalf("second request input = %q, want %q", got, want)
	}
}

// HTTP 入站一律用 HTTP 连上游，带 previous_response_id 也一样。
// 本地没有存档的续传原样转发；成功之后也不建存档：历史在上游那里，本地只有这一轮，拿来回放会丢掉前面的对话。
func TestHTTPIngressContinuationIsForwardedOverHTTP(t *testing.T) {
	env := newWSIngressEnv(t, func(n int, _ []byte, w http.ResponseWriter, _ *http.Request) {
		// 模仿存着历史的上游（store=true）：认识所有 previous_response_id
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, wsIngressSSE(n))
	})
	env.useUpstreamWS(wsIngressWSAnswers)

	previousIDs := []string{"resp_stored", "resp_1"}
	for i, previousID := range previousIDs {
		body := wsIngressCreate(fmt.Sprintf(`"stream":true,"previous_response_id":%q,`, previousID), wsIngressUserItem(fmt.Sprintf("turn %d", i+1)))
		if recorder := env.post(body); recorder.Code != http.StatusOK {
			t.Fatalf("turn %d status = %d, body %s", i+1, recorder.Code, recorder.Body.String())
		}
	}

	if dials, _ := env.upstreamWS(); len(dials) != 0 {
		t.Fatalf("HTTP ingress must not dial the upstream over ws, got %d handshakes", len(dials))
	}
	requests := env.upstreamRequests()
	if len(requests) != len(previousIDs) {
		t.Fatalf("expected one HTTP request per turn, got %d", len(requests))
	}
	for i, previousID := range previousIDs {
		if got := string(requests[i]["previous_response_id"]); got != fmt.Sprintf("%q", previousID) {
			t.Fatalf("request %d previous_response_id = %s, want %q forwarded as is", i+1, got, previousID)
		}
		if got, want := strings.Join(wsIngressInputTexts(t, requests[i]), " | "), fmt.Sprintf("message:turn %d", i+1); got != want {
			t.Fatalf("request %d input = %q, want %q", i+1, got, want)
		}
	}
}

// wsIngressRejected 跑一个回合，要求只收到一条这样的 error 事件，并返回它。
func (e *wsIngressEnv) wsIngressRejected(payload string, status int, code string) map[string]any {
	e.t.Helper()
	events, err := e.turn(5*time.Second, payload)
	if err != nil {
		e.t.Fatalf("turn failed after %d events: %v", len(events), err)
	}
	last := events[len(events)-1]
	errorBody, _ := last["error"].(map[string]any)
	if len(events) != 1 || last["type"] != "error" || last["status"] != float64(status) || errorBody["code"] != code {
		e.t.Fatalf("expected only a %d %s error event, got %v", status, code, events)
	}
	return last
}

// 一条连接最长能开 60 分钟。Key 在这期间被停用、改了可用模型、被删掉，下一条消息就要生效，
// 和 HTTP 每个请求都检查一样。被拒绝的消息不发给上游，连接还能接着用。
func TestWSIngressChecksTheKeyOnEveryMessage(t *testing.T) {
	env := newWSIngressEnv(t, wsIngressStatelessUpstream)
	request := wsIngressCreate("", wsIngressUserItem("hello"))
	updateKey := func(change func(*model.APIKey)) {
		t.Helper()
		key := *env.apiKey
		change(&key)
		if err := op.APIKeyUpdate(&key, env.ctx); err != nil {
			t.Fatalf("APIKeyUpdate failed: %v", err)
		}
	}

	env.mustComplete(request)

	updateKey(func(key *model.APIKey) { key.Enabled = false })
	env.wsIngressRejected(request, http.StatusUnauthorized, "auth.api_key_disabled")
	// 预热在本地应答，但也要先过这一关
	env.wsIngressRejected(wsIngressCreate(`"generate":false,`, wsIngressUserItem("hello")), http.StatusUnauthorized, "auth.api_key_disabled")

	updateKey(func(key *model.APIKey) { key.ExpireAt = time.Now().Add(-time.Minute).Unix() })
	env.wsIngressRejected(request, http.StatusUnauthorized, "auth.api_key_expired")

	updateKey(func(key *model.APIKey) { key.SupportedModels = "another-model" })
	env.wsIngressRejected(request, http.StatusBadRequest, "invalid_request")

	// 费用上限：先留出余量，再把用量记到超过它
	used := op.StatsAPIKeyGet(env.apiKey.ID).StatsMetrics
	updateKey(func(key *model.APIKey) { key.MaxCost = used.InputCost + used.OutputCost + 1 })
	env.mustComplete(request)
	if err := op.StatsAPIKeyUpdate(env.apiKey.ID, model.StatsMetrics{InputCost: 2}); err != nil {
		t.Fatalf("StatsAPIKeyUpdate failed: %v", err)
	}
	env.wsIngressRejected(request, http.StatusUnauthorized, "auth.api_key_cost_exceeded")

	updateKey(func(*model.APIKey) {}) // 改回原样
	env.mustComplete(request)

	if err := op.APIKeyDelete(env.apiKey.ID, env.ctx); err != nil {
		t.Fatalf("APIKeyDelete failed: %v", err)
	}
	env.wsIngressRejected(request, http.StatusUnauthorized, "auth.invalid_token")

	if n := len(env.upstreamRequests()); n != 3 {
		t.Fatalf("expected only the three accepted turns to reach the upstream, got %d requests", n)
	}
}

// 每分钟次数：握手时算的那一次抵掉连接上的第一个请求，预热不发给上游所以不算。
// 这样 WS 和 HTTP 一样，一分钟正好能发 MaxRPM 个请求。
// 超了的那一条收到 429，事件里带 retry-after（Codex 像读 HTTP 响应头一样读它）。
func TestWSIngressCountsRequestsTowardsTheRateLimit(t *testing.T) {
	// 计数在每个自然分钟开始时清零：离整分钟太近就等一下，免得中途清零
	if second := time.Now().Second(); second >= 57 {
		time.Sleep(time.Duration(61-second) * time.Second)
	}
	env := newWSIngressEnv(t, wsIngressStatelessUpstream, func(key *model.APIKey) { key.MaxRPM = 2 })
	request := wsIngressCreate("", wsIngressUserItem("hello"))

	if events, err := env.turn(5*time.Second, wsIngressCreate(`"generate":false,`, wsIngressUserItem("hello"))); err != nil || events[len(events)-1]["type"] != "response.completed" {
		t.Fatalf("expected the prewarm to be answered, got %v (err=%v)", events, err)
	}
	env.mustComplete(request)
	env.mustComplete(request)
	rejected := env.wsIngressRejected(request, http.StatusTooManyRequests, "auth.api_key_rate_limited")

	if errorBody, _ := rejected["error"].(map[string]any); errorBody["type"] != "rate_limit_error" {
		t.Errorf("error type = %v, want rate_limit_error", errorBody["type"])
	}
	headers, _ := rejected["headers"].(map[string]any)
	retryAfter, _ := headers["retry-after"].(string)
	if seconds, err := strconv.Atoi(retryAfter); err != nil || seconds < 1 || seconds > 60 {
		t.Errorf("headers.retry-after = %q, want the seconds until the next minute", retryAfter)
	}
	if n := len(env.upstreamRequests()); n != 2 {
		t.Fatalf("expected exactly MaxRPM=2 requests to reach the upstream, got %d", n)
	}

	// 这时再握手也被拒绝。握手和 HTTP 请求走的是同一个中间件：429 加 Retry-After 响应头
	conn, response, err := websocket.Dial(context.Background(), env.url, &websocket.DialOptions{HTTPHeader: wsIngressClientHeaders})
	if err == nil {
		conn.CloseNow()
		t.Fatalf("expected a new handshake to be rejected while over the rate limit")
	}
	if response == nil || response.StatusCode != http.StatusTooManyRequests || response.Header.Get("Retry-After") == "" {
		t.Fatalf("expected the handshake to fail with 429 and a Retry-After header, got %v (err=%v)", response, err)
	}
}

// eventually 等到 cond 成立，超时就让测试失败。
func (e *wsIngressEnv) eventually(what string, cond func() bool) {
	e.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// upstreamRequestCount 是上游到现在收到的请求数，HTTP 和 WS 一起数。
func (e *wsIngressEnv) upstreamRequestCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.requests) + len(e.wsRequests)
}

// 回合进行中也要读客户端。不读的话：
//   - 客户端的 ping 得不到回应，会定时 ping 的客户端遇到长回合就自己断开了；
//   - Codex 中途发来的 response.interrupt 要等回合结束才读到，回它的那条 error 事件会被下一个回合当成自己的结果。
func TestWSIngressReadsTheClientDuringATurn(t *testing.T) {
	release := make(chan struct{})
	env := newWSIngressEnv(t, func(n int, body []byte, w http.ResponseWriter, r *http.Request) {
		if n == 1 {
			<-release // 第一个回合卡在这里，等测试放行
		}
		wsIngressStatelessUpstream(n, body, w, r)
	})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseOnce)

	request := wsIngressCreate("", wsIngressUserItem("hello"))
	type turnResult struct {
		events []map[string]any
		err    error
	}
	done := make(chan turnResult, 1)
	go func() {
		events, err := env.turn(10*time.Second, request)
		done <- turnResult{events, err}
	}()
	env.eventually("the request to reach the upstream", func() bool { return env.upstreamRequestCount() == 1 })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := env.client.Ping(ctx); err != nil {
		t.Errorf("no pong while the turn is in progress: %v", err)
	}
	interrupt := `{"type":"response.interrupt","response_id":"resp_1","mode":"discard_partial_items"}`
	if err := env.client.Write(context.Background(), websocket.MessageText, []byte(interrupt)); err != nil {
		t.Fatalf("sending response.interrupt failed: %v", err)
	}

	releaseOnce()
	result := <-done
	if result.err != nil || len(result.events) != 5 || result.events[4]["type"] != "response.completed" {
		t.Fatalf("expected the interrupted turn to finish with the upstream's five events, got %v (err=%v)", result.events, result.err)
	}
	// 打断没有换来 error 事件：下一个回合读到的第一条就是它自己的
	env.mustComplete(request)
}

// 客户端中途断开：马上取消发给上游的请求，不等到下一次给客户端写东西才发现。
func TestWSIngressClientDisconnectCancelsTheUpstreamRequest(t *testing.T) {
	for _, upstreamWS := range []bool{false, true} {
		t.Run(fmt.Sprintf("上游 WS=%t", upstreamWS), func(t *testing.T) {
			cancelled := make(chan struct{})
			markCancelled := sync.OnceFunc(func() { close(cancelled) })
			// 上游收到请求后什么都不回，所以 Octopus 没有机会靠“写客户端失败”发现它走了
			env := newWSIngressEnv(t, func(_ int, _ []byte, _ http.ResponseWriter, r *http.Request) {
				select {
				case <-r.Context().Done():
					markCancelled()
				case <-time.After(10 * time.Second):
				}
			})
			if upstreamWS {
				env.useUpstreamWS(func(_ int, _ http.Header, conn *websocket.Conn) bool {
					readCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					// Octopus 关掉这条上游连接时返回
					if _, _, err := conn.Read(readCtx); err != nil && readCtx.Err() == nil {
						markCancelled()
					}
					return false
				})
			}
			// 渠道的统计是全局缓存，可能带着前面的测试留下的数
			failedBefore := op.StatsChannelGet(env.channel.ID).RequestFailed

			if err := env.client.Write(context.Background(), websocket.MessageText, []byte(wsIngressCreate("", wsIngressUserItem("hello")))); err != nil {
				t.Fatalf("sending the request failed: %v", err)
			}
			env.eventually("the request to reach the upstream", func() bool { return env.upstreamRequestCount() == 1 })
			env.client.CloseNow()

			select {
			case <-cancelled:
			case <-time.After(3 * time.Second):
				t.Fatalf("the upstream request was not cancelled after the client went away")
			}
			// 是客户端自己走的：记一次失败的请求，但不算渠道的错
			log := env.relayLogs(1)[0]
			if log.Success || len(log.Attempts) != 1 || !strings.Contains(log.Error, context.Canceled.Error()) {
				t.Errorf("log: success=%t attempts=%d error=%q, want one cancelled attempt", log.Success, len(log.Attempts), log.Error)
			}
			if failed := op.StatsChannelGet(env.channel.ID).RequestFailed - failedBefore; failed != 0 {
				t.Errorf("the channel got %d failed requests for a client that went away, want 0", failed)
			}
			if upstreamWS && !wsUpstreamAvailable(env.channel.ID) {
				t.Errorf("the channel's upstream ws must not be paused for a client that went away")
			}
		})
	}
}
