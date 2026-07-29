package relay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func TestHandlerPassthroughsOpenAIChatSameProtocol(t *testing.T) {
	ctx := setupRelayTestDB(t)
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","model":"chat-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}],"custom_response":{"keep":true}}`))
	}))
	defer server.Close()

	channel := createRelayProtocolChannel(t, ctx, "chat-passthrough", outbound.OutboundTypeOpenAIChat, server.URL+"/v1")
	group := createRelayProtocolGroup(t, ctx, "chat-passthrough-group", channel.ID, "chat-model")

	requestBody := `{"model":"chat-passthrough-group","messages":[{"role":"user","content":"hello"}],"custom_request":{"keep":true}}`
	recorder := httptest.NewRecorder()
	c, _ := newRelayTestContext(recorder, "/v1/chat/completions", requestBody)
	Handler(inbound.InboundTypeOpenAIChat, c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected chat passthrough success, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var upstream map[string]any
	if err := json.Unmarshal(capturedBody, &upstream); err != nil {
		t.Fatalf("unmarshal upstream request: %v", err)
	}
	if upstream["model"] != "chat-model" {
		t.Fatalf("expected upstream model rewrite, got %#v", upstream["model"])
	}
	custom, ok := upstream["custom_request"].(map[string]any)
	if !ok || custom["keep"] != true {
		t.Fatalf("same-protocol custom field was not preserved: %#v", upstream["custom_request"])
	}
	if !strings.Contains(recorder.Body.String(), `"custom_response":{"keep":true}`) {
		t.Fatalf("same-protocol response was not preserved: %s", recorder.Body.String())
	}
	if group.ID == 0 {
		t.Fatal("group was not created")
	}
}

func TestHandlerAllowLossyRecordsProtocolWarnings(t *testing.T) {
	ctx := setupRelayTestDB(t)
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","model":"lossy-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	channel := createRelayProtocolChannel(t, ctx, "lossy-chat", outbound.OutboundTypeOpenAIChat, server.URL+"/v1")
	allowLossy := true
	if _, err := op.ChannelUpdate(&model.ChannelUpdateRequest{
		ID:         channel.ID,
		AllowLossy: &allowLossy,
	}, ctx); err != nil {
		t.Fatalf("enable allow-lossy: %v", err)
	}
	createRelayProtocolGroup(t, ctx, "lossy-chat-group", channel.ID, "lossy-model")

	recorder := httptest.NewRecorder()
	c, _ := newRelayTestContext(
		recorder,
		"/v1/responses",
		`{"model":"lossy-chat-group","input":"hello","tools":[{"type":"apply_patch"}]}`,
	)
	Handler(inbound.InboundTypeOpenAIResponse, c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected explicit allow-lossy route to succeed, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if warning := recorder.Header().Get("X-Octopus-Warning"); !strings.Contains(warning, "built_in_tools") {
		t.Fatalf("expected lossy warning header, got %q", warning)
	}
	var upstream map[string]any
	if err := json.Unmarshal(capturedBody, &upstream); err != nil {
		t.Fatalf("unmarshal upstream body: %v", err)
	}
	if _, exists := upstream["tools"]; exists {
		t.Fatalf("unsupported native tool should be explicitly dropped on lossy transform: %#v", upstream["tools"])
	}

	logs, err := op.RelayLogList(ctx, nil, nil, nil, 1, 10)
	if err != nil {
		t.Fatalf("RelayLogList: %v", err)
	}
	if len(logs) == 0 || !logs[0].ProtocolAllowLossy || len(logs[0].ProtocolWarnings) == 0 {
		t.Fatalf("relay log did not retain protocol lossy evidence: %#v", logs)
	}
}

func TestSetProtocolWarningHeaderClearsPreviousCandidateWarning(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	setProtocolWarningHeader(c, []string{"lossy route"})
	if got := recorder.Header().Get("X-Octopus-Warning"); got != "lossy route" {
		t.Fatalf("warning header = %q", got)
	}
	setProtocolWarningHeader(c, nil)
	if got := recorder.Header().Get("X-Octopus-Warning"); got != "" {
		t.Fatalf("warning header leaked across candidates: %q", got)
	}
}

func TestCandidateHeaderPolicyAppliesToSpecialRelayTransports(t *testing.T) {
	ctx := setupRelayTestDB(t)
	const (
		canonicalModelID = 606
		routeCandidateID = 707
	)
	if _, err := op.HeaderPolicyUpsert(ctx, model.HeaderPolicy{
		Scope:   model.HeaderPolicyScopeCanonicalModel,
		ScopeID: canonicalModelID,
		Enabled: true,
		SetHeaders: []model.CustomHeader{{
			HeaderKey:   "X-Route-Scope",
			HeaderValue: "canonical",
		}},
	}); err != nil {
		t.Fatalf("create canonical header policy: %v", err)
	}
	if _, err := op.HeaderPolicyUpsert(ctx, model.HeaderPolicy{
		Scope:   model.HeaderPolicyScopeRouteCandidate,
		ScopeID: routeCandidateID,
		Enabled: true,
		SetHeaders: []model.CustomHeader{{
			HeaderKey:   "X-Route-Scope",
			HeaderValue: "candidate",
		}},
	}); err != nil {
		t.Fatalf("create candidate header policy: %v", err)
	}

	channel := &model.Channel{ID: 808}
	compactHeaders := http.Header{}
	compactPolicy := copyProxyHeaders(
		ctx,
		nil,
		channel,
		compactHeaders,
		canonicalModelID,
		routeCandidateID,
	)
	assertSpecialRouteHeaderPolicy(t, "compact", compactHeaders, compactPolicy)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	imageRequest := httptest.NewRequest(http.MethodPost, "https://upstream.example/images/generations", nil)
	imagePolicy := copyHeadersToUpstream(
		imageRequest,
		c,
		channel,
		"secret",
		"application/json",
		false,
		canonicalModelID,
		routeCandidateID,
	)
	assertSpecialRouteHeaderPolicy(t, "images", imageRequest.Header, imagePolicy)

	wsHeaders, wsPolicy := buildUpstreamWSHeadersForRoute(
		ctx,
		nil,
		channel,
		"secret",
		canonicalModelID,
		routeCandidateID,
	)
	assertSpecialRouteHeaderPolicy(t, "websocket", wsHeaders, wsPolicy)

	unscopedWSHeaders := buildUpstreamWSHeaders(nil, channel, "secret")
	if unscopedWSHeaders.Get("X-Route-Scope") != "" {
		t.Fatalf("unscoped WebSocket headers inherited route policy: %#v", unscopedWSHeaders)
	}
	if wsHeaderSignature(wsHeaders) == wsHeaderSignature(unscopedWSHeaders) {
		t.Fatal("route-scoped WebSocket headers did not isolate the connection pool")
	}
}

func assertSpecialRouteHeaderPolicy(
	t *testing.T,
	transport string,
	headers http.Header,
	policy model.ResolvedHeaderPolicy,
) {
	t.Helper()
	if got := headers.Get("X-Route-Scope"); got != "candidate" {
		t.Fatalf("%s route header = %q, want candidate", transport, got)
	}
	if len(policy.Trace) != 2 ||
		policy.Trace[0].Scope != model.HeaderPolicyScopeCanonicalModel ||
		policy.Trace[1].Scope != model.HeaderPolicyScopeRouteCandidate {
		t.Fatalf("%s policy trace lost canonical/candidate layers: %+v", transport, policy.Trace)
	}
}

func TestHandlerRecordsProtocolTransformFailureStageWithoutUpstreamAttribution(t *testing.T) {
	ctx := setupRelayTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer server.Close()

	channel := createRelayProtocolChannel(t, ctx, "transform-failure", outbound.OutboundTypeOpenAIChat, server.URL+"/v1")
	createRelayProtocolGroup(t, ctx, "transform-failure-group", channel.ID, "transform-model")

	recorder := httptest.NewRecorder()
	c, _ := newRelayTestContext(
		recorder,
		"/v1/messages",
		`{"model":"transform-failure-group","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`,
	)
	Handler(inbound.InboundTypeAnthropic, c)

	logs, err := op.RelayLogList(ctx, nil, nil, nil, 1, 10)
	if err != nil {
		t.Fatalf("RelayLogList: %v", err)
	}
	if len(logs) == 0 {
		t.Fatal("expected relay log for transform failure")
	}
	entry := logs[0]
	if entry.ProtocolFailureStage != model.ProtocolFailureStageOutboundResponseTransform {
		t.Fatalf("unexpected protocol failure stage: %q", entry.ProtocolFailureStage)
	}
	if len(entry.Attempts) != 1 || entry.Attempts[0].Attribution != model.AttemptAttributionRelay {
		t.Fatalf("transform failure should be relay-attributed: %#v", entry.Attempts)
	}
}

func createRelayProtocolChannel(
	t *testing.T,
	ctx context.Context,
	name string,
	channelType outbound.OutboundType,
	baseURL string,
) *model.Channel {
	t.Helper()
	channel := &model.Channel{
		Name:     name,
		Type:     channelType,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: baseURL}},
		Model:    name + "-model",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate: %v", err)
	}
	return channel
}

func createRelayProtocolGroup(
	t *testing.T,
	ctx context.Context,
	name string,
	channelID int,
	upstreamModel string,
) *model.Group {
	t.Helper()
	group := &model.Group{Name: name, Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{
		GroupID:   group.ID,
		ChannelID: channelID,
		ModelName: upstreamModel,
		Priority:  1,
		Weight:    1,
	}, ctx); err != nil {
		t.Fatalf("GroupItemAdd: %v", err)
	}
	return group
}

func newRelayTestContext(
	recorder *httptest.ResponseRecorder,
	path string,
	body string,
) (*gin.Context, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	contextValue, engine := gin.CreateTestContext(recorder)
	contextValue.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	contextValue.Request.Header.Set("Content-Type", "application/json")
	return contextValue, engine
}
