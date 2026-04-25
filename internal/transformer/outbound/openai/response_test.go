package openai

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// TestConvertToResponsesRequestForwardsVerbosity verifies O-M8: the gpt-5
// verbosity knob on the internal request lands on
// ResponsesRequest.Text.Verbosity regardless of whether ResponseFormat is
// set, and is omitted when the caller leaves it unset or blank.
func TestConvertToResponsesRequestForwardsVerbosity(t *testing.T) {
	v := "high"
	req := &model.InternalLLMRequest{
		Model:     "gpt-5",
		Verbosity: &v,
	}
	out := ConvertToResponsesRequest(req)
	if out.Text == nil || out.Text.Verbosity == nil || *out.Text.Verbosity != "high" {
		t.Fatalf("expected text.verbosity=high, got %+v", out.Text)
	}

	// Verbosity coexists with response_format (schema + verbosity are
	// independent siblings on Responses text options).
	req.ResponseFormat = &model.ResponseFormat{Type: "text"}
	out = ConvertToResponsesRequest(req)
	if out.Text == nil || out.Text.Format == nil || out.Text.Format.Type != "text" {
		t.Errorf("expected format preserved alongside verbosity, got %+v", out.Text)
	}
	if out.Text.Verbosity == nil || *out.Text.Verbosity != "high" {
		t.Errorf("expected verbosity preserved alongside format, got %+v", out.Text)
	}

	// Blank / nil verbosity omits the field.
	blank := "   "
	req.Verbosity = &blank
	req.ResponseFormat = nil
	out = ConvertToResponsesRequest(req)
	if out.Text != nil && out.Text.Verbosity != nil {
		t.Errorf("expected verbosity dropped for blank value, got %+v", out.Text)
	}
}

func TestConvertToResponsesRequestPreservesRawInputItems(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model:         "gpt-4o",
		RawInputItems: json.RawMessage(`[{"type":"input_text","text":"raw","native_meta":{"keep":true}}]`),
		Messages: []model.Message{{
			Role: "user",
			Content: model.MessageContent{
				Content: stringPtr("normalized"),
			},
		}},
	}

	responsesReq := ConvertToResponsesRequest(req)
	body, err := json.Marshal(responsesReq)
	if err != nil {
		t.Fatalf("marshal responses request failed: %v", err)
	}

	var payload struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal responses request failed: %v", err)
	}
	if len(payload.Input) != 1 {
		t.Fatalf("expected raw input item to be preserved, got %#v", payload.Input)
	}
	if payload.Input[0]["text"] != "raw" {
		t.Fatalf("expected raw input text to be preserved, got %#v", payload.Input[0])
	}
	if _, ok := payload.Input[0]["native_meta"]; !ok {
		t.Fatalf("expected native field to be preserved, got %#v", payload.Input[0])
	}
	if payload.Input[0]["text"] == "normalized" {
		t.Fatalf("expected raw input items to take precedence over normalized messages")
	}
}

func TestConvertToResponsesRequestSanitizesRawReasoningInputSummary(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "gpt-4o",
		RawInputItems: json.RawMessage(`[
			{"type":"input_text","text":"hello"},
			{"type":"reasoning","encrypted_content":"enc","native_meta":{"keep":true}}
		]`),
	}

	responsesReq := ConvertToResponsesRequest(req)
	body, err := json.Marshal(responsesReq)
	if err != nil {
		t.Fatalf("marshal responses request failed: %v", err)
	}

	var payload struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal responses request failed: %v", err)
	}

	if len(payload.Input) != 2 {
		t.Fatalf("expected two raw input items, got %#v", payload.Input)
	}

	reasoning := payload.Input[1]
	summary, ok := reasoning["summary"].([]any)
	if !ok || len(summary) != 1 {
		t.Fatalf("expected reasoning summary to be added, got %#v", reasoning["summary"])
	}
	part, ok := summary[0].(map[string]any)
	if !ok || part["type"] != "summary_text" || part["text"] != "" {
		t.Fatalf("expected default summary_text part, got %#v", summary[0])
	}
	if _, ok := reasoning["native_meta"]; !ok {
		t.Fatalf("expected native fields to be preserved, got %#v", reasoning)
	}
	if reasoning["encrypted_content"] != "enc" {
		t.Fatalf("expected encrypted_content to be preserved, got %#v", reasoning["encrypted_content"])
	}
}

func TestMarshalResponsesInputItemsBuildsArrayInput(t *testing.T) {
	data, err := MarshalResponsesInputItems([]model.Message{{
		Role: "assistant",
		ToolCalls: []model.ToolCall{{
			ID:   "call_123",
			Type: "function",
			Function: model.FunctionCall{
				Name:      "lookup",
				Arguments: `{}`,
			},
		}},
	}})
	if err != nil {
		t.Fatalf("marshal responses input items failed: %v", err)
	}

	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("unmarshal marshaled items failed: %v", err)
	}
	if len(items) != 1 || items[0]["type"] != "function_call" {
		t.Fatalf("expected assistant tool call to become function_call item, got %#v", items)
	}
}

func TestConvertToResponsesRequestOmitsDeprecatedUser(t *testing.T) {
	user := "legacy-user"
	req := &model.InternalLLMRequest{
		Model:    "gpt-4o",
		User:     &user,
		Metadata: map[string]string{"trace_id": "abc123"},
		Messages: []model.Message{{
			Role: "user",
			Content: model.MessageContent{
				Content: stringPtr("hello"),
			},
		}},
	}

	responsesReq := ConvertToResponsesRequest(req)
	body, err := json.Marshal(responsesReq)
	if err != nil {
		t.Fatalf("marshal responses request failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal responses request failed: %v", err)
	}

	if _, ok := payload["user"]; ok {
		t.Fatalf("expected deprecated user to be omitted, got %#v", payload["user"])
	}
	if _, ok := payload["metadata"]; !ok {
		t.Fatalf("expected metadata to remain available, got %#v", payload)
	}
}

func TestConvertToResponsesRequestDerivesPromptCacheKeyFromAnthropicCacheControl(t *testing.T) {
	system := "You are helpful."
	user := "hello"
	req := &model.InternalLLMRequest{
		Model:        "gpt-5.4",
		RawAPIFormat: model.APIFormatAnthropicMessage,
		Messages: []model.Message{
			{
				Role:         "system",
				Content:      model.MessageContent{Content: &system},
				CacheControl: &model.CacheControl{Type: model.CacheControlTypeEphemeral, TTL: model.CacheTTL5m},
			},
			{
				Role:    "user",
				Content: model.MessageContent{Content: &user},
			},
		},
	}

	out := ConvertToResponsesRequest(req)
	if out.PromptCacheKey == nil || *out.PromptCacheKey == "" {
		t.Fatalf("expected derived prompt_cache_key, got %+v", out.PromptCacheKey)
	}
	if out.PromptCacheRetention != nil {
		t.Fatalf("expected no retention for 5m cache control, got %+v", out.PromptCacheRetention)
	}

	out2 := ConvertToResponsesRequest(req)
	if out2.PromptCacheKey == nil || *out2.PromptCacheKey != *out.PromptCacheKey {
		t.Fatalf("expected deterministic prompt_cache_key, got %v and %v", out.PromptCacheKey, out2.PromptCacheKey)
	}
}

func TestConvertToResponsesRequestUsesStableAnthropicCachePrefix(t *testing.T) {
	base := anthropicCacheRequest("latest question")
	changedLatest := anthropicCacheRequest("different latest question")
	changedSystem := anthropicCacheRequest("latest question")
	changedSystem.Messages[0].Content.Content = stringPtr("Different instructions")

	baseOut := ConvertToResponsesRequest(base)
	changedLatestOut := ConvertToResponsesRequest(changedLatest)
	changedSystemOut := ConvertToResponsesRequest(changedSystem)
	if baseOut.PromptCacheKey == nil || changedLatestOut.PromptCacheKey == nil {
		t.Fatalf("expected cache keys, got %+v and %+v", baseOut.PromptCacheKey, changedLatestOut.PromptCacheKey)
	}
	if *baseOut.PromptCacheKey != *changedLatestOut.PromptCacheKey {
		t.Fatalf("expected latest user changes to keep stable cache key, got %q and %q", *baseOut.PromptCacheKey, *changedLatestOut.PromptCacheKey)
	}
	if changedSystemOut.PromptCacheKey == nil || *changedSystemOut.PromptCacheKey == *baseOut.PromptCacheKey {
		t.Fatalf("expected system change to alter cache key, got %v and %v", baseOut.PromptCacheKey, changedSystemOut.PromptCacheKey)
	}
}

func TestConvertToResponsesRequestCacheKeyIgnoresToolCallIDs(t *testing.T) {
	first := anthropicCacheRequest("latest question")
	second := anthropicCacheRequest("latest question")
	first.Messages[2].ToolCalls = []model.ToolCall{{ID: "call_a", Type: "function", Function: model.FunctionCall{Name: "lookup", Arguments: `{"q":"octopus"}`}}}
	second.Messages[2].ToolCalls = []model.ToolCall{{ID: "call_b", Type: "function", Function: model.FunctionCall{Name: "lookup", Arguments: `{"q":"octopus"}`}}}

	firstOut := ConvertToResponsesRequest(first)
	secondOut := ConvertToResponsesRequest(second)
	if firstOut.PromptCacheKey == nil || secondOut.PromptCacheKey == nil || *firstOut.PromptCacheKey != *secondOut.PromptCacheKey {
		t.Fatalf("expected tool call IDs to be ignored, got %v and %v", firstOut.PromptCacheKey, secondOut.PromptCacheKey)
	}
}

func TestConvertToResponsesRequestRetentionUsesSelectedStableMarker(t *testing.T) {
	stableLong := anthropicCacheRequest("latest question")
	stableLong.Tools[0].CacheControl = &model.CacheControl{Type: model.CacheControlTypeEphemeral, TTL: model.CacheTTL1h}
	stableLongOut := ConvertToResponsesRequest(stableLong)
	if stableLongOut.PromptCacheRetention == nil || *stableLongOut.PromptCacheRetention != anthropicPromptCacheRetention24h {
		t.Fatalf("expected stable 1h marker to set retention, got %+v", stableLongOut.PromptCacheRetention)
	}

	unstableLong := anthropicCacheRequest("latest question")
	unstableLong.Tools[0].CacheControl = nil
	unstableLong.Messages[len(unstableLong.Messages)-1].CacheControl = &model.CacheControl{Type: model.CacheControlTypeEphemeral, TTL: model.CacheTTL1h}
	unstableLongOut := ConvertToResponsesRequest(unstableLong)
	if unstableLongOut.PromptCacheRetention != nil {
		t.Fatalf("expected excluded final 1h marker not to set retention, got %+v", unstableLongOut.PromptCacheRetention)
	}
}

func TestConvertToResponsesRequestDerivesRetentionFromAnthropicCacheControlTTL1h(t *testing.T) {
	toolName := "lookup"
	req := &model.InternalLLMRequest{
		Model: "gpt-5.4",
		Tools: []model.Tool{{
			Type: "function",
			Function: model.Function{
				Name:       toolName,
				Parameters: json.RawMessage(`{"type":"object"}`),
			},
			CacheControl: &model.CacheControl{Type: model.CacheControlTypeEphemeral, TTL: model.CacheTTL1h},
		}},
	}

	out := ConvertToResponsesRequest(req)
	if out.PromptCacheKey == nil || *out.PromptCacheKey == "" {
		t.Fatalf("expected derived prompt_cache_key, got %+v", out.PromptCacheKey)
	}
	if out.PromptCacheRetention == nil || *out.PromptCacheRetention != anthropicPromptCacheRetention24h {
		t.Fatalf("expected retention %q, got %+v", anthropicPromptCacheRetention24h, out.PromptCacheRetention)
	}
}

func TestConvertToResponsesRequestPreservesExplicitResponsesCacheFields(t *testing.T) {
	key := "client-key"
	retention := "12h"
	req := &model.InternalLLMRequest{
		Model:                   "gpt-5.4",
		ResponsesPromptCacheKey: &key,
		PromptCacheRetention:    &retention,
		Messages: []model.Message{{
			Role:         "system",
			Content:      model.MessageContent{Content: stringPtr("ignored")},
			CacheControl: &model.CacheControl{Type: model.CacheControlTypeEphemeral, TTL: model.CacheTTL1h},
		}},
	}

	out := ConvertToResponsesRequest(req)
	if out.PromptCacheKey == nil || *out.PromptCacheKey != key {
		t.Fatalf("expected explicit prompt_cache_key preserved, got %+v", out.PromptCacheKey)
	}
	if out.PromptCacheRetention == nil || *out.PromptCacheRetention != retention {
		t.Fatalf("expected explicit retention preserved, got %+v", out.PromptCacheRetention)
	}
}

func anthropicCacheRequest(latestUser string) *model.InternalLLMRequest {
	system := "You are helpful."
	previousUser := "Summarize this repository."
	assistant := "I can help with that."
	return &model.InternalLLMRequest{
		Model:        "gpt-5.4",
		RawAPIFormat: model.APIFormatAnthropicMessage,
		Messages: []model.Message{
			{
				Role:         "system",
				Content:      model.MessageContent{Content: &system},
				CacheControl: &model.CacheControl{Type: model.CacheControlTypeEphemeral, TTL: model.CacheTTL5m},
			},
			{
				Role:         "user",
				Content:      model.MessageContent{Content: &previousUser},
				CacheControl: &model.CacheControl{Type: model.CacheControlTypeEphemeral, TTL: model.CacheTTL5m},
			},
			{
				Role:    "assistant",
				Content: model.MessageContent{Content: &assistant},
			},
			{
				Role:    "user",
				Content: model.MessageContent{Content: &latestUser},
			},
		},
		Tools: []model.Tool{{
			Type: "function",
			Function: model.Function{
				Name:        "lookup",
				Description: "Lookup repository information",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
			},
			CacheControl: &model.CacheControl{Type: model.CacheControlTypeEphemeral, TTL: model.CacheTTL5m},
		}},
	}
}

func TestTransformStreamAggregatesFunctionCallIDAcrossEvents(t *testing.T) {
	outbound := &ResponseOutbound{}

	first, err := outbound.TransformStream(nil, []byte(`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_123","name":"lookup"}}`))
	if err != nil {
		t.Fatalf("first function call stream transform failed: %v", err)
	}
	if first == nil || len(first.Choices) != 1 || first.Choices[0].Delta == nil {
		t.Fatalf("expected initial function call delta, got %#v", first)
	}
	if got := first.Choices[0].Delta.ToolCalls[0].ID; got != "call_123" {
		t.Fatalf("expected initial function call id to be preserved, got %q", got)
	}

	second, err := outbound.TransformStream(nil, []byte(`{"type":"response.function_call_arguments.delta","output_index":0,"call_id":"call_123","name":"lookup","delta":"{}"}`))
	if err != nil {
		t.Fatalf("second function call stream transform failed: %v", err)
	}
	if second == nil || len(second.Choices) != 1 || second.Choices[0].Delta == nil {
		t.Fatalf("expected function call arguments delta, got %#v", second)
	}
	toolCall := second.Choices[0].Delta.ToolCalls[0]
	if toolCall.ID != "call_123" {
		t.Fatalf("expected function call id to survive argument delta, got %q", toolCall.ID)
	}
	if toolCall.Function.Arguments != "{}" {
		t.Fatalf("expected function call arguments delta to be preserved, got %q", toolCall.Function.Arguments)
	}

	completed, err := outbound.TransformStream(nil, []byte(`{"type":"response.completed","response":{"id":"resp_123","model":"gpt-4o","status":"completed"}}`))
	if err != nil {
		t.Fatalf("completed stream transform failed: %v", err)
	}
	if completed == nil || len(completed.RawResponsesOutputItems) == 0 {
		t.Fatalf("expected completed stream response to preserve exact output items, got %#v", completed)
	}
}

func TestConvertToLLMResponseFromResponsesPreservesRawOutputItems(t *testing.T) {
	resp := &ResponsesResponse{
		ID:        "resp_123",
		Object:    "response",
		Model:     "gpt-4o",
		CreatedAt: 1,
		Output: []ResponsesItem{{
			Type:      "function_call",
			CallID:    "call_123",
			Name:      "lookup",
			Arguments: `{}`,
		}},
	}

	internalResp := convertToLLMResponseFromResponses(resp)
	if len(internalResp.RawResponsesOutputItems) == 0 {
		t.Fatalf("expected raw responses output items to be preserved")
	}
	var items []map[string]any
	if err := json.Unmarshal(internalResp.RawResponsesOutputItems, &items); err != nil {
		t.Fatalf("unmarshal raw output items failed: %v", err)
	}
	if len(items) != 1 || items[0]["type"] != "function_call" {
		t.Fatalf("expected original output items to be kept, got %#v", items)
	}
}

func TestConvertToResponsesRequestPreservesImageGenerationTools(t *testing.T) {
	content := "hello"
	req := &model.InternalLLMRequest{
		Model: "gpt-4o",
		Messages: []model.Message{{
			Role:    "user",
			Content: model.MessageContent{Content: &content},
		}},
		Tools: []model.Tool{{
			Type: "image_generation",
			ImageGeneration: &model.ImageGeneration{
				Background: "transparent",
				Size:       "1024x1024",
			},
		}},
	}

	wire := ConvertToResponsesRequest(req)
	body, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected image generation tool in responses payload, got %#v", payload["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok || tool["type"] != "image_generation" {
		t.Fatalf("expected image_generation tool in responses payload, got %#v", tools[0])
	}
	if got := tool["background"]; got != "transparent" {
		t.Fatalf("expected background to be preserved, got %#v", got)
	}
}

func TestTransformRequestRawRewritesModel(t *testing.T) {
	outbound := &ResponseOutbound{}
	req, err := outbound.TransformRequestRaw(
		context.Background(),
		[]byte(`{"model":"old-model","tools":[{"type":"apply_patch"}],"input":"hello"}`),
		"new-model",
		"https://example.com/v1",
		"test-key",
		nil,
	)
	if err != nil {
		t.Fatalf("TransformRequestRaw() error = %v", err)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read request body failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal request body failed: %v", err)
	}
	if payload["model"] != "new-model" {
		t.Fatalf("expected model to be rewritten, got %#v", payload["model"])
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected raw tools to stay intact, got %#v", payload["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok || tool["type"] != "apply_patch" {
		t.Fatalf("expected raw apply_patch tool to be preserved, got %#v", tools[0])
	}
}

func stringPtr(value string) *string {
	return &value
}

// TestNormalizeResponsesFinishReason verifies that Responses API status
// values are mapped to the OpenAI Chat Completions finish_reason enum
// (stop | length | tool_calls | content_filter | function_call). Historic
// behaviour emitted "error" which strict SDKs such as OpenAI Python reject
// with a Literal validation error. (O-C1)
func TestNormalizeResponsesFinishReason(t *testing.T) {
	cases := []struct {
		name       string
		status     string
		err        *ResponsesError
		wantReason string
		wantErrMsg string
	}{
		{name: "completed", status: "completed", wantReason: "stop"},
		{name: "incomplete", status: "incomplete", wantReason: "length"},
		{name: "failed carries error cause", status: "failed", err: &ResponsesError{Code: 400, Message: "boom"}, wantReason: "stop", wantErrMsg: "boom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status := tc.status
			reason, respErr := normalizeResponsesFinishReason(&status, tc.err)
			if reason == nil || *reason != tc.wantReason {
				t.Fatalf("want reason=%q, got %v", tc.wantReason, reason)
			}
			if tc.wantErrMsg != "" {
				if respErr == nil || respErr.Detail.Message != tc.wantErrMsg {
					t.Fatalf("want error %q, got %+v", tc.wantErrMsg, respErr)
				}
			}
			// Must never produce the legacy illegal "error" value.
			if reason != nil && *reason == "error" {
				t.Fatalf("illegal finish_reason=error leaked, status=%s", tc.status)
			}
		})
	}
}

// TestTransformStreamFailedEventPreservesLegalFinishReason covers the full
// stream path for a `response.failed` event carrying an embedded error
// payload. The event must translate to finish_reason=stop and an attached
// ResponseError so the inbound layer can forward the original cause. Any
// leak of finish_reason=error means a downstream client will throw out the
// whole turn.
func TestTransformStreamFailedEventPreservesLegalFinishReason(t *testing.T) {
	o := &ResponseOutbound{}
	event := `{"type":"response.failed","response":{"status":"failed","error":{"code":500,"message":"upstream oops"}}}`
	resp, err := o.TransformStream(context.Background(), []byte(event))
	if err != nil {
		t.Fatalf("TransformStream: %v", err)
	}
	if resp == nil || len(resp.Choices) != 1 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Choices[0].FinishReason == nil || *resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("want finish_reason=stop, got %+v", resp.Choices[0].FinishReason)
	}
	if resp.Error == nil || resp.Error.Detail.Message != "upstream oops" {
		t.Fatalf("expected ResponseError attached, got %+v", resp.Error)
	}
}

// TestTransformStreamIncompleteEventMapsToLength guards the incomplete
// status mapping specifically — under the bug this also became "error".
func TestTransformStreamIncompleteEventMapsToLength(t *testing.T) {
	o := &ResponseOutbound{}
	event := `{"type":"response.incomplete"}`
	resp, err := o.TransformStream(context.Background(), []byte(event))
	if err != nil {
		t.Fatalf("TransformStream: %v", err)
	}
	if resp == nil || len(resp.Choices) != 1 || resp.Choices[0].FinishReason == nil {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if *resp.Choices[0].FinishReason != "length" {
		t.Fatalf("want length, got %q", *resp.Choices[0].FinishReason)
	}
}

// TestTransformStreamCompletedWithFunctionCallMapsToToolCalls verifies the
// override introduced for O-C2: when a `response.completed` event carries
// a function_call item in its output, the Chat Completions finish_reason
// must be tool_calls, not stop. Under the bug the non-streaming path
// already did the right thing but the streaming path reported stop, so
// Agent SDKs silently skipped the tool invocation.
func TestTransformStreamCompletedWithFunctionCallMapsToToolCalls(t *testing.T) {
	o := &ResponseOutbound{}
	event := `{
		"type":"response.completed",
		"response":{
			"status":"completed",
			"output":[
				{"type":"function_call","call_id":"call_1","name":"Bash","arguments":"{}"}
			]
		}
	}`
	resp, err := o.TransformStream(context.Background(), []byte(event))
	if err != nil {
		t.Fatalf("TransformStream: %v", err)
	}
	if resp == nil || len(resp.Choices) != 1 || resp.Choices[0].FinishReason == nil {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if *resp.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("want finish_reason=tool_calls, got %q", *resp.Choices[0].FinishReason)
	}
}

// TestTransformStreamCompletedFallsBackToTrackedFunctionCall exercises the
// path where the terminal event omits `response.output` (some OpenAI-
// compat upstreams do this) and the tool_calls decision must consult
// the items tracked during the stream.
func TestTransformStreamCompletedFallsBackToTrackedFunctionCall(t *testing.T) {
	o := &ResponseOutbound{}
	// Prime the tracked output items by sending an added event first.
	addedEvent := `{
		"type":"response.output_item.added",
		"output_index":0,
		"item":{"type":"function_call","call_id":"c1","name":"Bash"}
	}`
	if _, err := o.TransformStream(context.Background(), []byte(addedEvent)); err != nil {
		t.Fatalf("prime added: %v", err)
	}
	// Now the completed event without an output array should still surface
	// tool_calls because the tracked items remember the function_call.
	completed := `{"type":"response.completed","response":{"status":"completed"}}`
	resp, err := o.TransformStream(context.Background(), []byte(completed))
	if err != nil {
		t.Fatalf("TransformStream completed: %v", err)
	}
	if resp == nil || len(resp.Choices) != 1 || resp.Choices[0].FinishReason == nil {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if *resp.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("want tool_calls from tracked items, got %q", *resp.Choices[0].FinishReason)
	}
}

// O-H4: Responses streaming "response.refusal.delta" must be forwarded as
// Choice.Delta.Refusal so downstream inbounds can surface the safety refusal
// as a distinct content part. Before this fix the outbound switch hit its
// default branch and dropped the entire refusal payload, so clients saw a
// truncated assistant message without a reason.
func TestTransformStreamRefusalDeltaIsForwarded(t *testing.T) {
	o := &ResponseOutbound{}
	event := `{"type":"response.refusal.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"I cannot help with that."}`
	resp, err := o.TransformStream(context.Background(), []byte(event))
	if err != nil {
		t.Fatalf("TransformStream refusal.delta: %v", err)
	}
	if resp == nil || len(resp.Choices) != 1 || resp.Choices[0].Delta == nil {
		t.Fatalf("expected 1 choice with delta, got %+v", resp)
	}
	if got := resp.Choices[0].Delta.Refusal; got != "I cannot help with that." {
		t.Fatalf("expected refusal to be forwarded, got %q", got)
	}
}

// O-H4: `response.refusal.done` carries the full refusal text which was
// already streamed via delta events. Re-emitting it would double-count on the
// inbound accumulator, so the outbound must drop the event.

func TestSanitizeResponsesItemsAddsTypedReasoningSummaryDefaults(t *testing.T) {
	items := sanitizeResponsesItems([]ResponsesItem{{
		Type:    "reasoning",
		Summary: []ResponsesReasoningSummary{{}},
	}})
	if len(items) != 1 || len(items[0].Summary) != 1 {
		t.Fatalf("expected one reasoning summary item, got %#v", items)
	}
	if items[0].Summary[0].Type != "summary_text" || items[0].Summary[0].Text != "" {
		t.Fatalf("expected default summary_text with empty text, got %#v", items[0].Summary[0])
	}
}

func TestSanitizeResponsesRawSummaryRepairsNullFields(t *testing.T) {
	raw, changed, ok := sanitizeResponsesRawSummary(json.RawMessage(`[{"type":null,"text":null}]`))
	if !ok || !changed {
		t.Fatalf("expected raw summary to be repaired, ok=%v changed=%v", ok, changed)
	}
	var summary []map[string]any
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatalf("unmarshal sanitized summary failed: %v", err)
	}
	if len(summary) != 1 || summary[0]["type"] != "summary_text" || summary[0]["text"] != "" {
		t.Fatalf("expected repaired summary_text entry, got %#v", summary)
	}
}

func TestTransformStreamPreservesTextDeltaBeforeOutputItemAdded(t *testing.T) {
	o := &ResponseOutbound{}
	first := `{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"hel"}`
	if _, err := o.TransformStream(context.Background(), []byte(first)); err != nil {
		t.Fatalf("text delta before added: %v", err)
	}
	added := `{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant"}}`
	if _, err := o.TransformStream(context.Background(), []byte(added)); err != nil {
		t.Fatalf("output item added: %v", err)
	}
	second := `{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"lo"}`
	if _, err := o.TransformStream(context.Background(), []byte(second)); err != nil {
		t.Fatalf("second text delta: %v", err)
	}
	completed := `{"type":"response.completed","response":{"status":"completed"}}`
	resp, err := o.TransformStream(context.Background(), []byte(completed))
	if err != nil {
		t.Fatalf("completed: %v", err)
	}
	if len(resp.RawResponsesOutputItems) == 0 {
		t.Fatalf("expected tracked output items, got %+v", resp)
	}
	var items []map[string]any
	if err := json.Unmarshal(resp.RawResponsesOutputItems, &items); err != nil {
		t.Fatalf("unmarshal raw output failed: %v", err)
	}
	content := items[0]["content"].([]any)
	part := content[0].(map[string]any)
	if part["type"] != "output_text" || part["text"] != "hello" {
		t.Fatalf("expected merged text delta, got %#v", part)
	}
}

func TestTransformStreamPreservesFunctionCallDeltaBeforeOutputItemAdded(t *testing.T) {
	o := &ResponseOutbound{}
	delta := `{"type":"response.function_call_arguments.delta","output_index":0,"call_id":"call_123","name":"lookup","delta":"{}"}`
	if _, err := o.TransformStream(context.Background(), []byte(delta)); err != nil {
		t.Fatalf("function_call delta before added: %v", err)
	}
	added := `{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call"}}`
	if _, err := o.TransformStream(context.Background(), []byte(added)); err != nil {
		t.Fatalf("function_call output item added: %v", err)
	}
	completed := `{"type":"response.completed","response":{"status":"completed"}}`
	resp, err := o.TransformStream(context.Background(), []byte(completed))
	if err != nil {
		t.Fatalf("completed: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(resp.RawResponsesOutputItems, &items); err != nil {
		t.Fatalf("unmarshal raw output failed: %v", err)
	}
	if items[0]["type"] != "function_call" || items[0]["call_id"] != "call_123" || items[0]["name"] != "lookup" || items[0]["arguments"] != "{}" {
		t.Fatalf("expected merged function_call item, got %#v", items[0])
	}
}

func TestTransformStreamPreservesReasoningDeltaBeforeOutputItemAdded(t *testing.T) {
	o := &ResponseOutbound{}
	delta := `{"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":0,"delta":"step"}`
	if _, err := o.TransformStream(context.Background(), []byte(delta)); err != nil {
		t.Fatalf("reasoning delta before added: %v", err)
	}
	added := `{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","encrypted_content":"enc"}}`
	if _, err := o.TransformStream(context.Background(), []byte(added)); err != nil {
		t.Fatalf("reasoning output item added: %v", err)
	}
	completed := `{"type":"response.completed","response":{"status":"completed"}}`
	resp, err := o.TransformStream(context.Background(), []byte(completed))
	if err != nil {
		t.Fatalf("completed: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(resp.RawResponsesOutputItems, &items); err != nil {
		t.Fatalf("unmarshal raw output failed: %v", err)
	}
	summary := items[0]["summary"].([]any)
	part := summary[0].(map[string]any)
	if part["type"] != "summary_text" || part["text"] != "step" {
		t.Fatalf("expected merged reasoning summary, got %#v", part)
	}
}
