package gemini

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestCleanGeminiSchemaRemovesPropertyNamesRecursively(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"propertyNames": map[string]any{
			"type": "string",
		},
		"properties": map[string]any{
			"payload": map[string]any{
				"type": "object",
				"propertyNames": map[string]any{
					"pattern": "^[a-z]+$",
				},
			},
		},
	}

	cleanGeminiSchema(schema)

	if _, ok := schema["propertyNames"]; ok {
		t.Fatalf("expected top-level propertyNames to be removed")
	}
	props := schema["properties"].(map[string]any)
	payload := props["payload"].(map[string]any)
	if _, ok := payload["propertyNames"]; ok {
		t.Fatalf("expected nested propertyNames to be removed")
	}
}

func TestConvertGeminiRequestBindsToolCallThoughtSignature(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "gemini-3.1-pro",
		Messages: []model.Message{
			{
				Role: "assistant",
				Content: model.MessageContent{
					Content: stringPtr("I will call a tool."),
				},
				ReasoningBlocks: []model.ReasoningBlock{
					{Kind: model.ReasoningBlockKindThinking, Text: "thinking", Signature: "sig-thought", Provider: "gemini"},
					{Kind: model.ReasoningBlockKindSignature, Signature: "sig-call", Provider: "gemini"},
				},
				ToolCalls: []model.ToolCall{
					{
						ID:   "call-1",
						Type: "function",
						Function: model.FunctionCall{
							Name:      "Bash",
							Arguments: `{"cmd":"pwd"}`,
						},
					},
				},
			},
		},
	}

	out := convertLLMToGeminiRequest(req)
	parts := out.Contents[0].Parts
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d: %+v", len(parts), parts)
	}
	if !parts[0].Thought || parts[0].ThoughtSignature != "sig-thought" {
		t.Fatalf("expected first part to be replayed thought with signature, got %+v", parts[0])
	}
	if parts[1].Text != "I will call a tool." || parts[1].ThoughtSignature != "" {
		t.Fatalf("expected visible text part without signature, got %+v", parts[1])
	}
	if parts[2].FunctionCall == nil || parts[2].ThoughtSignature != "sig-call" {
		t.Fatalf("expected functionCall part to keep its own signature, got %+v", parts[2])
	}
}

func TestConvertGeminiRequestDowngradesUnsignedHistoricalToolUse(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "gemini-3.1-pro",
		Messages: []model.Message{
			{
				Role: "assistant",
				ToolCalls: []model.ToolCall{
					{
						ID:   "call-1",
						Type: "function",
						Function: model.FunctionCall{
							Name:      "Bash",
							Arguments: `{"cmd":"ls"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				ToolCallID: stringPtr("call-1"),
				Content: model.MessageContent{
					Content: stringPtr("tool output"),
				},
			},
		},
	}

	out := convertLLMToGeminiRequest(req)
	// After fix: unsigned tool calls are now sent as functionCall parts (not degraded to text)
	if got := out.Contents[0].Parts[0].FunctionCall; got == nil {
		t.Fatalf("expected unsigned tool call to be sent as functionCall, got %+v", out.Contents[0].Parts[0])
	}
	if got := out.Contents[0].Parts[0].FunctionCall; got.Name != "Bash" {
		t.Fatalf("expected functionCall name to be 'Bash', got %+v", got)
	}
	if out.Contents[1].Parts[0].FunctionResponse == nil {
		t.Fatalf("expected matching tool result to be sent as functionResponse, got %+v", out.Contents[1].Parts[0])
	}
}

func TestDecodeGeminiToolResponseAcceptsScalarJSON(t *testing.T) {
	decoded, ok := decodeGeminiToolResponse(`true`)
	if !ok {
		t.Fatalf("expected scalar JSON to decode")
	}
	if got, ok := decoded["result"].(bool); !ok || !got {
		t.Fatalf("expected scalar JSON wrapped under result, got %+v", decoded)
	}
}

// TestConvertGeminiRequestFunctionResponseName verifies that a signed
// assistant→tool turn reaches Gemini with functionResponse.name equal to the
// originating functionCall.name, not the tool-call ID. Prior implementation
// filled Name with msg.ToolCallID, producing
// `INVALID_ARGUMENT: Function response name does not match any function call
// name` on any non-single-turn flow. (G-C2)
func TestConvertGeminiRequestFunctionResponseNameFromAssistantLookup(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "gemini-3.1-pro",
		Messages: []model.Message{
			{
				Role: "assistant",
				ReasoningBlocks: []model.ReasoningBlock{
					{Kind: model.ReasoningBlockKindSignature, Signature: "sig-call", Provider: "gemini"},
				},
				ToolCalls: []model.ToolCall{
					{
						ID:   "call_Bash_0",
						Type: "function",
						Function: model.FunctionCall{
							Name:      "Bash",
							Arguments: `{"cmd":"pwd"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				ToolCallID: stringPtr("call_Bash_0"),
				Content: model.MessageContent{
					Content: stringPtr(`{"stdout":"/tmp"}`),
				},
			},
		},
	}
	out := convertLLMToGeminiRequest(req)
	if len(out.Contents) < 2 {
		t.Fatalf("expected assistant + tool contents, got %d", len(out.Contents))
	}
	toolContent := out.Contents[1]
	fr := toolContent.Parts[0].FunctionResponse
	if fr == nil {
		t.Fatalf("expected functionResponse part, got %+v", toolContent.Parts[0])
	}
	if fr.Name != "Bash" {
		t.Fatalf("expected functionResponse.name=%q, got %q", "Bash", fr.Name)
	}
}

// TestConvertGeminiRequestFunctionResponseNamePrefersToolCallName covers the
// case where the inbound layer already resolved the function name and placed
// it on Message.ToolCallName; this path should win over the ID lookup.
func TestConvertGeminiRequestFunctionResponseNamePrefersToolCallName(t *testing.T) {
	nameOnly := "preferred_name"
	req := &model.InternalLLMRequest{
		Model: "gemini-2.5-flash",
		Messages: []model.Message{
			{
				Role:         "tool",
				ToolCallID:   stringPtr("call_99"),
				ToolCallName: &nameOnly,
				Content: model.MessageContent{
					Content: stringPtr(`{"ok":true}`),
				},
			},
		},
	}
	out := convertLLMToGeminiRequest(req)
	if len(out.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(out.Contents))
	}
	fr := out.Contents[0].Parts[0].FunctionResponse
	if fr == nil {
		t.Fatalf("expected functionResponse part, got %+v", out.Contents[0].Parts[0])
	}
	if fr.Name != nameOnly {
		t.Fatalf("expected functionResponse.name=%q, got %q", nameOnly, fr.Name)
	}
}

// TestConvertGeminiResponseCodeExecutionParts verifies G-H9: when Gemini
// returns ExecutableCode and CodeExecutionResult parts (emitted by the
// sandboxed code_execution tool), the outbound transformer folds them
// into MessageContentPart entries with ServerToolUse / ServerToolResult
// envelopes so the existing cross-provider passthrough picks them up.
// Previously these parts were silently dropped during unmarshaling.
func TestConvertGeminiResponseCodeExecutionParts(t *testing.T) {
	reason := "STOP"
	resp := &model.GeminiGenerateContentResponse{
		Candidates: []*model.GeminiCandidate{
			{
				Index:        0,
				FinishReason: &reason,
				Content: &model.GeminiContent{
					Role: "model",
					Parts: []*model.GeminiPart{
						{Text: "Let me compute that."},
						{ExecutableCode: &model.GeminiExecutableCode{
							Language: "PYTHON",
							Code:     "print(1+1)",
						}},
						{CodeExecutionResult: &model.GeminiCodeExecutionResult{
							Outcome: "OUTCOME_OK",
							Output:  "2\n",
						}},
					},
				},
			},
		},
	}
	internal := convertGeminiToLLMResponse(resp, false, nil)
	if len(internal.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(internal.Choices))
	}
	msg := internal.Choices[0].Message
	if msg == nil {
		t.Fatalf("expected message, got nil")
	}
	parts := msg.Content.MultipleContent
	if len(parts) < 2 {
		t.Fatalf("expected at least 2 structured parts, got %d: %+v", len(parts), parts)
	}

	var sawUse, sawResult bool
	for _, p := range parts {
		if p.Type == "server_tool_use" && p.ServerToolUse != nil {
			sawUse = true
			if p.ServerToolUse.Name != "code_execution" {
				t.Errorf("expected server_tool_use.Name=code_execution, got %q", p.ServerToolUse.Name)
			}
			if !strings.Contains(string(p.ServerToolUse.Input), "PYTHON") {
				t.Errorf("expected server_tool_use.Input to carry language, got %s", string(p.ServerToolUse.Input))
			}
		}
		if p.Type == "server_tool_result" && p.ServerToolResult != nil {
			sawResult = true
			if p.ServerToolResult.BlockType != "code_execution_tool_result" {
				t.Errorf("expected BlockType=code_execution_tool_result, got %q", p.ServerToolResult.BlockType)
			}
			if p.ServerToolResult.IsError == nil || *p.ServerToolResult.IsError {
				t.Errorf("expected IsError=false for OUTCOME_OK, got %+v", p.ServerToolResult.IsError)
			}
			if !strings.Contains(string(p.ServerToolResult.Content), "2") {
				t.Errorf("expected result.Content to carry output, got %s", string(p.ServerToolResult.Content))
			}
		}
	}
	if !sawUse {
		t.Errorf("expected server_tool_use part, got %+v", parts)
	}
	if !sawResult {
		t.Errorf("expected server_tool_result part, got %+v", parts)
	}
}

// TestConvertGeminiResponseCodeExecutionResultFailedOutcome verifies the
// IsError flag is set for non-OK outcomes so downstream consumers can
// distinguish failed runs from successful ones.
func TestConvertGeminiResponseCodeExecutionResultFailedOutcome(t *testing.T) {
	reason := "STOP"
	resp := &model.GeminiGenerateContentResponse{
		Candidates: []*model.GeminiCandidate{
			{
				Index:        0,
				FinishReason: &reason,
				Content: &model.GeminiContent{
					Role: "model",
					Parts: []*model.GeminiPart{
						{CodeExecutionResult: &model.GeminiCodeExecutionResult{
							Outcome: "OUTCOME_FAILED",
							Output:  "SyntaxError",
						}},
					},
				},
			},
		},
	}
	internal := convertGeminiToLLMResponse(resp, false, nil)
	parts := internal.Choices[0].Message.Content.MultipleContent
	if len(parts) != 1 || parts[0].ServerToolResult == nil {
		t.Fatalf("expected single server_tool_result part, got %+v", parts)
	}
	if parts[0].ServerToolResult.IsError == nil || !*parts[0].ServerToolResult.IsError {
		t.Errorf("expected IsError=true for OUTCOME_FAILED, got %+v", parts[0].ServerToolResult.IsError)
	}
}

// TestConvertGeminiResponseGroundingMetadata verifies G-H10: Gemini's
// groundingMetadata surfaces on Choice.Grounding with all four sub-fields
// populated (queries, sources, supports, entry-point HTML). Empty
// groundingMetadata payloads leave Choice.Grounding nil so consumers can
// branch cheaply.
func TestConvertGeminiResponseGroundingMetadata(t *testing.T) {
	reason := "STOP"
	resp := &model.GeminiGenerateContentResponse{
		Candidates: []*model.GeminiCandidate{
			{
				Index:        0,
				FinishReason: &reason,
				Content: &model.GeminiContent{
					Role:  "model",
					Parts: []*model.GeminiPart{{Text: "Paris is the capital of France."}},
				},
				GroundingMetadata: &model.GeminiGroundingMetadata{
					SearchEntryPoint: &model.GeminiSearchEntryPoint{
						RenderedContent: "<div>search chip</div>",
					},
					WebSearchQueries: []string{"capital of France"},
					GroundingChunks: []*model.GeminiGroundingChunk{
						{Web: &model.GeminiGroundingChunkWeb{
							URI:   "https://example.com/paris",
							Title: "Paris - Wikipedia",
						}},
					},
					GroundingSupports: []*model.GeminiGroundingSupport{
						{
							Segment: &model.GeminiGroundingSegment{
								StartIndex: 0,
								EndIndex:   29,
								Text:       "Paris is the capital of France.",
							},
							GroundingChunkIndices: []int{0},
							ConfidenceScores:      []float64{0.95},
						},
					},
				},
			},
		},
	}
	internal := convertGeminiToLLMResponse(resp, false, nil)
	g := internal.Choices[0].Grounding
	if g == nil {
		t.Fatalf("expected Choice.Grounding populated, got nil")
	}
	if len(g.SearchQueries) != 1 || g.SearchQueries[0] != "capital of France" {
		t.Errorf("expected search queries, got %+v", g.SearchQueries)
	}
	if len(g.Sources) != 1 || g.Sources[0].URI != "https://example.com/paris" {
		t.Errorf("expected source URI, got %+v", g.Sources)
	}
	if len(g.Supports) != 1 || g.Supports[0].SegmentEndIndex != 29 {
		t.Errorf("expected support span, got %+v", g.Supports)
	}
	if g.SearchEntryPointHTML != "<div>search chip</div>" {
		t.Errorf("expected entry-point HTML, got %q", g.SearchEntryPointHTML)
	}

	// Empty groundingMetadata should leave Choice.Grounding nil.
	resp2 := &model.GeminiGenerateContentResponse{
		Candidates: []*model.GeminiCandidate{{
			Index:             0,
			FinishReason:      &reason,
			Content:           &model.GeminiContent{Role: "model", Parts: []*model.GeminiPart{{Text: "hi"}}},
			GroundingMetadata: &model.GeminiGroundingMetadata{},
		}},
	}
	if convertGeminiToLLMResponse(resp2, false, nil).Choices[0].Grounding != nil {
		t.Errorf("expected empty groundingMetadata to leave Grounding nil")
	}
}

// TestConvertGeminiResponseCitationMetadata verifies G-H10 citation spans
// round-trip with offsets and source URIs intact.
func TestConvertGeminiResponseCitationMetadata(t *testing.T) {
	reason := "STOP"
	resp := &model.GeminiGenerateContentResponse{
		Candidates: []*model.GeminiCandidate{{
			Index:        0,
			FinishReason: &reason,
			Content:      &model.GeminiContent{Role: "model", Parts: []*model.GeminiPart{{Text: "text"}}},
			CitationMetadata: &model.GeminiCitationMetadata{
				CitationSources: []*model.GeminiCitationSource{
					{StartIndex: 10, EndIndex: 42, URI: "https://example.com/src", License: "MIT"},
					{StartIndex: 50, EndIndex: 88, URI: "https://example.com/src2"},
				},
			},
		}},
	}
	cites := convertGeminiToLLMResponse(resp, false, nil).Choices[0].Citations
	if len(cites) != 2 {
		t.Fatalf("expected 2 citations, got %d", len(cites))
	}
	if cites[0].URI != "https://example.com/src" || cites[0].License != "MIT" {
		t.Errorf("first citation: %+v", cites[0])
	}
	if cites[1].StartIndex != 50 || cites[1].EndIndex != 88 {
		t.Errorf("second citation offsets: %+v", cites[1])
	}
}

// TestConvertGeminiResponseURLContextMetadata verifies G-H10 URL context
// metadata is carried through with status preserved, including the
// retrievedUrl → url fallback.
func TestConvertGeminiResponseURLContextMetadata(t *testing.T) {
	reason := "STOP"
	resp := &model.GeminiGenerateContentResponse{
		Candidates: []*model.GeminiCandidate{{
			Index:        0,
			FinishReason: &reason,
			Content:      &model.GeminiContent{Role: "model", Parts: []*model.GeminiPart{{Text: "text"}}},
			UrlContextMetadata: &model.GeminiUrlContextMetadata{
				URLMetadata: []*model.GeminiURLMetadata{
					{RetrievedURL: "https://a.example/", URLRetrievalStatus: "URL_RETRIEVAL_STATUS_SUCCESS"},
					{URL: "https://b.example/", URLRetrievalStatus: "URL_RETRIEVAL_STATUS_FAILED"},
				},
			},
		}},
	}
	u := convertGeminiToLLMResponse(resp, false, nil).Choices[0].URLContext
	if u == nil || len(u.URLs) != 2 {
		t.Fatalf("expected 2 URL entries, got %+v", u)
	}
	if u.URLs[0].URL != "https://a.example/" || u.URLs[0].Status != "URL_RETRIEVAL_STATUS_SUCCESS" {
		t.Errorf("first url: %+v", u.URLs[0])
	}
	// Fallback when only `url` is set (no retrievedUrl).
	if u.URLs[1].URL != "https://b.example/" {
		t.Errorf("expected URL fallback from retrievedUrl to url, got %+v", u.URLs[1])
	}
}

// TestConvertGeminiResponseSafetyRatings verifies G-M9: per-candidate and
// promptFeedback safety ratings both land on the Choice, including the
// synthetic-choice path when the prompt was blocked.
func TestConvertGeminiResponseSafetyRatings(t *testing.T) {
	reason := "STOP"
	resp := &model.GeminiGenerateContentResponse{
		Candidates: []*model.GeminiCandidate{{
			Index:        0,
			FinishReason: &reason,
			Content:      &model.GeminiContent{Role: "model", Parts: []*model.GeminiPart{{Text: "hi"}}},
			SafetyRatings: []*model.GeminiSafetyRating{
				{Category: "HARM_CATEGORY_HARASSMENT", Probability: "NEGLIGIBLE"},
				{Category: "HARM_CATEGORY_HATE_SPEECH", Probability: "LOW"},
			},
		}},
	}
	sr := convertGeminiToLLMResponse(resp, false, nil).Choices[0].SafetyRatings
	if len(sr) != 2 || sr[0].Category != "HARM_CATEGORY_HARASSMENT" {
		t.Fatalf("expected 2 safety ratings on candidate, got %+v", sr)
	}

	// Blocked-prompt path: promptFeedback.safetyRatings surface on the
	// synthetic choice even when the candidates slice is empty.
	blocked := &model.GeminiGenerateContentResponse{
		PromptFeedback: &model.GeminiPromptFeedback{
			BlockReason: "SAFETY",
			SafetyRatings: []*model.GeminiSafetyRating{
				{Category: "HARM_CATEGORY_SEXUALLY_EXPLICIT", Probability: "HIGH", Blocked: true},
			},
		},
	}
	choices := convertGeminiToLLMResponse(blocked, false, nil).Choices
	if len(choices) != 1 {
		t.Fatalf("expected synthetic choice for blocked prompt, got %d", len(choices))
	}
	if len(choices[0].SafetyRatings) != 1 || !choices[0].SafetyRatings[0].Blocked {
		t.Errorf("expected promptFeedback safety ratings on synthetic choice, got %+v", choices[0].SafetyRatings)
	}
}

// TestConvertDocumentToGeminiPartInlineLimitFallback verifies G-M10:
// base64 documents estimated to exceed the inline-data ceiling either
// (a) route to a pre-uploaded Files API URI when TransformerMetadata
// carries one, or (b) are dropped with a warning when no URI is
// provided. Small documents still go through as inline_data.
//
// The ceiling var is shrunk during the test so we don't need a 27 MB
// base64 fixture.
func TestConvertDocumentToGeminiPartInlineLimitFallback(t *testing.T) {
	orig := geminiInlineDataMaxBytes
	geminiInlineDataMaxBytes = 100 // decoded bytes
	defer func() { geminiInlineDataMaxBytes = orig }()

	bigPayload := strings.Repeat("A", 500) // ~375 decoded bytes > 100 limit
	doc := &model.DocumentSource{Type: "base64", MediaType: "application/pdf", Data: bigPayload}

	// No file URI → drop.
	if p := convertDocumentToGeminiPart(doc, &model.InternalLLMRequest{}); p != nil {
		t.Errorf("expected oversized doc to be dropped, got %+v", p)
	}

	// With generic URI → FileData reference.
	req := &model.InternalLLMRequest{
		TransformerMetadata: map[string]string{
			"gemini_files_api_uri": "https://generativelanguage.googleapis.com/v1beta/files/abc",
		},
	}
	p := convertDocumentToGeminiPart(doc, req)
	if p == nil || p.FileData == nil {
		t.Fatalf("expected FileData fallback, got %+v", p)
	}
	if p.FileData.FileURI != "https://generativelanguage.googleapis.com/v1beta/files/abc" {
		t.Errorf("expected generic URI, got %q", p.FileData.FileURI)
	}
	if p.InlineData != nil {
		t.Errorf("expected inline_data stripped on fallback, got %+v", p.InlineData)
	}

	// Per-media-type URI takes precedence over the generic one.
	req.TransformerMetadata["gemini_files_api_uri:application/pdf"] =
		"https://generativelanguage.googleapis.com/v1beta/files/pdf-specific"
	p = convertDocumentToGeminiPart(doc, req)
	if p == nil || p.FileData == nil ||
		p.FileData.FileURI != "https://generativelanguage.googleapis.com/v1beta/files/pdf-specific" {
		t.Errorf("expected per-mime URI precedence, got %+v", p)
	}

	// Small payloads still go through as inline_data unchanged.
	small := &model.DocumentSource{Type: "base64", MediaType: "application/pdf", Data: "AAAA"}
	p = convertDocumentToGeminiPart(small, &model.InternalLLMRequest{})
	if p == nil || p.InlineData == nil || p.InlineData.Data != "AAAA" {
		t.Errorf("expected small doc to keep inline_data, got %+v", p)
	}
}

// TestConvertGeminiRequestCandidateCount verifies G-M8: a numeric value
// in TransformerMetadata["gemini_candidate_count"] populates
// generationConfig.candidateCount. Non-positive / invalid values leave
// the field at its zero default (which Gemini treats as 1).
func TestConvertGeminiRequestCandidateCount(t *testing.T) {
	mk := func(meta map[string]string) *model.InternalLLMRequest {
		return &model.InternalLLMRequest{
			Model: "gemini-2.5-flash",
			Messages: []model.Message{
				{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}},
			},
			TransformerMetadata: meta,
		}
	}

	out := convertLLMToGeminiRequest(mk(map[string]string{"gemini_candidate_count": "3"}))
	if out.GenerationConfig == nil || out.GenerationConfig.CandidateCount != 3 {
		t.Fatalf("expected candidateCount=3, got %+v", out.GenerationConfig)
	}

	// n=1 is the default — we deliberately skip writing it to avoid a
	// redundant field on the wire.
	out = convertLLMToGeminiRequest(mk(map[string]string{"gemini_candidate_count": "1"}))
	if out.GenerationConfig != nil && out.GenerationConfig.CandidateCount != 0 {
		t.Errorf("expected candidateCount unset for n=1, got %d", out.GenerationConfig.CandidateCount)
	}

	// Invalid value → field stays unset.
	out = convertLLMToGeminiRequest(mk(map[string]string{"gemini_candidate_count": "abc"}))
	if out.GenerationConfig != nil && out.GenerationConfig.CandidateCount != 0 {
		t.Errorf("expected candidateCount unset for unparseable value")
	}
}

// TestConvertGeminiRequestSpeechConfigRawPassthrough verifies G-H11 when
// the caller supplies a fully-formed speechConfig JSON blob: the outbound
// transformer forwards the bytes verbatim into generationConfig.
func TestConvertGeminiRequestSpeechConfigRawPassthrough(t *testing.T) {
	raw := json.RawMessage(`{"voiceConfig":{"prebuiltVoiceConfig":{"voiceName":"Kore"}},"languageCode":"en-US"}`)
	req := &model.InternalLLMRequest{
		Model: "gemini-2.5-flash",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}},
		},
		GeminiSpeechConfig: raw,
	}
	out := convertLLMToGeminiRequest(req)
	if out.GenerationConfig == nil || len(out.GenerationConfig.SpeechConfig) == 0 {
		t.Fatalf("expected speechConfig on generationConfig, got %+v", out.GenerationConfig)
	}
	if !strings.Contains(string(out.GenerationConfig.SpeechConfig), "Kore") {
		t.Errorf("expected raw speechConfig preserved, got %s", out.GenerationConfig.SpeechConfig)
	}
}

// TestConvertGeminiRequestSpeechConfigSynthesizeFromAudioVoice verifies
// G-H11 when the caller only supplies the generic request.Audio.Voice
// pair: the transformer synthesises a minimal prebuiltVoiceConfig.
func TestConvertGeminiRequestSpeechConfigSynthesizeFromAudioVoice(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "gemini-2.5-flash",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}},
		},
		Audio: &struct {
			Format string `json:"format,omitempty"`
			Voice  string `json:"voice,omitempty"`
		}{Voice: "Charon"},
	}
	out := convertLLMToGeminiRequest(req)
	if out.GenerationConfig == nil || len(out.GenerationConfig.SpeechConfig) == 0 {
		t.Fatalf("expected synthesised speechConfig, got %+v", out.GenerationConfig)
	}
	wire := string(out.GenerationConfig.SpeechConfig)
	if !strings.Contains(wire, "Charon") || !strings.Contains(wire, "prebuiltVoiceConfig") {
		t.Errorf("expected prebuiltVoiceConfig with voiceName, got %s", wire)
	}
}

// TestConvertGeminiRequestSpeechConfigOmittedWhenAbsent verifies that no
// speechConfig key is emitted when neither channel is set.
func TestConvertGeminiRequestSpeechConfigOmittedWhenAbsent(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "gemini-2.5-flash",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}},
		},
	}
	out := convertLLMToGeminiRequest(req)
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "speechConfig") {
		t.Errorf("expected omitempty speechConfig, wire=%s", b)
	}
}

func stringPtr(v string) *string {
	return &v
}

// TestConvertGeminiRequestCachedContentAndLabels verifies G-H8:
//   - InternalLLMRequest.GeminiCachedContentRef populates the top-level
//     `cachedContent` field on the Gemini wire body.
//   - InternalLLMRequest.Metadata is forwarded as `labels` (same k/v
//     semantics on both sides).
//   - Empty / whitespace-only cached-content refs are dropped (wire omits
//     the field entirely thanks to omitempty).
func TestConvertGeminiRequestCachedContentAndLabels(t *testing.T) {
	ref := "cachedContents/abc123"
	req := &model.InternalLLMRequest{
		Model: "gemini-2.5-flash",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}},
		},
		GeminiCachedContentRef: &ref,
		Metadata: map[string]string{
			"project": "demo",
			"team":    "eng",
		},
	}
	out := convertLLMToGeminiRequest(req)
	if out.CachedContent != ref {
		t.Errorf("expected cachedContent=%q, got %q", ref, out.CachedContent)
	}
	if out.Labels["project"] != "demo" || out.Labels["team"] != "eng" {
		t.Errorf("expected labels to include project/team, got %+v", out.Labels)
	}

	// Whitespace-only ref should drop the field.
	blank := "   "
	req.GeminiCachedContentRef = &blank
	out = convertLLMToGeminiRequest(req)
	if out.CachedContent != "" {
		t.Errorf("expected blank cachedContent to be dropped, got %q", out.CachedContent)
	}

	// Nil ref + nil metadata -> wire body omits both keys.
	req.GeminiCachedContentRef = nil
	req.Metadata = nil
	out = convertLLMToGeminiRequest(req)
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wire := string(b)
	if strings.Contains(wire, `"cachedContent"`) {
		t.Errorf("expected omitempty on cachedContent, wire=%s", wire)
	}
	if strings.Contains(wire, `"labels"`) {
		t.Errorf("expected omitempty on labels, wire=%s", wire)
	}
}

// TestConvertGeminiRequestSystemInstructionWireShape asserts the Gemini
// request JSON uses the camelCase `systemInstruction` key (not snake_case)
// and that the system instruction content omits `role` entirely, matching
// Gemini's REST spec. (G-C3)
// Ref: https://ai.google.dev/api/generate-content#request-body
func TestConvertGeminiRequestSystemInstructionWireShape(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "gemini-2.5-flash",
		Messages: []model.Message{
			{Role: "system", Content: model.MessageContent{Content: stringPtr("be concise")}},
			{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}},
		},
	}
	out := convertLLMToGeminiRequest(req)
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wire := string(b)
	if !strings.Contains(wire, `"systemInstruction":`) {
		t.Errorf("expected camelCase systemInstruction key, got %s", wire)
	}
	if strings.Contains(wire, `"system_instruction"`) {
		t.Errorf("unexpected snake_case key in wire: %s", wire)
	}
	// The systemInstruction body must not carry a role field. We look for
	// `"role":""` specifically; user / model roles are still allowed
	// elsewhere.
	if strings.Contains(wire, `"role":""`) {
		t.Errorf("systemInstruction should omit empty role, wire=%s", wire)
	}
	// Sanity-check the user turn still carries its role.
	if !strings.Contains(wire, `"role":"user"`) {
		t.Errorf("expected user role preserved, wire=%s", wire)
	}
}
