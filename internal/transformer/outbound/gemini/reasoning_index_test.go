package gemini

import (
	"context"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// G-C4: when Gemini streams multiple chunks, each carrying its own Thought /
// Signature parts, the ReasoningBlock.Index must stay monotonically
// increasing across chunks. Before this fix every chunk used its local slice
// length so downstream consumers could not bind signatures to the right
// thinking block across SSE boundaries.
func TestTransformStreamReasoningIndexIsGlobalAcrossChunks(t *testing.T) {
	outbound := &MessagesOutbound{}

	chunk1 := []byte(`{
		"candidates":[{
			"index":0,
			"content":{
				"role":"model",
				"parts":[
					{"thought":true,"text":"plan","thoughtSignature":"sig-a"}
				]
			}
		}]
	}`)
	chunk2 := []byte(`{
		"candidates":[{
			"index":0,
			"content":{
				"role":"model",
				"parts":[
					{"thought":true,"text":"refine","thoughtSignature":"sig-b"}
				]
			}
		}]
	}`)

	first, err := outbound.TransformStream(context.Background(), chunk1)
	if err != nil {
		t.Fatalf("first chunk: %v", err)
	}
	second, err := outbound.TransformStream(context.Background(), chunk2)
	if err != nil {
		t.Fatalf("second chunk: %v", err)
	}

	if got := firstReasoningBlockIndex(first); got != 0 {
		t.Fatalf("first chunk reasoning index = %d, want 0", got)
	}
	if got := firstReasoningBlockIndex(second); got != 1 {
		t.Fatalf("second chunk reasoning index = %d, want 1 (global counter)", got)
	}
}

// G-C4 (multi-candidate): counters are per-candidate — candidate 1 does not
// consume indices reserved for candidate 0.
func TestTransformStreamReasoningIndexIsPerCandidate(t *testing.T) {
	outbound := &MessagesOutbound{}

	chunk := []byte(`{
		"candidates":[
			{"index":0,"content":{"role":"model","parts":[{"thought":true,"text":"p0","thoughtSignature":"a"}]}},
			{"index":1,"content":{"role":"model","parts":[{"thought":true,"text":"p1","thoughtSignature":"b"}]}}
		]
	}`)

	resp, err := outbound.TransformStream(context.Background(), chunk)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}
	if len(resp.Choices) != 2 {
		t.Fatalf("expected 2 choices, got %d", len(resp.Choices))
	}
	for _, c := range resp.Choices {
		if c.Delta == nil || len(c.Delta.ReasoningBlocks) == 0 {
			t.Fatalf("choice %d missing reasoning blocks", c.Index)
		}
		if c.Delta.ReasoningBlocks[0].Index != 0 {
			t.Fatalf("candidate %d first block index = %d, want 0", c.Index, c.Delta.ReasoningBlocks[0].Index)
		}
	}
}

// G-H7: thoughtSignature attached to a functionCall part must be captured
// together with the tool call name on the ReasoningBlock so the outbound
// replay layer can map signatures back to their originating tool even when
// the order differs from the source turn.
func TestConvertGeminiResponseAnchorsFunctionCallSignatures(t *testing.T) {
	resp := &model.GeminiGenerateContentResponse{
		Candidates: []*model.GeminiCandidate{
			{
				Index: 0,
				Content: &model.GeminiContent{
					Role: "model",
					Parts: []*model.GeminiPart{
						{FunctionCall: &model.GeminiFunctionCall{Name: "search", Args: map[string]interface{}{"q": "x"}}, ThoughtSignature: "sig-search"},
						{FunctionCall: &model.GeminiFunctionCall{Name: "translate", Args: map[string]interface{}{"text": "y"}}, ThoughtSignature: "sig-translate"},
					},
				},
			},
		},
	}

	out := convertGeminiToLLMResponse(resp, false, nil)
	if len(out.Choices) != 1 || out.Choices[0].Message == nil {
		t.Fatalf("unexpected response: %+v", out)
	}
	blocks := out.Choices[0].Message.ReasoningBlocks
	wantMap := map[string]string{"search": "sig-search", "translate": "sig-translate"}
	gotMap := map[string]string{}
	for _, b := range blocks {
		if b.Kind != model.ReasoningBlockKindSignature {
			continue
		}
		if b.ToolCallName == "" || b.Signature == "" {
			continue
		}
		gotMap[b.ToolCallName] = b.Signature
	}
	if len(gotMap) != len(wantMap) {
		t.Fatalf("expected %d name-anchored signatures, got %d: %+v", len(wantMap), len(gotMap), gotMap)
	}
	for k, want := range wantMap {
		if got := gotMap[k]; got != want {
			t.Fatalf("signature for %q = %q, want %q", k, got, want)
		}
	}
}

// G-H7: the replay path must consult the name index first, so multi-tool
// assistant turns stay correctly paired even if the internal slice order
// differs from the tool call order.
func TestCollectGeminiSignaturesByNameReturnsFirstMatch(t *testing.T) {
	blocks := []model.ReasoningBlock{
		{Kind: model.ReasoningBlockKindSignature, Signature: "sig-a", ToolCallName: "search"},
		{Kind: model.ReasoningBlockKindSignature, Signature: "sig-b", ToolCallName: "translate"},
		{Kind: model.ReasoningBlockKindSignature, Signature: "sig-c", ToolCallName: "search"}, // duplicate name
		{Kind: model.ReasoningBlockKindThinking, Signature: "sig-d", ToolCallName: "search"},  // wrong kind
		{Kind: model.ReasoningBlockKindSignature, Signature: ""}, // empty
		{Kind: model.ReasoningBlockKindSignature, Signature: "sig-e"}, // no name
	}
	got := collectGeminiSignaturesByName(blocks)
	if got["search"] != "sig-a" {
		t.Fatalf("expected first-match for search, got %q", got["search"])
	}
	if got["translate"] != "sig-b" {
		t.Fatalf("expected first-match for translate, got %q", got["translate"])
	}
	if _, has := got[""]; has {
		t.Fatalf("should not index empty-name blocks: %+v", got)
	}
}

func firstReasoningBlockIndex(resp *model.InternalLLMResponse) int {
	if resp == nil || len(resp.Choices) == 0 || resp.Choices[0].Delta == nil {
		return -1
	}
	if len(resp.Choices[0].Delta.ReasoningBlocks) == 0 {
		return -1
	}
	return resp.Choices[0].Delta.ReasoningBlocks[0].Index
}

// G-H7 (end-to-end replay): when an assistant turn carries two functionCalls
// whose signatures were stored with ToolCallName, the outbound must bind each
// signature to the matching functionCall regardless of slice order. Prior to
// this fix the ordinal cursor could swap signatures and Gemini rejects the
// replay with 400.
func TestConvertLLMToGeminiRequestBindsSignaturesByName(t *testing.T) {
	arg1 := `{"q":"x"}`
	arg2 := `{"text":"y"}`
	// Assistant issued `search` before `translate`, but the stored
	// ReasoningBlocks are in the opposite order — only the name-indexed
	// lookup can re-pair them correctly.
	req := &model.InternalLLMRequest{
		Model: "gemini-3.1-pro",
		Messages: []model.Message{
			{
				Role:    "user",
				Content: model.MessageContent{Content: stringPtrGemini("hi")},
			},
			{
				Role: "assistant",
				ToolCalls: []model.ToolCall{
					{ID: "call-1", Type: "function", Function: model.FunctionCall{Name: "search", Arguments: arg1}},
					{ID: "call-2", Type: "function", Function: model.FunctionCall{Name: "translate", Arguments: arg2}},
				},
				ReasoningBlocks: []model.ReasoningBlock{
					{Kind: model.ReasoningBlockKindSignature, Index: 0, Signature: "sig-translate", Provider: "gemini", ToolCallName: "translate"},
					{Kind: model.ReasoningBlockKindSignature, Index: 1, Signature: "sig-search", Provider: "gemini", ToolCallName: "search"},
				},
			},
			{
				Role:         "tool",
				ToolCallID:   stringPtrGemini("call-1"),
				ToolCallName: stringPtrGemini("search"),
				Content:      model.MessageContent{Content: stringPtrGemini("ok")},
			},
			{
				Role:         "tool",
				ToolCallID:   stringPtrGemini("call-2"),
				ToolCallName: stringPtrGemini("translate"),
				Content:      model.MessageContent{Content: stringPtrGemini("ok")},
			},
		},
	}

	gReq := convertLLMToGeminiRequest(req)

	// Find the assistant (role=model) content and check its function_call parts.
	var modelContents []*model.GeminiContent
	for _, c := range gReq.Contents {
		if c != nil && c.Role == "model" {
			modelContents = append(modelContents, c)
		}
	}
	if len(modelContents) == 0 {
		t.Fatalf("no model content produced, got %+v", gReq.Contents)
	}

	sigByName := map[string]string{}
	for _, c := range modelContents {
		for _, p := range c.Parts {
			if p.FunctionCall != nil && p.ThoughtSignature != "" {
				sigByName[p.FunctionCall.Name] = p.ThoughtSignature
			}
		}
	}
	if sigByName["search"] != "sig-search" {
		t.Fatalf("search signature = %q, want sig-search — signatures are getting swapped", sigByName["search"])
	}
	if sigByName["translate"] != "sig-translate" {
		t.Fatalf("translate signature = %q, want sig-translate", sigByName["translate"])
	}
}

func stringPtrGemini(v string) *string {
	return &v
}
