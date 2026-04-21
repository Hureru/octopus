package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/xurl"
	"github.com/samber/lo"
)

type MessagesOutbound struct {
	// streamReasoningIndex tracks the global (cross-chunk) emission order of
	// reasoning blocks per candidate. Gemini 3 interleaves thought /
	// signature parts across many SSE chunks; the inbound aggregator needs a
	// monotonically-increasing Index to bind signatures to the correct
	// thinking block. See G-C4.
	streamReasoningIndex map[int]int
}

func (o *MessagesOutbound) nextReasoningIndex(candidateIndex int) int {
	if o.streamReasoningIndex == nil {
		o.streamReasoningIndex = make(map[int]int)
	}
	idx := o.streamReasoningIndex[candidateIndex]
	o.streamReasoningIndex[candidateIndex] = idx + 1
	return idx
}

func (o *MessagesOutbound) TransformRequest(ctx context.Context, request *model.InternalLLMRequest, baseUrl, key string) (*http.Request, error) {
	if request == nil {
		return nil, fmt.Errorf("request is nil")
	}

	request.NormalizeMessages()
	request.EnforceMessageAlternation(model.AlternationProviderGemini)

	// Convert internal request to Gemini format
	geminiReq := convertLLMToGeminiRequest(request)

	body, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal gemini request: %w", err)
	}

	// Build URL
	parsedUrl, err := url.Parse(strings.TrimSuffix(baseUrl, "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}

	// G-H5: When the channel BaseURL omits the API version segment
	// (`https://generativelanguage.googleapis.com`), the downstream request
	// would land on `/models/...` which 404s. Fall back to `/v1beta` when
	// no version prefix is configured; leave explicit `/v1` or `/v1beta`
	// paths alone.
	if !pathHasGeminiVersion(parsedUrl.Path) {
		parsedUrl.Path = strings.TrimRight(parsedUrl.Path, "/") + "/v1beta"
	}

	// Determine if streaming
	isStream := request.Stream != nil && *request.Stream
	method := "generateContent"
	if isStream {
		method = "streamGenerateContent"
	}

	// Build path: /models/{model}:{method}
	modelName := request.Model
	if !strings.Contains(modelName, "/") {
		modelName = "models/" + modelName
	}
	parsedUrl.Path = fmt.Sprintf("%s/%s:%s", parsedUrl.Path, modelName, method)

	// G-H6: Carry the API key in `x-goog-api-key` — the query-string form
	// still works but leaks the secret into proxy access logs and is
	// discouraged by Google's current docs.
	if isStream {
		q := parsedUrl.Query()
		q.Set("alt", "sse")
		parsedUrl.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsedUrl.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if key != "" {
		req.Header.Set("x-goog-api-key", key)
	}

	return req, nil
}

func (o *MessagesOutbound) TransformResponse(ctx context.Context, response *http.Response) (*model.InternalLLMResponse, error) {
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if len(body) == 0 {
		return nil, fmt.Errorf("response body is empty")
	}

	var geminiResp model.GeminiGenerateContentResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gemini response: %w", err)
	}

	// Convert Gemini response to internal format
	return convertGeminiToLLMResponse(&geminiResp, false, nil), nil
}

func (o *MessagesOutbound) TransformStream(ctx context.Context, eventData []byte) (*model.InternalLLMResponse, error) {
	// Handle [DONE] marker
	if bytes.HasPrefix(eventData, []byte("[DONE]")) || len(eventData) == 0 {
		return &model.InternalLLMResponse{
			Object: "[DONE]",
		}, nil
	}

	// Parse Gemini streaming response
	var geminiResp model.GeminiGenerateContentResponse
	if err := json.Unmarshal(eventData, &geminiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gemini stream chunk: %w", err)
	}

	// Convert to internal format, handing in a per-candidate global index
	// counter so ReasoningBlock.Index stays monotonically increasing across
	// stream chunks (G-C4).
	return convertGeminiToLLMResponse(&geminiResp, true, o.nextReasoningIndex), nil
}

// Helper functions

// reasoningToThinkingBudget maps reasoning effort levels to thinking budget in tokens
// https://ai.google.dev/gemini-api/docs/thinking
func reasoningToThinkingBudget(effort string) int32 {
	switch strings.ToLower(effort) {
	case "low":
		return 1024
	case "medium":
		return 4096
	case "high":
		return 24576
	default:
		// 防御性：未知值走动态
		return -1
	}
}

// pathHasGeminiVersion reports whether the configured base-URL path already
// contains a Gemini API version segment (`/v1`, `/v1beta`, etc.). Used by
// G-H5 to decide whether to prepend `/v1beta` as a fallback when channels
// were provisioned with a bare hostname. Matching on a leading `/v` prefix
// covers the versions Google documents (v1, v1beta, v1beta2, v1alpha) and
// will survive future bumps without churn.
func pathHasGeminiVersion(p string) bool {
	segment := strings.Trim(p, "/")
	if segment == "" {
		return false
	}
	first := segment
	if idx := strings.Index(segment, "/"); idx >= 0 {
		first = segment[:idx]
	}
	if len(first) < 2 || first[0] != 'v' {
		return false
	}
	// Must be `v<digit>...`; `/viewer` etc. should not count.
	return first[1] >= '0' && first[1] <= '9'
}

// canonicalGeminiModality normalises a client-supplied modality keyword into
// the Gemini wire shape. Gemini accepts TEXT / IMAGE / AUDIO (upper case);
// unknown values return the empty string so the caller drops them rather
// than letting a 400 surface at request time.
func canonicalGeminiModality(m string) string {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "text":
		return "TEXT"
	case "image":
		return "IMAGE"
	case "audio":
		return "AUDIO"
	default:
		return ""
	}
}

// convertDocumentToGeminiPart maps an Anthropic-style document block onto a
// GeminiPart. PDF / image payloads become inline_data; text documents fall
// back to a Text part prefixed with the title/context so the model still
// has the metadata. URL-sourced documents degrade to a text hint since
// Gemini does not fetch URLs inline.
func convertDocumentToGeminiPart(doc *model.DocumentSource) *model.GeminiPart {
	if doc == nil {
		return nil
	}
	switch doc.Type {
	case "base64":
		if doc.Data == "" {
			return nil
		}
		mime := doc.MediaType
		if mime == "" {
			mime = "application/pdf"
		}
		return &model.GeminiPart{
			InlineData: &model.GeminiBlob{MimeType: mime, Data: doc.Data},
		}
	case "url":
		if doc.URL == "" {
			return nil
		}
		// Gemini FileData supports Google-Cloud-Storage / gs:// URIs, not
		// arbitrary HTTPS; fall back to a text hint so the user sees the
		// reference instead of having the block silently dropped.
		hint := buildDocumentTextHint(doc, "document at "+doc.URL)
		return &model.GeminiPart{Text: hint}
	case "text":
		text := doc.Text
		if text == "" {
			text = doc.Data
		}
		if text == "" {
			return nil
		}
		return &model.GeminiPart{Text: buildDocumentTextHint(doc, text)}
	default:
		return nil
	}
}

// buildDocumentTextHint joins title / context / body into a single
// whitespace-separated block. Used as a fallback when Gemini (or any other
// non-Anthropic provider) cannot embed a native document.
func buildDocumentTextHint(doc *model.DocumentSource, body string) string {
	parts := make([]string, 0, 3)
	if doc.Title != "" {
		parts = append(parts, "Title: "+doc.Title)
	}
	if doc.Context != "" {
		parts = append(parts, "Context: "+doc.Context)
	}
	if body != "" {
		parts = append(parts, body)
	}
	return strings.Join(parts, "\n\n")
}

func audioTypeToMimeType(format string) string {
	switch format {
	case "wav":
		return "audio/wav"
	case "mp3":
		return "audio/mp3"
	case "aiff":
		return "audio/aiff"
	case "aac":
		return "audio/aac"
	case "ogg":
		return "audio/ogg"
	case "flac":
		return "audio/flac"
	default:
		return "audio/wav"
	}
}

// collectGeminiSignatures flattens blocks that carry a Gemini thoughtSignature into the order
// they were produced upstream. It accepts both dedicated signature blocks and thinking blocks
// that happened to carry one (some SDK variants record them that way).
func collectGeminiSignatures(blocks []model.ReasoningBlock) []string {
	out := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.Signature == "" {
			continue
		}
		switch b.Kind {
		case model.ReasoningBlockKindSignature:
			out = append(out, b.Signature)
		}
	}
	return out
}

// collectGeminiSignaturesByName indexes Signature-kind blocks by the tool
// call they originated from (ToolCallName). This lets the outbound replay
// attach each signature to its matching functionCall, which is required for
// multi-tool turns — Gemini 3 rejects the request if a signature is bound
// to the wrong functionCall. See G-H7.
//
// Blocks without a ToolCallName are ignored; the caller falls back to the
// ordinal list returned by collectGeminiSignatures for them.
func collectGeminiSignaturesByName(blocks []model.ReasoningBlock) map[string]string {
	out := make(map[string]string, len(blocks))
	for _, b := range blocks {
		if b.Kind != model.ReasoningBlockKindSignature || b.Signature == "" {
			continue
		}
		name := strings.TrimSpace(b.ToolCallName)
		if name == "" {
			continue
		}
		if _, exists := out[name]; exists {
			continue
		}
		out[name] = b.Signature
	}
	return out
}

func buildGeminiThoughtParts(blocks []model.ReasoningBlock) []*model.GeminiPart {
	parts := make([]*model.GeminiPart, 0, len(blocks))
	for _, b := range blocks {
		if b.Kind != model.ReasoningBlockKindThinking {
			continue
		}
		part := &model.GeminiPart{Thought: true}
		if b.Text != "" {
			part.Text = b.Text
		}
		if b.Signature != "" {
			part.ThoughtSignature = b.Signature
		}
		parts = append(parts, part)
	}
	return parts
}

// nextGeminiSignature pops the next signature string, advancing the caller-managed cursor.
// Returns false when no more signatures are available for the current assistant turn.
func nextGeminiSignature(sigs []string, cursor *int) (string, bool) {
	if cursor == nil || *cursor >= len(sigs) {
		return "", false
	}
	s := sigs[*cursor]
	*cursor++
	return s, true
}

// logGeminiSignatureAudit emits the audit counter for Gemini thoughtSignature
// extraction (signatures attached to text / function_call parts from the
// upstream response). direction is "extract" for now; inject is logged
// inline in convertLLMToGeminiRequest. Fixed event name
// `transformer.reasoning.signature.passthrough` allows downstream log
// pipelines to aggregate across providers.
func logGeminiSignatureAudit(direction string, blocks []model.ReasoningBlock) {
	var thinking, sigCount int
	for _, rb := range blocks {
		switch rb.Kind {
		case model.ReasoningBlockKindThinking:
			thinking++
			if rb.Signature != "" {
				sigCount++
			}
		case model.ReasoningBlockKindSignature:
			if rb.Signature != "" {
				sigCount++
			}
		}
	}
	if thinking == 0 && sigCount == 0 {
		return
	}
	log.Debugw("transformer.reasoning.signature.passthrough",
		"provider", "gemini",
		"direction", direction,
		"thinking_count", thinking,
		"signature_count", sigCount,
	)
}

func convertLLMToGeminiRequest(request *model.InternalLLMRequest) *model.GeminiGenerateContentRequest {
	geminiReq := &model.GeminiGenerateContentRequest{
		Contents: []*model.GeminiContent{},
	}

	// Convert messages
	var systemInstruction *model.GeminiContent
	degradedToolCalls := map[string]string{}
	// toolCallNamesByID captures the Function.Name of every assistant tool
	// call seen so far, keyed by the call's ID. Gemini requires
	// `functionResponse.name` to match the originating `functionCall.name`
	// byte-for-byte, so we use this map to look the name up when a
	// subsequent tool-result message only carries an ID. Preserved across
	// assistant turns so multi-round conversations still resolve correctly.
	toolCallNamesByID := map[string]string{}

	for _, msg := range request.Messages {
		switch msg.Role {
		case "system", "developer":
			// Collect system messages into system instruction
			if systemInstruction == nil {
				systemInstruction = &model.GeminiContent{
					Parts: []*model.GeminiPart{},
				}
			}
			if msg.Content.Content != nil {
				systemInstruction.Parts = append(systemInstruction.Parts, &model.GeminiPart{
					Text: *msg.Content.Content,
				})
			}

		case "user":
			content := &model.GeminiContent{
				Role:  "user",
				Parts: []*model.GeminiPart{},
			}
			if msg.Content.Content != nil {
				content.Parts = append(content.Parts, &model.GeminiPart{
					Text: *msg.Content.Content,
				})
			}

			if msg.Content.MultipleContent != nil {
				for _, part := range msg.Content.MultipleContent {
					switch part.Type {
					case "text":
						if part.Text != nil {
							content.Parts = append(content.Parts, &model.GeminiPart{
								Text: *part.Text,
							})
						}
					case "image_url":
						// get mime type from url extension
						dataurl := xurl.ParseDataURL(part.ImageURL.URL)
						if dataurl != nil && dataurl.IsBase64 {
							content.Parts = append(content.Parts, &model.GeminiPart{
								InlineData: &model.GeminiBlob{
									MimeType: dataurl.MediaType,
									Data:     dataurl.Data,
								},
							})
						}
					case "input_audio":
						if part.Audio != nil {
							content.Parts = append(content.Parts, &model.GeminiPart{
								InlineData: &model.GeminiBlob{
									MimeType: audioTypeToMimeType(part.Audio.Format),
									Data:     part.Audio.Data,
								},
							})
						}
					case "file":
						if part.File != nil {
							dataurl := xurl.ParseDataURL(part.File.FileData)
							if dataurl != nil && dataurl.IsBase64 {
								content.Parts = append(content.Parts, &model.GeminiPart{
									InlineData: &model.GeminiBlob{
										MimeType: dataurl.MediaType,
										Data:     dataurl.Data,
									},
								})
							}
						}
					case "document":
						if p := convertDocumentToGeminiPart(part.Document); p != nil {
							content.Parts = append(content.Parts, p)
						}
					case "server_tool_use", "server_tool_result":
						// Gemini has no native server-tool equivalent. Drop
						// with a warning so the request still dispatches;
						// the relay layer may surface an X-Octopus-Warning
						// header.
						log.Warnf("gemini: dropping unsupported %q block", part.Type)
					}
				}
			}

			geminiReq.Contents = append(geminiReq.Contents, content)

		case "assistant":
			content := &model.GeminiContent{
				Role:  "model",
				Parts: []*model.GeminiPart{},
			}
			// Gemini 3 requires Part-level thoughtSignature verbatim on multi-turn function
			// calling. Replay Gemini-authored thinking parts as thoughts, and keep standalone
			// signature blocks for matching functionCall parts only.
			geminiBlocks := msg.ReasoningBlocksByProvider("gemini")
			content.Parts = append(content.Parts, buildGeminiThoughtParts(geminiBlocks)...)
			geminiSigs := collectGeminiSignatures(geminiBlocks)
			geminiSigByName := collectGeminiSignaturesByName(geminiBlocks)
			sigIdx := 0
			// Handle text content
			if msg.Content.Content != nil && *msg.Content.Content != "" {
				content.Parts = append(content.Parts, &model.GeminiPart{Text: *msg.Content.Content})
			}
			// Handle tool calls
			if len(msg.ToolCalls) > 0 {
				for _, toolCall := range msg.ToolCalls {
					if toolCall.ID != "" && toolCall.Function.Name != "" {
						toolCallNamesByID[toolCall.ID] = toolCall.Function.Name
					}
					var args map[string]interface{}
					if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
						log.Warnf("gemini: failed to unmarshal tool call arguments for %s: %v", toolCall.Function.Name, err)
					}
					part := &model.GeminiPart{
						FunctionCall: &model.GeminiFunctionCall{
							Name: toolCall.Function.Name,
							Args: args,
						},
					}

					sig := strings.TrimSpace(toolCall.ThoughtSignature)
					if sig == "" {
						// Prefer a name-indexed lookup so multi-tool turns bind
						// the correct signature to each functionCall; fall back
						// to the ordinal cursor only if no name-match exists
						// (mostly legacy or single-tool traces). See G-H7.
						if named, ok := geminiSigByName[toolCall.Function.Name]; ok && named != "" {
							sig = named
							delete(geminiSigByName, toolCall.Function.Name)
						} else if fallbackSig, ok := nextGeminiSignature(geminiSigs, &sigIdx); ok {
							sig = fallbackSig
						}
					}

					if sig != "" {
						part.ThoughtSignature = sig
						content.Parts = append(content.Parts, part)
						continue
					}

					// Cross-provider tool histories (for example Anthropic inbound ->
					// Gemini outbound) don't carry Gemini thoughtSignatures. Degrade the
					// historical function call into plain text context instead of sending
					// an invalid functionCall part that Gemini rejects with 400.
					if fallback := formatGeminiToolCallFallback(toolCall); fallback != "" {
						content.Parts = append(content.Parts, &model.GeminiPart{Text: fallback})
					}
					if toolCall.ID != "" {
						degradedToolCalls[toolCall.ID] = toolCall.Function.Name
					}
				}
			}
			geminiReq.Contents = append(geminiReq.Contents, content)

			if len(geminiBlocks) > 0 || sigIdx > 0 {
				log.Debugw("transformer.reasoning.signature.passthrough",
					"provider", "gemini",
					"direction", "inject",
					"signature_count", sigIdx,
					"available_signatures", len(geminiSigs),
				)
			}

		case "tool":
			// Tool result. If the corresponding assistant functionCall had to be
			// downgraded to plain text because no Gemini thoughtSignature was
			// available, degrade the tool result too so the request stays valid.
			functionName := resolveGeminiToolResponseName(&msg, toolCallNamesByID)
			content := convertLLMToolResultToGeminiContent(&msg, functionName)
			if msg.ToolCallID != nil {
				if toolName, ok := degradedToolCalls[*msg.ToolCallID]; ok {
					content = convertLLMToolResultToGeminiTextContent(&msg, toolName)
				}
			}
			geminiReq.Contents = append(geminiReq.Contents, content)
		}
	}

	geminiReq.SystemInstruction = systemInstruction

	// Convert generation config
	config := &model.GeminiGenerationConfig{}
	hasConfig := false

	if request.MaxTokens != nil {
		config.MaxOutputTokens = int(*request.MaxTokens)
		hasConfig = true
	}
	if request.Temperature != nil {
		config.Temperature = request.Temperature
		hasConfig = true
	}
	if request.TopP != nil {
		config.TopP = request.TopP
		hasConfig = true
	}
	// G-H1 + A-H3 follow-up: prefer the native TopK field; fall back to the
	// legacy TransformerMetadata hook so older callers still work.
	if request.TopK != nil {
		topK := int(*request.TopK)
		config.TopK = &topK
		hasConfig = true
	} else if topKStr, ok := request.TransformerMetadata["gemini_top_k"]; ok {
		var topK int
		fmt.Sscanf(topKStr, "%d", &topK)
		config.TopK = &topK
		hasConfig = true
	}
	if request.PresencePenalty != nil {
		config.PresencePenalty = request.PresencePenalty
		hasConfig = true
	}
	if request.FrequencyPenalty != nil {
		config.FrequencyPenalty = request.FrequencyPenalty
		hasConfig = true
	}
	if request.Seed != nil {
		config.Seed = request.Seed
		hasConfig = true
	}
	if request.Logprobs != nil {
		enabled := *request.Logprobs
		config.ResponseLogprobs = &enabled
		hasConfig = true
	}
	if request.TopLogprobs != nil {
		// Gemini caps logprobs at 5; anything higher would 400 upstream.
		n := int(*request.TopLogprobs)
		if n > 5 {
			n = 5
		}
		if n < 0 {
			n = 0
		}
		config.Logprobs = &n
		hasConfig = true
	}
	if mr, ok := request.TransformerMetadata["gemini_media_resolution"]; ok {
		if trimmed := strings.TrimSpace(mr); trimmed != "" {
			config.MediaResolution = trimmed
			hasConfig = true
		}
	}
	if request.Stop != nil && request.Stop.MultipleStop != nil {
		config.StopSequences = request.Stop.MultipleStop
		hasConfig = true
	} else if request.Stop != nil && request.Stop.Stop != nil {
		config.StopSequences = []string{*request.Stop.Stop}
		hasConfig = true
	}

	if request.ReasoningEffort != "" || request.ReasoningBudget != nil || request.AdaptiveThinking {
		decision := resolveThinkingConfig(request.Model, request.ReasoningBudget, request.ReasoningEffort, request.AdaptiveThinking)
		if decision.Supported {
			thinkingConfig := &model.GeminiThinkingConfig{
				IncludeThoughts: decision.IncludeThoughts,
			}
			if decision.UseLevel {
				// Empty Level signals "server-side dynamic default" for
				// Gemini 3.x — we deliberately avoid emitting an unsupported
				// "dynamic" / "none" string by leaving the field unset.
				if decision.Level != "" {
					thinkingConfig.ThinkingLevel = decision.Level
				}
			} else {
				b := decision.Budget
				thinkingConfig.ThinkingBudget = &b
			}
			config.ThinkingConfig = thinkingConfig
			hasConfig = true
		}
	}

	// Convert ResponseFormat to ResponseMimeType and ResponseSchema
	if request.ResponseFormat != nil {
		switch request.ResponseFormat.Type {
		case "json_object":
			config.ResponseMimeType = "application/json"
			hasConfig = true
		case "json_schema":
			config.ResponseMimeType = "application/json"
			if request.ResponseFormat.Schema != nil {
				geminiSchema, err := request.ResponseFormat.Schema.ToGemini()
				if err != nil {
					// Lossy: Gemini cannot express every Draft-07 keyword.
					// Log-and-continue rather than fail the whole request —
					// the schema was advisory anyway and JSON mode still
					// constrains output shape.
					log.Warnf("gemini: response schema lossy conversion: %v", err)
				}
				if geminiSchema != nil {
					config.ResponseSchema = geminiSchema
				}
			} else if len(request.ResponseFormat.RawSchema) > 0 {
				// Passthrough path: decode the raw bytes into GeminiSchema
				// shape best-effort. If decoding fails we still set the
				// MIME type so the model returns JSON.
				var fallback model.GeminiSchema
				if err := json.Unmarshal(request.ResponseFormat.RawSchema, &fallback); err == nil {
					config.ResponseSchema = &fallback
				} else {
					log.Warnf("gemini: response raw schema passthrough failed: %v", err)
				}
			}
			hasConfig = true
		case "text":
			config.ResponseMimeType = "text/plain"
			hasConfig = true
		}
	}

	// Convert Modalities to ResponseModalities.
	// Gemini requires upper-case modality tokens (TEXT / IMAGE / AUDIO).
	// The previous `strings.ToUpper(m[:1]) + strings.ToLower(m[1:])` produced
	// "Text"/"Image" which Gemini 2.5+ rejects with a 400.
	if len(request.Modalities) > 0 {
		convertedModalities := make([]string, 0, len(request.Modalities))
		for _, m := range request.Modalities {
			if wire := canonicalGeminiModality(m); wire != "" {
				convertedModalities = append(convertedModalities, wire)
			}
		}
		if len(convertedModalities) > 0 {
			config.ResponseModalities = convertedModalities
			hasConfig = true
		}
	}

	if hasConfig {
		geminiReq.GenerationConfig = config
	}

	// Convert SafetySettings from metadata if present
	if safetyJSON, ok := request.TransformerMetadata["gemini_safety_settings"]; ok {
		var safetySettings []*model.GeminiSafetySetting
		if err := json.Unmarshal([]byte(safetyJSON), &safetySettings); err == nil {
			geminiReq.SafetySettings = safetySettings
		}
	}

	// Convert tools. Gemini's API treats GeminiTool entries as a
	// discriminated union — functionDeclarations + googleSearch cannot
	// co-exist per the current API — so we emit server tools as separate
	// GeminiTool entries and log a warning if the client mixes both.
	if len(request.Tools) > 0 {
		functionDeclarations := make([]*model.GeminiFunctionDeclaration, 0, len(request.Tools))
		serverTools := make([]*model.GeminiTool, 0, len(request.Tools))

		for _, tool := range request.Tools {
			switch tool.Type {
			case "function", "":
				var params map[string]any
				if len(tool.Function.Parameters) > 0 {
					// Best-effort: if schema can't be parsed, we still send the declaration without parameters.
					if err := json.Unmarshal(tool.Function.Parameters, &params); err != nil {
						log.Warnf("gemini: failed to unmarshal tool parameters for %s: %v", tool.Function.Name, err)
					}
				}
				cleanGeminiSchema(params)

				functionDeclarations = append(functionDeclarations, &model.GeminiFunctionDeclaration{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
					Parameters:  params,
				})
			case "server_search":
				serverTools = append(serverTools, &model.GeminiTool{GoogleSearch: &model.GeminiGoogleSearch{}})
			case "code_execution":
				serverTools = append(serverTools, &model.GeminiTool{CodeExecution: &model.GeminiCodeExecution{}})
			case "url_context":
				serverTools = append(serverTools, &model.GeminiTool{UrlContext: &model.GeminiUrlContext{}})
			default:
				log.Warnf("gemini: dropping unsupported tool type %q", tool.Type)
			}
		}

		tools := make([]*model.GeminiTool, 0, len(serverTools)+1)
		if len(functionDeclarations) > 0 {
			tools = append(tools, &model.GeminiTool{FunctionDeclarations: functionDeclarations})
		}
		tools = append(tools, serverTools...)

		if len(functionDeclarations) > 0 && len(serverTools) > 0 {
			log.Warnf("gemini: server tools and functionDeclarations declared together; provider may reject the request")
		}

		if len(tools) > 0 {
			geminiReq.Tools = tools
		}
	}

	// Convert tool choice to Gemini toolConfig.functionCallingConfig.
	// Gemini only exposes mode = AUTO/ANY/NONE + allowedFunctionNames, so the
	// rich OpenAI / Anthropic variants collapse into one of three modes.
	// Anthropic's disable_parallel_tool_use has no Gemini equivalent and is
	// dropped (Gemini always emits at most one functionCall per Part anyway).
	if request.ToolChoice != nil {
		mode := "AUTO"
		var allowed []string

		if request.ToolChoice.ToolChoice != nil {
			switch strings.ToLower(*request.ToolChoice.ToolChoice) {
			case "auto":
				mode = "AUTO"
			case "required", "any":
				mode = "ANY"
			case "none":
				mode = "NONE"
			}
		} else if named := request.ToolChoice.NamedToolChoice; named != nil {
			switch strings.ToLower(named.Type) {
			case "auto":
				mode = "AUTO"
			case "any", "required":
				mode = "ANY"
			case "none":
				mode = "NONE"
			case "function", "tool":
				mode = "ANY"
				if name := named.ResolvedFunctionName(); name != "" {
					allowed = []string{name}
				}
			}
		}

		geminiReq.ToolConfig = &model.GeminiToolConfig{
			FunctionCallingConfig: &model.GeminiFunctionCallingConfig{
				Mode:                 mode,
				AllowedFunctionNames: allowed,
			},
		}
	}

	// cachedContent reference (G-H8): forward so the upstream reuses the
	// managed cached prefix instead of re-reading the bytes.
	if request.GeminiCachedContentRef != nil {
		if ref := strings.TrimSpace(*request.GeminiCachedContentRef); ref != "" {
			geminiReq.CachedContent = ref
		}
	}

	// Labels (G-H8): Gemini accepts arbitrary string→string tags for
	// billing / analytics attribution. We reuse the OpenAI-style Metadata
	// channel since both APIs model the same concept (k/v tags). Callers
	// targeting Gemini specifically can set request.Metadata and know it
	// will surface as `labels` on the wire.
	if len(request.Metadata) > 0 {
		labels := make(map[string]string, len(request.Metadata))
		for k, v := range request.Metadata {
			labels[k] = v
		}
		geminiReq.Labels = labels
	}

	return geminiReq

}

func convertLLMToolResultToGeminiContent(msg *model.Message, functionName string) *model.GeminiContent {
	content := &model.GeminiContent{
		Role: "user", // Function responses come from user role in Gemini
	}

	var responseData map[string]any
	if msg.Content.Content != nil {
		if parsed, ok := decodeGeminiToolResponse(*msg.Content.Content); ok {
			responseData = parsed
		}
	}

	if responseData == nil {
		responseData = map[string]any{"result": lo.FromPtrOr(msg.Content.Content, "")}
	}

	fp := &model.GeminiFunctionResponse{
		Name:     functionName,
		Response: responseData,
	}

	content.Parts = []*model.GeminiPart{
		{FunctionResponse: fp},
	}

	return content
}

// resolveGeminiToolResponseName looks up the originating function name for a
// tool-result message. Gemini requires `functionResponse.name` to match the
// upstream `functionCall.name` byte-for-byte; falling back to the tool-call
// ID (as the previous implementation did) produces
// `INVALID_ARGUMENT: Function response name does not match any function call
// name`.
//
// Precedence:
//  1. `msg.ToolCallName` — populated by the inbound layer when available.
//  2. `toolCallNamesByID[msg.ToolCallID]` — name observed on a prior assistant
//     turn within the same request.
//  3. Empty string — caller is expected to downgrade the turn (degradedToolCalls
//     path) so Gemini still receives a well-formed message.
func resolveGeminiToolResponseName(msg *model.Message, toolCallNamesByID map[string]string) string {
	if msg == nil {
		return ""
	}
	if msg.ToolCallName != nil {
		if name := strings.TrimSpace(*msg.ToolCallName); name != "" {
			return name
		}
	}
	if msg.ToolCallID != nil {
		if name, ok := toolCallNamesByID[*msg.ToolCallID]; ok {
			return name
		}
	}
	return ""
}

func decodeGeminiToolResponse(raw string) (map[string]any, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || !json.Valid([]byte(trimmed)) {
		return nil, false
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil, false
	}
	switch value := decoded.(type) {
	case map[string]any:
		return value, true
	default:
		return map[string]any{"result": value}, true
	}
}

func formatGeminiToolCallFallback(toolCall model.ToolCall) string {
	name := strings.TrimSpace(toolCall.Function.Name)
	args := strings.TrimSpace(toolCall.Function.Arguments)
	switch {
	case name == "" && args == "":
		return ""
	case args == "":
		return fmt.Sprintf("Tool call: %s", name)
	case name == "":
		return fmt.Sprintf("Tool call arguments: %s", args)
	default:
		return fmt.Sprintf("Tool call %s arguments: %s", name, args)
	}
}

func convertLLMToolResultToGeminiTextContent(msg *model.Message, toolName string) *model.GeminiContent {
	text := formatGeminiToolResultFallback(msg, toolName)
	if text == "" {
		text = "Tool result received."
	}
	return &model.GeminiContent{
		Role:  "user",
		Parts: []*model.GeminiPart{{Text: text}},
	}
}

func formatGeminiToolResultFallback(msg *model.Message, toolName string) string {
	label := strings.TrimSpace(toolName)
	if label == "" {
		if msg.ToolCallName != nil {
			label = strings.TrimSpace(*msg.ToolCallName)
		}
	}
	body := strings.TrimSpace(flattenGeminiToolResultContent(msg))
	switch {
	case label != "" && body != "":
		return fmt.Sprintf("Tool result %s: %s", label, body)
	case label != "":
		return fmt.Sprintf("Tool result %s received.", label)
	default:
		return body
	}
}

func flattenGeminiToolResultContent(msg *model.Message) string {
	if msg == nil {
		return ""
	}
	if msg.Content.Content != nil {
		return *msg.Content.Content
	}
	if len(msg.Content.MultipleContent) == 0 {
		return ""
	}
	texts := make([]string, 0, len(msg.Content.MultipleContent))
	for _, part := range msg.Content.MultipleContent {
		if part.Text != nil && *part.Text != "" {
			texts = append(texts, *part.Text)
		}
	}
	if len(texts) > 0 {
		return strings.Join(texts, "\n")
	}
	if body, err := json.Marshal(msg.Content.MultipleContent); err == nil {
		return string(body)
	}
	return ""
}

func convertGeminiToLLMResponse(geminiResp *model.GeminiGenerateContentResponse, isStream bool, streamIndexer func(candidateIndex int) int) *model.InternalLLMResponse {
	resp := &model.InternalLLMResponse{
		Choices: []model.Choice{},
	}

	if isStream {
		resp.Object = "chat.completion.chunk"
	} else {
		resp.Object = "chat.completion"
	}

	if geminiResp.ResponseId != "" {
		resp.ID = geminiResp.ResponseId
	}
	if geminiResp.ModelVersion != "" {
		resp.Model = geminiResp.ModelVersion
	}
	if geminiResp.CreateTime != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, geminiResp.CreateTime); err == nil {
			resp.Created = parsed.Unix()
		} else if parsed, err := time.Parse(time.RFC3339, geminiResp.CreateTime); err == nil {
			resp.Created = parsed.Unix()
		}
	}

	// Convert candidates to choices
	for _, candidate := range geminiResp.Candidates {
		choice := model.Choice{
			Index: candidate.Index,
		}

		// nextReasoningIndex returns the Index to stamp on the next
		// ReasoningBlock for this candidate. For streaming, it draws from the
		// outbound's per-candidate counter so signatures bind to the right
		// thinking block across chunks (G-C4). For non-streaming, it falls
		// back to the local slice length as before.
		nextReasoningIndex := func() int {
			if streamIndexer != nil {
				return streamIndexer(candidate.Index)
			}
			// local fallback — len of whatever we've appended so far
			// (captured via closure below).
			return -1
		}

		// Convert finish reason
		if candidate.FinishReason != nil {
			reason := convertGeminiFinishReason(*candidate.FinishReason)
			choice.FinishReason = &reason
		}

		// Convert content
		if candidate.Content != nil {
			msg := &model.Message{
				Role: "assistant",
			}

			// Extract text, images and function calls from parts
			var textParts []string
			var contentParts []model.MessageContentPart
			var toolCalls []model.ToolCall
			var reasoningContent *string
			// hasStructuredPart flags parts that cannot be serialised as a
			// plain string (inline data, server_tool_use, server_tool_result).
			// When true the message must use MultipleContent instead of the
			// scalar Content field, or the structured parts are silently
			// dropped. G-H9.
			var hasStructuredPart bool
			var reasoningBlocks []model.ReasoningBlock
			assignIndex := func() int {
				if idx := nextReasoningIndex(); idx >= 0 {
					return idx
				}
				return len(reasoningBlocks)
			}

			for idx, part := range candidate.Content.Parts {
				if part.Thought {
					// Handle thinking/reasoning content
					if part.Text != "" && reasoningContent == nil {
						reasoningContent = &part.Text
					}
					// Thought Parts in Gemini 3 may carry a thoughtSignature that must be
					// replayed verbatim on the next turn.
					if part.Text != "" || part.ThoughtSignature != "" {
						reasoningBlocks = append(reasoningBlocks, model.ReasoningBlock{
							Kind:      model.ReasoningBlockKindThinking,
							Index:     assignIndex(),
							Text:      part.Text,
							Signature: part.ThoughtSignature,
							Provider:  "gemini",
						})
					}
				} else if part.Text != "" {
					textParts = append(textParts, part.Text)
					// Also add to content parts for multimodal response
					text := part.Text
					contentParts = append(contentParts, model.MessageContentPart{
						Type: "text",
						Text: &text,
					})
					if part.ThoughtSignature != "" {
						reasoningBlocks = append(reasoningBlocks, model.ReasoningBlock{
							Kind:      model.ReasoningBlockKindSignature,
							Index:     assignIndex(),
							Signature: part.ThoughtSignature,
							Provider:  "gemini",
						})
					}
				}
				// Handle inline data (images, audio, etc.)
				if part.InlineData != nil {
					hasStructuredPart = true
					// Convert to data URL format: data:{mimeType};base64,{data}
					dataURL := fmt.Sprintf("data:%s;base64,%s", part.InlineData.MimeType, part.InlineData.Data)
					contentParts = append(contentParts, model.MessageContentPart{
						Type: "image_url",
						ImageURL: &model.ImageURL{
							URL: dataURL,
						},
					})
				}
				if part.FunctionCall != nil {
					argsJSON, _ := json.Marshal(part.FunctionCall.Args)
					toolCallID := fmt.Sprintf("call_%s_%d", part.FunctionCall.Name, idx)
					toolCall := model.ToolCall{
						Index: idx,
						ID:    toolCallID,
						Type:  "function",
						Function: model.FunctionCall{
							Name:      part.FunctionCall.Name,
							Arguments: string(argsJSON),
						},
						ThoughtSignature: part.ThoughtSignature,
					}
					toolCalls = append(toolCalls, toolCall)
					if part.ThoughtSignature != "" {
						// Anchor signatures to their originating functionCall so
						// the outbound replay can reconstruct the mapping by
						// name (G-H7) instead of relying on ordinal position —
						// multi-tool turns otherwise swap signatures and Gemini
						// rejects the request with 400.
						reasoningBlocks = append(reasoningBlocks, model.ReasoningBlock{
							Kind:         model.ReasoningBlockKindSignature,
							Index:        assignIndex(),
							Signature:    part.ThoughtSignature,
							Provider:     "gemini",
							ToolCallID:   toolCallID,
							ToolCallName: part.FunctionCall.Name,
						})
					}
				}

				// ExecutableCode / CodeExecutionResult (G-H9): Gemini emits
				// these parts when the sandboxed code_execution tool runs.
				// We fold them into the cross-provider ServerToolUse /
				// ServerToolResult envelopes with BlockType="code_execution"
				// so the existing passthrough infrastructure (P1.1) carries
				// them through. ID ties use→result for clients that care.
				if part.ExecutableCode != nil {
					input, _ := json.Marshal(map[string]any{
						"language": part.ExecutableCode.Language,
						"code":     part.ExecutableCode.Code,
					})
					codeID := fmt.Sprintf("gemini_code_exec_%d", idx)
					hasStructuredPart = true
					contentParts = append(contentParts, model.MessageContentPart{
						Type: "server_tool_use",
						ServerToolUse: &model.ServerToolUseBlock{
							ID:    codeID,
							Name:  "code_execution",
							Input: input,
						},
					})
				}
				if part.CodeExecutionResult != nil {
					resultPayload, _ := json.Marshal(map[string]any{
						"outcome": part.CodeExecutionResult.Outcome,
						"output":  part.CodeExecutionResult.Output,
					})
					isError := part.CodeExecutionResult.Outcome != "" &&
						part.CodeExecutionResult.Outcome != "OUTCOME_OK" &&
						part.CodeExecutionResult.Outcome != "OUTCOME_UNSPECIFIED"
					hasStructuredPart = true
					contentParts = append(contentParts, model.MessageContentPart{
						Type: "server_tool_result",
						ServerToolResult: &model.ServerToolResultBlock{
							Content:   resultPayload,
							IsError:   &isError,
							BlockType: "code_execution_tool_result",
						},
					})
				}
			}

			// Set content - use MultipleContent if we have any structured
			// parts (inline data or server-tool blocks), otherwise fall back
			// to the scalar string form. G-H9 widened this from
			// hasInlineData-only so code_execution parts aren't dropped.
			//
			// Text parts are also already appended to contentParts in order
			// (see the text branch above), so the multipart path preserves
			// the full sequence.
			if hasStructuredPart {
				msg.Content = model.MessageContent{
					MultipleContent: contentParts,
				}
			} else if len(textParts) > 0 {
				text := strings.Join(textParts, "")
				msg.Content = model.MessageContent{
					Content: &text,
				}
			}

			// Set reasoning content
			if reasoningContent != nil {
				msg.ReasoningContent = reasoningContent
			}

			// Preserve Gemini thoughtSignatures in the order they arrived so outbound can
			// replay them verbatim on the next turn (mandatory for Gemini 3 function calls).
			if len(reasoningBlocks) > 0 {
				msg.ReasoningBlocks = reasoningBlocks
				logGeminiSignatureAudit("extract", reasoningBlocks)
			}

			// Set tool calls
			if len(toolCalls) > 0 {
				msg.ToolCalls = toolCalls
				if choice.FinishReason == nil {
					reason := "tool_calls"
					choice.FinishReason = &reason
				}
			}

			if isStream {
				choice.Delta = msg
			} else {
				choice.Message = msg
			}
		}

		// Grounding / citations / URL context / safety ratings (G-H10, G-M9).
		// Populated on the Choice directly so consumers can surface them
		// without parsing the provider-native payload.
		if g := convertGeminiGroundingToInternal(candidate.GroundingMetadata); g != nil {
			choice.Grounding = g
		}
		if cites := convertGeminiCitationsToInternal(candidate.CitationMetadata); len(cites) > 0 {
			choice.Citations = cites
		}
		if u := convertGeminiURLContextToInternal(candidate.UrlContextMetadata); u != nil {
			choice.URLContext = u
		}
		if ratings := convertGeminiSafetyRatings(candidate.SafetyRatings); len(ratings) > 0 {
			choice.SafetyRatings = ratings
		}

		resp.Choices = append(resp.Choices, choice)
	}

	// Convert usage metadata
	if geminiResp.UsageMetadata != nil {
		usage := &model.Usage{
			PromptTokens:     int64(geminiResp.UsageMetadata.PromptTokenCount),
			CompletionTokens: int64(geminiResp.UsageMetadata.CandidatesTokenCount),
			TotalTokens:      int64(geminiResp.UsageMetadata.TotalTokenCount),
		}

		// Add cached tokens to prompt tokens details if present
		if geminiResp.UsageMetadata.CachedContentTokenCount > 0 {
			if usage.PromptTokensDetails == nil {
				usage.PromptTokensDetails = &model.PromptTokensDetails{}
			}
			usage.PromptTokensDetails.CachedTokens = int64(geminiResp.UsageMetadata.CachedContentTokenCount)
		}

		// Add thoughts tokens to completion tokens details if present
		if geminiResp.UsageMetadata.ThoughtsTokenCount > 0 {
			if usage.CompletionTokensDetails == nil {
				usage.CompletionTokensDetails = &model.CompletionTokensDetails{}
			}
			usage.CompletionTokensDetails.ReasoningTokens = int64(geminiResp.UsageMetadata.ThoughtsTokenCount)
		}

		if geminiResp.UsageMetadata.ToolUsePromptTokenCount > 0 {
			usage.ToolUsePromptTokens = int64(geminiResp.UsageMetadata.ToolUsePromptTokenCount)
		}

		if len(geminiResp.UsageMetadata.PromptTokensDetails) > 0 {
			if usage.PromptTokensDetails == nil {
				usage.PromptTokensDetails = &model.PromptTokensDetails{}
			}
			applyGeminiModalityToPromptDetails(usage.PromptTokensDetails, geminiResp.UsageMetadata.PromptTokensDetails)
			usage.PromptModalityTokenDetails = toInternalModalityCounts(geminiResp.UsageMetadata.PromptTokensDetails)
		}

		if len(geminiResp.UsageMetadata.CandidatesTokensDetails) > 0 {
			if usage.CompletionTokensDetails == nil {
				usage.CompletionTokensDetails = &model.CompletionTokensDetails{}
			}
			applyGeminiModalityToCompletionDetails(usage.CompletionTokensDetails, geminiResp.UsageMetadata.CandidatesTokensDetails)
			usage.CompletionModalityTokenDetails = toInternalModalityCounts(geminiResp.UsageMetadata.CandidatesTokensDetails)
		}

		resp.Usage = usage
	}

	// When the prompt is blocked Gemini returns no candidates, only
	// promptFeedback.blockReason. Surface a synthetic choice so downstream
	// inbounds can translate to a proper content_filter / refusal finish
	// reason instead of returning an empty 200.
	if len(geminiResp.Candidates) == 0 && geminiResp.PromptFeedback != nil && geminiResp.PromptFeedback.BlockReason != "" {
		reason := model.FinishReasonFromGemini(geminiResp.PromptFeedback.BlockReason).String()
		if reason == "" {
			reason = string(model.FinishReasonContentFilter)
		}
		synthetic := model.Choice{
			Index:        0,
			Message:      &model.Message{Role: "assistant"},
			FinishReason: &reason,
		}
		// Promote promptFeedback.safetyRatings onto the synthetic choice so
		// the reason for the block is discoverable downstream. G-M9.
		if ratings := convertGeminiSafetyRatings(geminiResp.PromptFeedback.SafetyRatings); len(ratings) > 0 {
			synthetic.SafetyRatings = ratings
		}
		resp.Choices = append(resp.Choices, synthetic)
	}

	return resp
}

// convertGeminiGroundingToInternal maps Gemini's groundingMetadata response
// block onto the cross-provider GroundingInfo shape. Returns nil when the
// upstream payload is empty so the caller can skip assignment. G-H10.
func convertGeminiGroundingToInternal(md *model.GeminiGroundingMetadata) *model.GroundingInfo {
	if md == nil {
		return nil
	}
	info := &model.GroundingInfo{
		SearchQueries: md.WebSearchQueries,
	}
	if md.SearchEntryPoint != nil {
		info.SearchEntryPointHTML = md.SearchEntryPoint.RenderedContent
	}
	if len(md.GroundingChunks) > 0 {
		sources := make([]model.GroundingSource, 0, len(md.GroundingChunks))
		for _, c := range md.GroundingChunks {
			if c == nil || c.Web == nil {
				// Skip unknown chunk shapes — only "web" is modelled.
				sources = append(sources, model.GroundingSource{})
				continue
			}
			sources = append(sources, model.GroundingSource{
				URI:     c.Web.URI,
				Title:   c.Web.Title,
				Snippet: c.Web.Snippet,
			})
		}
		info.Sources = sources
	}
	if len(md.GroundingSupports) > 0 {
		supports := make([]model.GroundingSupport, 0, len(md.GroundingSupports))
		for _, s := range md.GroundingSupports {
			if s == nil {
				continue
			}
			gs := model.GroundingSupport{
				SourceIndices:    s.GroundingChunkIndices,
				ConfidenceScores: s.ConfidenceScores,
			}
			if s.Segment != nil {
				gs.SegmentStartIndex = s.Segment.StartIndex
				gs.SegmentEndIndex = s.Segment.EndIndex
				gs.SegmentText = s.Segment.Text
			}
			supports = append(supports, gs)
		}
		info.Supports = supports
	}
	// If literally nothing was populated, treat as absent so the caller can
	// leave Choice.Grounding nil.
	if len(info.SearchQueries) == 0 && len(info.Sources) == 0 && len(info.Supports) == 0 && info.SearchEntryPointHTML == "" {
		return nil
	}
	return info
}

// convertGeminiCitationsToInternal maps Gemini's citationMetadata response
// block onto the cross-provider []Citation shape. G-H10.
func convertGeminiCitationsToInternal(md *model.GeminiCitationMetadata) []model.Citation {
	if md == nil || len(md.CitationSources) == 0 {
		return nil
	}
	out := make([]model.Citation, 0, len(md.CitationSources))
	for _, src := range md.CitationSources {
		if src == nil {
			continue
		}
		out = append(out, model.Citation{
			StartIndex: src.StartIndex,
			EndIndex:   src.EndIndex,
			URI:        src.URI,
			Title:      src.Title,
			License:    src.License,
		})
	}
	return out
}

// convertGeminiURLContextToInternal maps Gemini's urlContextMetadata
// response block onto the cross-provider URLContextInfo shape. G-H10.
func convertGeminiURLContextToInternal(md *model.GeminiUrlContextMetadata) *model.URLContextInfo {
	if md == nil || len(md.URLMetadata) == 0 {
		return nil
	}
	entries := make([]model.URLContextEntry, 0, len(md.URLMetadata))
	for _, u := range md.URLMetadata {
		if u == nil {
			continue
		}
		url := u.RetrievedURL
		if url == "" {
			url = u.URL
		}
		entries = append(entries, model.URLContextEntry{
			URL:    url,
			Status: u.URLRetrievalStatus,
		})
	}
	if len(entries) == 0 {
		return nil
	}
	return &model.URLContextInfo{URLs: entries}
}

// convertGeminiSafetyRatings maps Gemini's per-category safety evaluation
// onto the cross-provider SafetyRating shape. Returns nil for empty input
// so the caller can skip assignment. G-M9.
func convertGeminiSafetyRatings(raw []*model.GeminiSafetyRating) []model.SafetyRating {
	if len(raw) == 0 {
		return nil
	}
	out := make([]model.SafetyRating, 0, len(raw))
	for _, r := range raw {
		if r == nil {
			continue
		}
		out = append(out, model.SafetyRating{
			Category:    r.Category,
			Probability: r.Probability,
			Blocked:     r.Blocked,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// applyGeminiModalityToPromptDetails folds per-modality Gemini breakdowns into
// the internal PromptTokensDetails (TEXT/IMAGE/VIDEO/AUDIO/DOCUMENT).
func applyGeminiModalityToPromptDetails(details *model.PromptTokensDetails, counts []model.GeminiModalityTokenCount) {
	if details == nil {
		return
	}
	for _, mt := range counts {
		switch strings.ToUpper(mt.Modality) {
		case "TEXT":
			details.TextTokens += int64(mt.TokenCount)
		case "IMAGE":
			details.ImageTokens += int64(mt.TokenCount)
		case "VIDEO":
			details.VideoTokens += int64(mt.TokenCount)
		case "AUDIO":
			details.AudioTokens += int64(mt.TokenCount)
		case "DOCUMENT":
			details.DocumentTokens += int64(mt.TokenCount)
		}
	}
}

// applyGeminiModalityToCompletionDetails folds per-modality Gemini breakdowns
// into the internal CompletionTokensDetails.
func applyGeminiModalityToCompletionDetails(details *model.CompletionTokensDetails, counts []model.GeminiModalityTokenCount) {
	if details == nil {
		return
	}
	for _, mt := range counts {
		switch strings.ToUpper(mt.Modality) {
		case "TEXT":
			details.TextTokens += int64(mt.TokenCount)
		case "IMAGE":
			details.ImageTokens += int64(mt.TokenCount)
		case "VIDEO":
			details.VideoTokens += int64(mt.TokenCount)
		case "AUDIO":
			details.AudioTokens += int64(mt.TokenCount)
		}
	}
}

func toInternalModalityCounts(counts []model.GeminiModalityTokenCount) []model.ModalityTokenCount {
	if len(counts) == 0 {
		return nil
	}
	result := make([]model.ModalityTokenCount, 0, len(counts))
	for _, mt := range counts {
		result = append(result, model.ModalityTokenCount{
			Modality:   mt.Modality,
			TokenCount: int64(mt.TokenCount),
		})
	}
	return result
}

// convertGeminiFinishReason maps a Gemini finishReason to the canonical
// FinishReason wire string. Unlike the previous hardcoded switch, this
// preserves provider-specific reasons (BLOCKLIST / PROHIBITED_CONTENT / SPII /
// MALFORMED_FUNCTION_CALL / IMAGE_SAFETY / LANGUAGE / OTHER) instead of
// collapsing them to "stop".
func convertGeminiFinishReason(reason string) string {
	return model.FinishReasonFromGemini(reason).String()
}

func cleanGeminiSchema(schema map[string]any) {
	if schema == nil {
		return
	}
	t := &geminiSchemaTransformer{
		root:    schema,
		visited: map[uintptr]struct{}{},
	}
	t.transform(schema)
}

type geminiSchemaTransformer struct {
	root    map[string]any
	visited map[uintptr]struct{}
}

func (t *geminiSchemaTransformer) transform(schemaNode any) {
	if schemaNode == nil {
		return
	}

	// Cycle guard: schema graphs can contain shared sub-objects (or be cyclic after merges).
	// We only track reference-like kinds to avoid false positives.
	rv := reflect.ValueOf(schemaNode)
	switch rv.Kind() {
	case reflect.Map, reflect.Slice, reflect.Pointer:
		if rv.IsNil() {
			return
		}
		id := rv.Pointer()
		if _, seen := t.visited[id]; seen {
			return
		}
		t.visited[id] = struct{}{}
	}

	switch node := schemaNode.(type) {
	case []any:
		for _, item := range node {
			t.transform(item)
		}
		return

	case map[string]any:
		// 1) Resolve $ref (local-only: #/...)
		if ref, ok := node["$ref"].(string); ok && strings.HasPrefix(ref, "#/") {
			path := strings.Split(ref[2:], "/")
			var cur any = t.root
			for _, seg := range path {
				seg = strings.ReplaceAll(seg, "~1", "/")
				seg = strings.ReplaceAll(seg, "~0", "~")
				m, ok := cur.(map[string]any)
				if !ok {
					cur = nil
					break
				}
				cur = m[seg]
				if cur == nil {
					break
				}
			}

			if resolved, ok := cur.(map[string]any); ok && resolved != nil {
				// Merge resolved schema into node, but keep local overrides in node.
				overlay := make(map[string]any, len(node))
				for k, v := range node {
					if k != "$ref" {
						overlay[k] = v
					}
				}

				var copied map[string]any
				if b, err := json.Marshal(resolved); err == nil {
					if err := json.Unmarshal(b, &copied); err != nil {
						log.Warnf("gemini: failed to deep-copy resolved schema: %v", err)
					}
				}
				if copied == nil {
					copied = make(map[string]any, len(resolved))
					for k, v := range resolved {
						copied[k] = v
					}
				}

				for k := range node {
					delete(node, k)
				}
				for k, v := range copied {
					node[k] = v
				}
				for k, v := range overlay {
					node[k] = v
				}
				delete(node, "$ref")
			}
		}

		// 2) Merge allOf into current node
		if allOf, ok := node["allOf"].([]any); ok {
			for _, item := range allOf {
				t.transform(item)
				itemMap, ok := item.(map[string]any)
				if !ok {
					continue
				}

				// Merge properties (existing props win)
				if itemProps, ok := itemMap["properties"].(map[string]any); ok {
					props, _ := node["properties"].(map[string]any)
					if props == nil {
						props = map[string]any{}
					}
					for k, v := range itemProps {
						if _, exists := props[k]; !exists {
							props[k] = v
						}
					}
					node["properties"] = props
				}

				// Merge required
				itemReq := t.asStringSlice(itemMap["required"])
				if len(itemReq) > 0 {
					curReq := t.asStringSlice(node["required"])
					curReq = append(curReq, itemReq...)
					node["required"] = t.dedupeStrings(curReq)
				}
			}
			delete(node, "allOf")
		}

		// 3) Type mapping (and nullable union handling)
		if typ, ok := node["type"]; ok {
			primary := ""
			switch v := typ.(type) {
			case string:
				primary = v
			case []any:
				for _, it := range v {
					if s, ok := it.(string); ok && strings.ToLower(s) != "null" {
						primary = s
						break
					}
				}
			case []string:
				for _, s := range v {
					if strings.ToLower(s) != "null" {
						primary = s
						break
					}
				}
			}

			switch strings.ToLower(primary) {
			case "string":
				node["type"] = "STRING"
			case "number":
				node["type"] = "NUMBER"
			case "integer":
				node["type"] = "INTEGER"
			case "boolean":
				node["type"] = "BOOLEAN"
			case "array":
				node["type"] = "ARRAY"
			case "object":
				node["type"] = "OBJECT"
			}
		}

		// 4) ARRAY items fixes + tuple handling
		if node["type"] == "ARRAY" {
			if node["items"] == nil {
				node["items"] = map[string]any{}
			} else if tuple, ok := node["items"].([]any); ok {
				for _, it := range tuple {
					t.transform(it)
				}

				// Add tuple hint to description
				tupleTypes := make([]string, 0, len(tuple))
				for _, it := range tuple {
					if itMap, ok := it.(map[string]any); ok {
						if tt, ok := itMap["type"].(string); ok && tt != "" {
							tupleTypes = append(tupleTypes, tt)
						} else {
							tupleTypes = append(tupleTypes, "any")
						}
					} else {
						tupleTypes = append(tupleTypes, "any")
					}
				}
				hint := fmt.Sprintf("(Tuple: [%s])", strings.Join(tupleTypes, ", "))
				if origDesc, _ := node["description"].(string); origDesc == "" {
					node["description"] = hint
				} else {
					node["description"] = strings.TrimSpace(origDesc + " " + hint)
				}

				// Homogeneous tuple => collapse to list schema; otherwise loosen.
				firstType := ""
				if len(tuple) > 0 {
					if itMap, ok := tuple[0].(map[string]any); ok {
						firstType, _ = itMap["type"].(string)
					}
				}
				isHomogeneous := firstType != ""
				for _, it := range tuple {
					itMap, ok := it.(map[string]any)
					if !ok {
						isHomogeneous = false
						break
					}
					tt, _ := itMap["type"].(string)
					if tt != firstType {
						isHomogeneous = false
						break
					}
				}

				if isHomogeneous {
					node["items"] = tuple[0]
				} else {
					node["items"] = map[string]any{}
				}
			}
		}

		// 5) anyOf: try const->enum; otherwise take first usable schema if no type set
		if anyOf, ok := node["anyOf"].([]any); ok {
			for _, item := range anyOf {
				t.transform(item)
			}

			allConst := true
			enumVals := make([]string, 0, len(anyOf))
			for _, item := range anyOf {
				itemMap, ok := item.(map[string]any)
				if !ok {
					allConst = false
					break
				}
				c, ok := itemMap["const"]
				if !ok {
					allConst = false
					break
				}
				if c == nil || c == "" {
					continue
				}
				enumVals = append(enumVals, fmt.Sprint(c))
			}

			if allConst && len(enumVals) > 0 {
				node["type"] = "STRING"
				node["enum"] = enumVals
			} else if _, hasType := node["type"]; !hasType {
				for _, item := range anyOf {
					if itemMap, ok := item.(map[string]any); ok {
						if itemMap["type"] != nil || itemMap["enum"] != nil {
							for k, v := range itemMap {
								node[k] = v
							}
							break
						}
					}
				}
			}
			delete(node, "anyOf")
		}

		// 6) Default value -> description hint (then delete default)
		if def, ok := node["default"]; ok {
			if desc, ok := node["description"].(string); ok && desc != "" {
				if b, err := json.Marshal(def); err == nil {
					node["description"] = desc + " (Default: " + string(b) + ")"
				}
			}
		}

		// 7) Remove unsupported fields
		for _, k := range []string{
			"title", "$schema", "$ref", "strict",
			"exclusiveMaximum", "exclusiveMinimum",
			"additionalProperties", "oneOf", "default",
			"propertyNames",
			"$defs",
		} {
			delete(node, k)
		}

		// 8) Recurse into properties/items
		if props, ok := node["properties"].(map[string]any); ok {
			for _, prop := range props {
				t.transform(prop)
			}
		}
		if items := node["items"]; items != nil {
			t.transform(items)
		}

		// 9) Ensure required is de-duped (allOf merge can introduce duplicates)
		if req := t.asStringSlice(node["required"]); len(req) > 0 {
			node["required"] = t.dedupeStrings(req)
		}
	}
}

func (t *geminiSchemaTransformer) asStringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return append([]string(nil), s...)
	case []any:
		out := make([]string, 0, len(s))
		for _, it := range s {
			if str, ok := it.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

func (t *geminiSchemaTransformer) dedupeStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
