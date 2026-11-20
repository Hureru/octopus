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
	engine.GET("/v1/responses", func(c *gin.Context) {
		c.Set("api_key_id", 77)
		HandleWSResponse(c)
	})
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	client, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", nil)
	if err != nil {
		t.Fatalf("ws dial failed: %v", err)
	}
	client.SetReadLimit(1 << 20)
	t.Cleanup(func() { client.CloseNow() })
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

func TestWSIngressUnknownPreviousResponseIDIsReported(t *testing.T) {
	env := newWSIngressEnv(t, wsIngressStatelessUpstream)

	events, err := env.turn(10*time.Second, wsIngressCreate(`"previous_response_id":"resp_unknown",`, wsIngressUserItem("hello")))
	if err != nil {
		t.Fatalf("turn failed after %d events: %v", len(events), err)
	}
	last := events[len(events)-1]
	errorBody, _ := last["error"].(map[string]any)
	if last["type"] != "error" || errorBody["code"] != "previous_response_not_found" {
		t.Fatalf("expected previous_response_not_found error event, got %v", last)
	}
	if requests := env.upstreamRequests(); len(requests) != 1 {
		t.Fatalf("expected the request to be forwarded once, got %d requests", len(requests))
	}
}

// wsIngressChatUpstream 是 Chat Completions 上游：第 n 个回复的内容是 "answer n"。
func wsIngressChatUpstream(n int, _ []byte, w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-%d\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"answer %d\"},\"finish_reason\":null}]}\n\n", n, n)
	fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-%d\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n", n)
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// 非 Responses 渠道从 Messages 生成请求：续传回合也要带上完整对话，WS 和 HTTP 入站都一样。
func TestContinuationOnChatChannelCarriesConversation(t *testing.T) {
	for _, ingress := range []string{"ws", "http"} {
		t.Run(ingress, func(t *testing.T) {
			env := newWSIngressEnv(t, wsIngressChatUpstream)
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
				} else if recorder := env.post(payload); recorder.Code != http.StatusOK {
					t.Fatalf("turn %d status = %d, body %s", n+1, recorder.Code, recorder.Body.String())
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
