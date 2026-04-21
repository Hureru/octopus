package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/samber/lo"

	anthropicModel "github.com/bestruirui/octopus/internal/transformer/inbound/anthropic"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/xurl"
)

type MessageOutbound struct {
	// Stream state tracking
	streamID    string
	streamModel string
	streamUsage *model.Usage
	toolIndex   int
	toolCalls   map[int]*model.ToolCall
	initialized bool
}

func (o *MessageOutbound) TransformRequest(ctx context.Context, request *model.InternalLLMRequest, baseUrl, key string) (*http.Request, error) {
	if request == nil {
		return nil, fmt.Errorf("request is nil")
	}

	request.NormalizeMessages()
	request.EnforceMessageAlternation(model.AlternationProviderAnthropic)

	// Convert to Anthropic request format
	anthropicReq := convertToAnthropicRequest(request)

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal anthropic request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	// For streaming requests, Anthropic returns Server-Sent Events.
	if request.Stream != nil && *request.Stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("Anthropic-Version", "2023-06-01")
	req.Header.Set("X-API-Key", key)
	if betas := collectAnthropicBetaHeaders(anthropicReq); len(betas) > 0 {
		req.Header.Set("anthropic-beta", strings.Join(betas, ","))
	}

	// Parse and set URL
	parsedUrl, err := url.Parse(strings.TrimSuffix(baseUrl, "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}

	parsedUrl.Path = parsedUrl.Path + "/messages"
	// Pass through the original query parameters exactly as-is
	if request.Query != nil {
		parsedUrl.RawQuery = request.Query.Encode()
	}
	req.URL = parsedUrl

	return req, nil
}

// TransformRequestRaw 把客户端原始 Anthropic 请求字节直接转发给上游，仅重写顶层 model 为
// 当前命中的实际上游模型，不做其他字段白名单解析。
// 用于 Anthropic → Anthropic 的同协议直通路径，保证 anthropic-beta 相关字段（context_management、
// betas 等）、内容块原始顺序、extended thinking 签名等信息尽量完整传递到上游。
//
// 仅设置上游必需的鉴权/URL；Accept、Content-Type、Anthropic-Version、anthropic-beta 等请求头由
// 上层 copyHeaders 从客户端透传（已被 hop-by-hop 过滤保护，x-api-key/authorization 不会覆盖）。
// 注意：为了 HTTP/2 与 401/429/5xx 重试时可以重放 body，同时设置 ContentLength 与 GetBody。
func (o *MessageOutbound) TransformRequestRaw(ctx context.Context, rawBody []byte, modelName, baseUrl, key string, query url.Values) (*http.Request, error) {
	if len(rawBody) == 0 {
		return nil, fmt.Errorf("raw body is empty")
	}
	if strings.TrimSpace(modelName) != "" {
		rewrittenBody, err := rewriteRawRequestModel(rawBody, modelName)
		if err != nil {
			return nil, err
		}
		rawBody = rewrittenBody
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", bytes.NewReader(rawBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.ContentLength = int64(len(rawBody))
	bodyBytes := rawBody
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bodyBytes)), nil
	}

	// 默认请求头：上层 copyHeaders 随后会用客户端真实值覆盖 Content-Type / Accept /
	// Anthropic-Version / anthropic-beta；x-api-key 与 authorization 被 hop-by-hop 过滤，
	// 因此 Set 的上游密钥不会被客户端请求头覆盖。
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Anthropic-Version", "2023-06-01")
	req.Header.Set("X-API-Key", key)

	parsedUrl, err := url.Parse(strings.TrimSuffix(baseUrl, "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}
	parsedUrl.Path = parsedUrl.Path + "/messages"
	if query != nil {
		parsedUrl.RawQuery = query.Encode()
	}
	req.URL = parsedUrl

	return req, nil
}

func rewriteRawRequestModel(rawBody []byte, modelName string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return nil, fmt.Errorf("failed to decode raw anthropic request: %w", err)
	}
	payload["model"] = strings.TrimSpace(modelName)
	rewrittenBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode raw anthropic request: %w", err)
	}
	return rewrittenBody, nil
}

func (o *MessageOutbound) TransformResponse(ctx context.Context, response *http.Response) (*model.InternalLLMResponse, error) {
	if response == nil {
		return nil, fmt.Errorf("response is nil")
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if len(body) == 0 {
		return nil, fmt.Errorf("response body is empty")
	}

	// Check for error response
	if response.StatusCode >= 400 {
		var errResp anthropicModel.AnthropicError
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
			if strings.Contains(strings.ToLower(errResp.Error.Message), "signature") {
				log.Warnw("transformer.reasoning.signature.passthrough",
					"provider", "anthropic",
					"direction", "error",
					"status_code", response.StatusCode,
					"error_type", errResp.Error.Type,
					"error_message", truncateForAudit(errResp.Error.Message, 256),
				)
			}
			return nil, &model.ResponseError{
				StatusCode: response.StatusCode,
				Detail: model.ErrorDetail{
					Message: errResp.Error.Message,
					Type:    errResp.Error.Type,
				},
			}
		}
		return nil, fmt.Errorf("HTTP error %d: %s", response.StatusCode, string(body))
	}

	var anthropicResp anthropicModel.Message
	if err := json.Unmarshal(body, &anthropicResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal anthropic response: %w", err)
	}

	// Convert to internal response
	return convertToLLMResponse(&anthropicResp), nil
}

func (o *MessageOutbound) TransformStream(ctx context.Context, eventData []byte) (*model.InternalLLMResponse, error) {
	if len(eventData) == 0 {
		return nil, nil
	}

	// Handle [DONE] marker
	if bytes.HasPrefix(eventData, []byte("[DONE]")) {
		return &model.InternalLLMResponse{
			Object: "[DONE]",
		}, nil
	}

	// Initialize state if needed
	if !o.initialized {
		o.toolCalls = make(map[int]*model.ToolCall)
		o.toolIndex = -1
		o.initialized = true
	}

	// Parse the streaming event
	var streamEvent anthropicModel.StreamEvent
	if err := json.Unmarshal(eventData, &streamEvent); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stream event: %w", err)
	}

	resp := &model.InternalLLMResponse{
		ID:      o.streamID,
		Model:   o.streamModel,
		Object:  "chat.completion.chunk",
		Created: 0,
	}

	switch streamEvent.Type {
	case "message_start":
		if streamEvent.Message != nil {
			o.streamID = streamEvent.Message.ID
			o.streamModel = streamEvent.Message.Model
			resp.ID = o.streamID
			resp.Model = o.streamModel

			if streamEvent.Message.Usage != nil &&
				(streamEvent.Message.Usage.InputTokens > 0 ||
					streamEvent.Message.Usage.OutputTokens > 0 ||
					streamEvent.Message.Usage.CacheReadInputTokens > 0 ||
					streamEvent.Message.Usage.CacheCreationInputTokens > 0) {
				o.streamUsage = convertAnthropicUsage(streamEvent.Message.Usage)
				resp.Usage = o.streamUsage
			}
		}

		resp.Choices = []model.Choice{
			{
				Index: 0,
				Delta: &model.Message{
					Role: "assistant",
				},
			},
		}

	case "content_block_start":
		if streamEvent.ContentBlock != nil {
			switch streamEvent.ContentBlock.Type {
			case "tool_use":
				o.toolIndex++
				toolCall := model.ToolCall{
					Index: o.toolIndex,
					ID:    streamEvent.ContentBlock.ID,
					Type:  "function",
					Function: model.FunctionCall{
						Name:      lo.FromPtr(streamEvent.ContentBlock.Name),
						Arguments: "",
					},
				}
				o.toolCalls[o.toolIndex] = &toolCall

				resp.Choices = []model.Choice{
					{
						Index: 0,
						Delta: &model.Message{
							Role:      "assistant",
							ToolCalls: []model.ToolCall{toolCall},
						},
					},
				}
			case "text", "thinking":
				// These are handled in content_block_delta
				return nil, nil
			case "redacted_thinking":
				// Pass through as a complete block (no delta)
				resp.Choices = []model.Choice{
					{
						Index: 0,
						Delta: &model.Message{
							Role:                   "assistant",
							RedactedThinkingBlocks: []string{streamEvent.ContentBlock.Data},
							ReasoningBlocks: []model.ReasoningBlock{{
								Kind:     model.ReasoningBlockKindRedacted,
								Index:    -1,
								Data:     streamEvent.ContentBlock.Data,
								Provider: "anthropic",
							}},
						},
					},
				}
			default:
				return nil, nil
			}
		}

	case "content_block_delta":
		if streamEvent.Delta != nil && streamEvent.Delta.Type != nil {
			choice := model.Choice{
				Index: 0,
				Delta: &model.Message{
					Role: "assistant",
				},
			}

			switch *streamEvent.Delta.Type {
			case "text_delta":
				if streamEvent.Delta.Text != nil {
					choice.Delta.Content = model.MessageContent{
						Content: streamEvent.Delta.Text,
					}
				}
			case "input_json_delta":
				if streamEvent.Delta.PartialJSON != nil && o.toolIndex >= 0 {
					choice.Delta.ToolCalls = []model.ToolCall{
						{
							Index: o.toolIndex,
							ID:    o.toolCalls[o.toolIndex].ID,
							Type:  "function",
							Function: model.FunctionCall{
								Arguments: *streamEvent.Delta.PartialJSON,
							},
						},
					}
				}
			case "thinking_delta":
				if streamEvent.Delta.Thinking != nil {
					choice.Delta.ReasoningContent = streamEvent.Delta.Thinking
				}
			case "signature_delta":
				if streamEvent.Delta.Signature != nil {
					choice.Delta.ReasoningSignature = streamEvent.Delta.Signature
					// Emit a standalone signature block so downstream aggregators can attach it
					// to the correct thinking block even when multiple thinking blocks exist.
					choice.Delta.ReasoningBlocks = []model.ReasoningBlock{{
						Kind:      model.ReasoningBlockKindSignature,
						Index:     -1,
						Signature: *streamEvent.Delta.Signature,
						Provider:  "anthropic",
					}}
				}
			default:
				return nil, nil
			}

			resp.Choices = []model.Choice{choice}
		}

	case "message_delta":
		if streamEvent.Usage != nil {
			usage := convertAnthropicUsage(streamEvent.Usage)
			if o.streamUsage != nil {
				usage.PromptTokens = o.streamUsage.PromptTokens
				usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
			}
			o.streamUsage = usage
		}

		if streamEvent.Delta != nil && streamEvent.Delta.StopReason != nil {
			finishReason := convertStopReason(streamEvent.Delta.StopReason)
			resp.Choices = []model.Choice{
				{
					Index:        0,
					FinishReason: finishReason,
				},
			}
		}

	case "message_stop":
		resp.Choices = []model.Choice{}
		if o.streamUsage != nil {
			resp.Usage = o.streamUsage
		}

	case "content_block_stop", "ping":
		return nil, nil

	case "error":
		if streamEvent.Error == nil {
			return nil, nil
		}
		resp.Error = &model.ResponseError{
			StatusCode: mapAnthropicErrorTypeToStatus(streamEvent.Error.Type),
			Detail: model.ErrorDetail{
				Type:    streamEvent.Error.Type,
				Message: streamEvent.Error.Message,
			},
		}
		resp.Choices = nil

	default:
		return nil, nil
	}

	return resp, nil
}

// convertToAnthropicRequest converts internal LLM request to Anthropic format
func convertToAnthropicRequest(req *model.InternalLLMRequest) *anthropicModel.MessageRequest {
	result := &anthropicModel.MessageRequest{
		Model:       req.Model,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
		MaxTokens:   resolveMaxTokens(req),
		System:      convertSystemPrompt(req),
	}

	if userID := resolveAnthropicUserID(req); userID != "" {
		result.Metadata = &anthropicModel.AnthropicMetadata{UserID: userID}
	}

	// Convert messages
	result.Messages = convertMessages(req)

	// Convert tools
	if len(req.Tools) > 0 {
		result.Tools = convertTools(req.Tools)
	}

	// Convert stop sequences
	if req.Stop != nil {
		result.StopSequences = convertStopSequences(req.Stop)
	}

	// Convert thinking/reasoning
	if req.ReasoningEffort != "" {
		if req.AdaptiveThinking {
			result.Thinking = &anthropicModel.Thinking{
				Type:    anthropicModel.ThinkingTypeAdaptive,
				Display: req.ThinkingDisplay,
			}
			result.OutputConfig = &anthropicModel.OutputConfig{
				Effort: req.ReasoningEffort,
			}
		} else {
			result.Thinking = &anthropicModel.Thinking{
				Type:         anthropicModel.ThinkingTypeEnabled,
				BudgetTokens: getThinkingBudget(req.ReasoningEffort, req.ReasoningBudget),
				Display:      req.ThinkingDisplay,
			}
		}
	}

	// Convert tool choice
	if tc := convertToolChoice(req.ToolChoice); tc != nil {
		result.ToolChoice = tc
	}

	// Cap cache_control breakpoints to Anthropic's per-request ceiling. Excess markers are
	// silently dropped rather than surfacing a 400 — the request still succeeds, just without
	// caching on the trimmed blocks.
	pruneCacheBreakpoints(result)

	return result
}

func resolveAnthropicUserID(req *model.InternalLLMRequest) string {
	if req == nil {
		return ""
	}
	if req.Metadata != nil {
		if userID := strings.TrimSpace(req.Metadata["user_id"]); userID != "" {
			return userID
		}
	}
	if req.TransformerMetadata != nil {
		if userID := strings.TrimSpace(req.TransformerMetadata["anthropic_user_id"]); userID != "" {
			return userID
		}
	}
	if req.User != nil {
		return strings.TrimSpace(*req.User)
	}
	return ""
}

// convertToolChoice maps the internal ToolChoice into the Anthropic wire
// shape: {type, name?, disable_parallel_tool_use?}. The string form
// ("auto"/"none"/"required"/"any") is normalised into the Anthropic enum,
// and OpenAI-style {type:"function", function:{name}} is re-expressed as
// {type:"tool", name}. Anthropic's schema rejects unknown types, so we drop
// anything we can't translate rather than passing it through.
func convertToolChoice(tc *model.ToolChoice) *anthropicModel.ToolChoice {
	if tc == nil {
		return nil
	}
	if tc.ToolChoice != nil {
		switch strings.ToLower(*tc.ToolChoice) {
		case "auto":
			return &anthropicModel.ToolChoice{Type: "auto"}
		case "none":
			return &anthropicModel.ToolChoice{Type: "none"}
		case "required", "any":
			return &anthropicModel.ToolChoice{Type: "any"}
		default:
			return nil
		}
	}
	named := tc.NamedToolChoice
	if named == nil {
		return nil
	}
	out := &anthropicModel.ToolChoice{
		DisableParallelToolUse: named.DisableParallelToolUse,
	}
	switch strings.ToLower(named.Type) {
	case "auto":
		out.Type = "auto"
	case "any", "required":
		out.Type = "any"
	case "none":
		out.Type = "none"
	case "tool", "function":
		out.Type = "tool"
		if name := named.ResolvedFunctionName(); name != "" {
			n := name
			out.Name = &n
		} else {
			// tool type requires a name on Anthropic; without one the
			// request would 400. Fall back to auto so the request stays
			// valid.
			out.Type = "auto"
		}
	default:
		return nil
	}
	return out
}

func resolveMaxTokens(req *model.InternalLLMRequest) int64 {
	var maxtoken int64 = 1
	switch {
	case req.MaxTokens != nil:
		maxtoken = *req.MaxTokens
	case req.MaxCompletionTokens != nil:
		maxtoken = *req.MaxCompletionTokens
	default:
		maxtoken = 8192
	}
	if maxtoken < 1 {
		maxtoken = 1
	}
	return maxtoken
}

func convertSystemPrompt(req *model.InternalLLMRequest) *anthropicModel.SystemPrompt {
	var systemMessages []model.Message
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemMessages = append(systemMessages, msg)
		}
	}

	if len(systemMessages) == 0 {
		return nil
	}

	if len(systemMessages) == 1 {
		return &anthropicModel.SystemPrompt{
			MultiplePrompts: []anthropicModel.SystemPromptPart{{
				Type:         "text",
				Text:         lo.FromPtr(systemMessages[0].Content.Content),
				CacheControl: convertCacheControl(systemMessages[0].CacheControl),
			}},
		}
	}

	parts := make([]anthropicModel.SystemPromptPart, 0, len(systemMessages))
	for _, msg := range systemMessages {
		parts = append(parts, anthropicModel.SystemPromptPart{
			Type:         "text",
			Text:         lo.FromPtr(msg.Content.Content),
			CacheControl: convertCacheControl(msg.CacheControl),
		})
	}
	return &anthropicModel.SystemPrompt{
		MultiplePrompts: parts,
	}
}

func convertMessages(req *model.InternalLLMRequest) []anthropicModel.MessageParam {
	messages := make([]anthropicModel.MessageParam, 0, len(req.Messages))
	processedIndexes := make(map[int]bool)

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			continue
		}

		converted := convertSingleMessage(msg, req.Messages, processedIndexes)
		for _, convertedMsg := range converted {
			// Anthropic API 要求消息角色必须交替出现（user/assistant/user/assistant）。
			// 当 OpenAI 格式的多个连续 tool 消息被各自转换为独立的 user 消息时，
			// 会产生连续的同角色消息，需要合并以避免 "Improperly formed request" 错误。
			if n := len(messages); n > 0 && messages[n-1].Role == convertedMsg.Role {
				last := &messages[n-1]
				last.Content = anthropicModel.MessageContent{
					MultipleContent: append(contentToBlocks(last.Content), contentToBlocks(convertedMsg.Content)...),
				}
			} else {
				messages = append(messages, convertedMsg)
			}
		}
	}

	return messages
}

// contentToBlocks 将 MessageContent 统一展开为 MessageContentBlock 切片。
func contentToBlocks(c anthropicModel.MessageContent) []anthropicModel.MessageContentBlock {
	if len(c.MultipleContent) > 0 {
		// 返回副本，避免后续 append 污染原 slice
		return append([]anthropicModel.MessageContentBlock(nil), c.MultipleContent...)
	}
	if c.Content != nil && *c.Content != "" {
		return []anthropicModel.MessageContentBlock{{Type: "text", Text: c.Content}}
	}
	return nil
}

func convertSingleMessage(msg model.Message, allMessages []model.Message, processedIndexes map[int]bool) []anthropicModel.MessageParam {
	switch msg.Role {
	case "tool":
		return convertToolMessage(msg, allMessages, processedIndexes)
	case "user":
		if msg.MessageIndex != nil && processedIndexes[*msg.MessageIndex] {
			return nil
		}
		return convertUserMessage(msg)
	case "assistant":
		return convertAssistantMessage(msg)
	default:
		return nil
	}
}

func convertToolMessage(msg model.Message, allMessages []model.Message, processedIndexes map[int]bool) []anthropicModel.MessageParam {
	if msg.MessageIndex == nil {
		return []anthropicModel.MessageParam{{
			Role: "user",
			Content: anthropicModel.MessageContent{
				MultipleContent: []anthropicModel.MessageContentBlock{convertToolResultBlock(msg)},
			},
		}}
	}

	if processedIndexes[*msg.MessageIndex] {
		return nil
	}

	var toolMsgs []model.Message
	for _, m := range allMessages {
		if m.Role == "tool" && m.MessageIndex != nil && *m.MessageIndex == *msg.MessageIndex {
			toolMsgs = append(toolMsgs, m)
		}
	}

	if len(toolMsgs) == 0 {
		return nil
	}

	contentBlocks := make([]anthropicModel.MessageContentBlock, 0, len(toolMsgs))
	for _, tm := range toolMsgs {
		contentBlocks = append(contentBlocks, convertToolResultBlock(tm))
	}

	// Merge the associated user message content (if any) into the same Anthropic user message.
	// In Anthropic Messages, tool_result blocks live inside a user message's content array.
	// Our internal format represents tool results as separate "tool" role messages, but the
	// original Anthropic request may also include additional user content alongside tool_result.
	if userMsg := findUserMessageByIndex(allMessages, *msg.MessageIndex); userMsg != nil {
		userContent := buildMessageContent(*userMsg)
		contentBlocks = append(contentBlocks, contentToBlocks(userContent)...)
	}

	processedIndexes[*msg.MessageIndex] = true

	return []anthropicModel.MessageParam{{
		Role:    "user",
		Content: anthropicModel.MessageContent{MultipleContent: contentBlocks},
	}}
}

func findUserMessageByIndex(allMessages []model.Message, messageIndex int) *model.Message {
	for i := range allMessages {
		m := &allMessages[i]
		if m.Role == "user" && m.MessageIndex != nil && *m.MessageIndex == messageIndex {
			return m
		}
	}
	return nil
}

func convertToolResultBlock(msg model.Message) anthropicModel.MessageContentBlock {
	block := anthropicModel.MessageContentBlock{
		Type:         "tool_result",
		ToolUseID:    msg.ToolCallID,
		CacheControl: convertCacheControl(msg.CacheControl),
		IsError:      msg.ToolCallIsError,
	}

	if msg.Content.Content != nil {
		block.Content = &anthropicModel.MessageContent{
			Content: msg.Content.Content,
		}
	} else if len(msg.Content.MultipleContent) > 0 {
		blocks := make([]anthropicModel.MessageContentBlock, 0, len(msg.Content.MultipleContent))
		for _, part := range msg.Content.MultipleContent {
			if part.Type == "text" && part.Text != nil {
				blocks = append(blocks, anthropicModel.MessageContentBlock{
					Type: "text",
					Text: part.Text,
				})
			}
		}
		block.Content = &anthropicModel.MessageContent{
			MultipleContent: blocks,
		}
	}

	return block
}

func convertUserMessage(msg model.Message) []anthropicModel.MessageParam {
	content := buildMessageContent(msg)
	return []anthropicModel.MessageParam{{Role: "user", Content: content}}
}

func convertAssistantMessage(msg model.Message) []anthropicModel.MessageParam {
	if len(msg.ToolCalls) > 0 {
		return convertAssistantWithToolCalls(msg)
	}

	content := buildMessageContent(msg)
	return []anthropicModel.MessageParam{{Role: "assistant", Content: content}}
}

func convertAssistantWithToolCalls(msg model.Message) []anthropicModel.MessageParam {
	var blocks []anthropicModel.MessageContentBlock

	// Thinking + redacted_thinking blocks, emitted in their original order so Anthropic
	// multi-turn signature verification does not fail on interleaved blocks.
	blocks = append(blocks, emitThinkingBlocks(msg)...)

	// Add text content if present
	if msg.Content.Content != nil && *msg.Content.Content != "" {
		blocks = append(blocks, anthropicModel.MessageContentBlock{
			Type:         "text",
			Text:         msg.Content.Content,
			CacheControl: convertCacheControl(msg.CacheControl),
		})
	} else if len(msg.Content.MultipleContent) > 0 {
		for _, part := range msg.Content.MultipleContent {
			if part.Type == "text" && part.Text != nil {
				blocks = append(blocks, anthropicModel.MessageContentBlock{
					Type:         "text",
					Text:         part.Text,
					CacheControl: convertCacheControl(part.CacheControl),
				})
			}
		}
	}

	// Add tool calls
	for _, toolCall := range msg.ToolCalls {
		input := json.RawMessage("{}")
		if toolCall.Function.Arguments != "" {
			if json.Valid([]byte(toolCall.Function.Arguments)) {
				input = json.RawMessage(toolCall.Function.Arguments)
			}
		}
		blocks = append(blocks, anthropicModel.MessageContentBlock{
			Type:         "tool_use",
			ID:           toolCall.ID,
			Name:         &toolCall.Function.Name,
			Input:        input,
			CacheControl: convertCacheControl(toolCall.CacheControl),
		})
	}

	if len(blocks) == 0 {
		return nil
	}

	return []anthropicModel.MessageParam{{
		Role:    "assistant",
		Content: anthropicModel.MessageContent{MultipleContent: blocks},
	}}
}

func buildMessageContent(msg model.Message) anthropicModel.MessageContent {
	// Handle simple string content
	if msg.Content.Content != nil {
		if msg.CacheControl != nil || hasThinkingContent(msg) {
			return buildMultipleContentWithThinking(msg)
		}
		return anthropicModel.MessageContent{Content: msg.Content.Content}
	}

	// Handle multiple content parts
	if len(msg.Content.MultipleContent) > 0 {
		return convertMultiplePartContent(msg)
	}

	// Handle reasoning-only messages (no text content, but has thinking/redacted thinking)
	if hasThinkingContent(msg) || len(msg.RedactedThinkingBlocks) > 0 || len(msg.ReasoningBlocks) > 0 {
		return buildMultipleContentWithThinking(msg)
	}

	return anthropicModel.MessageContent{}
}

func hasThinkingContent(msg model.Message) bool {
	if msg.ReasoningContent != nil && *msg.ReasoningContent != "" {
		return true
	}
	for _, rb := range msg.ReasoningBlocks {
		if rb.Kind == model.ReasoningBlockKindThinking && (rb.Text != "" || rb.Signature != "") {
			return true
		}
	}
	return false
}

// emitThinkingBlocks reproduces Anthropic thinking / redacted_thinking blocks in their original
// order so multi-turn extended-thinking requests pass signature verification. It prefers the
// per-block ReasoningBlocks representation; when absent (e.g. the upstream was OpenRouter or
// the turn predates this refactor), it falls back to the flat ReasoningContent/Signature pair.
func emitThinkingBlocks(msg model.Message) []anthropicModel.MessageContentBlock {
	anthropicBlocks := msg.ReasoningBlocksByProvider("anthropic")
	if len(anthropicBlocks) == 0 {
		// Some callers (e.g. Anthropic inbound parsed v1) may have populated ReasoningBlocks
		// without tagging Provider. Treat untagged blocks as Anthropic as a safety net.
		for _, rb := range msg.ReasoningBlocks {
			if rb.Provider == "" {
				anthropicBlocks = append(anthropicBlocks, rb)
			}
		}
	}

	if len(anthropicBlocks) == 0 {
		return emitThinkingBlocksLegacy(msg)
	}

	out := make([]anthropicModel.MessageContentBlock, 0, len(anthropicBlocks))
	// signature-only blocks attach to the most recent thinking block.
	var lastThinking *anthropicModel.MessageContentBlock
	for _, rb := range anthropicBlocks {
		switch rb.Kind {
		case model.ReasoningBlockKindThinking:
			block := anthropicModel.MessageContentBlock{Type: "thinking"}
			if rb.Text != "" {
				t := rb.Text
				block.Thinking = &t
			}
			if rb.Signature != "" {
				s := rb.Signature
				block.Signature = &s
			}
			out = append(out, block)
			lastThinking = &out[len(out)-1]
		case model.ReasoningBlockKindRedacted:
			if rb.Data != "" {
				out = append(out, anthropicModel.MessageContentBlock{
					Type: "redacted_thinking",
					Data: rb.Data,
				})
				lastThinking = nil
			}
		case model.ReasoningBlockKindSignature:
			if rb.Signature != "" && lastThinking != nil && lastThinking.Signature == nil {
				s := rb.Signature
				lastThinking.Signature = &s
			}
		}
	}

	logAnthropicSignatureAudit("inject", anthropicBlocks)

	return out
}

// logAnthropicSignatureAudit emits the audit counter for Anthropic
// reasoning signature passthrough. direction is one of inject / extract;
// the event name `transformer.reasoning.signature.passthrough` is fixed so
// downstream log pipelines can aggregate by (provider, direction). Called
// at Debug level so it only fires when diagnostic logging is enabled.
func logAnthropicSignatureAudit(direction string, blocks []model.ReasoningBlock) {
	var thinking, redacted, sigCount int
	for _, rb := range blocks {
		switch rb.Kind {
		case model.ReasoningBlockKindThinking:
			thinking++
			if rb.Signature != "" {
				sigCount++
			}
		case model.ReasoningBlockKindRedacted:
			redacted++
			sigCount++
		case model.ReasoningBlockKindSignature:
			if rb.Signature != "" {
				sigCount++
			}
		}
	}
	if thinking == 0 && redacted == 0 && sigCount == 0 {
		return
	}
	log.Debugw("transformer.reasoning.signature.passthrough",
		"provider", "anthropic",
		"direction", direction,
		"thinking_count", thinking,
		"redacted_count", redacted,
		"signature_count", sigCount,
	)
}

// truncateForAudit keeps audit log fields bounded to avoid logging entire
// multi-KB provider error payloads. Byte-level truncation is fine for
// audit purposes.
func truncateForAudit(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func emitThinkingBlocksLegacy(msg model.Message) []anthropicModel.MessageContentBlock {
	var out []anthropicModel.MessageContentBlock
	if msg.ReasoningContent != nil && *msg.ReasoningContent != "" {
		out = append(out, anthropicModel.MessageContentBlock{
			Type:      "thinking",
			Thinking:  msg.ReasoningContent,
			Signature: msg.ReasoningSignature,
		})
	}
	for _, data := range msg.RedactedThinkingBlocks {
		out = append(out, anthropicModel.MessageContentBlock{
			Type: "redacted_thinking",
			Data: data,
		})
	}
	return out
}

func buildMultipleContentWithThinking(msg model.Message) anthropicModel.MessageContent {
	blocks := emitThinkingBlocks(msg)

	// Only add text block if content is non-nil and non-empty
	if msg.Content.Content != nil && *msg.Content.Content != "" {
		blocks = append(blocks, anthropicModel.MessageContentBlock{
			Type:         "text",
			Text:         msg.Content.Content,
			CacheControl: convertCacheControl(msg.CacheControl),
		})
	}

	return anthropicModel.MessageContent{MultipleContent: blocks}
}

func convertMultiplePartContent(msg model.Message) anthropicModel.MessageContent {
	blocks := make([]anthropicModel.MessageContentBlock, 0, len(msg.Content.MultipleContent)+2)

	// Only emit thinking blocks when they carry a signature; without one, Anthropic rejects the
	// turn in subsequent extended-thinking rounds. emitThinkingBlocks already preserves order.
	for _, b := range emitThinkingBlocks(msg) {
		if b.Type == "thinking" && (b.Signature == nil || *b.Signature == "") {
			continue
		}
		blocks = append(blocks, b)
	}

	for _, part := range msg.Content.MultipleContent {
		switch part.Type {
		case "text":
			if part.Text != nil {
				blocks = append(blocks, anthropicModel.MessageContentBlock{
					Type:         "text",
					Text:         part.Text,
					CacheControl: convertCacheControl(part.CacheControl),
				})
			}
		case "image_url":
			if part.ImageURL != nil && part.ImageURL.URL != "" {
				block := convertImageURLToBlock(part)
				if block != nil {
					blocks = append(blocks, *block)
				}
			}
		case "document":
			if block := convertDocumentPartToBlock(part); block != nil {
				blocks = append(blocks, *block)
			}
		case "server_tool_use":
			if part.ServerToolUse == nil {
				continue
			}
			name := part.ServerToolUse.Name
			blocks = append(blocks, anthropicModel.MessageContentBlock{
				Type:         "server_tool_use",
				ID:           part.ServerToolUse.ID,
				Name:         &name,
				Input:        part.ServerToolUse.Input,
				CacheControl: convertCacheControl(part.CacheControl),
			})
		case "server_tool_result":
			if part.ServerToolResult == nil {
				continue
			}
			// Server tool result blocks carry a `content` field which may be
			// a raw text string or an array of sub-blocks; passthrough the
			// bytes so Anthropic receives the same shape the upstream
			// model produced.
			toolUseID := part.ServerToolResult.ToolUseID
			// BlockType preserves the exact Anthropic wire type seen by the
			// inbound layer (web_search_tool_result / code_execution_tool_result).
			// Falling back to web_search_tool_result keeps backwards
			// compatibility with callers that don't set BlockType.
			wireType := part.ServerToolResult.BlockType
			if wireType == "" {
				wireType = "web_search_tool_result"
			}
			var contentWrap *anthropicModel.MessageContent
			if len(part.ServerToolResult.Content) > 0 {
				c := anthropicModel.MessageContent{}
				if err := json.Unmarshal(part.ServerToolResult.Content, &c); err == nil {
					contentWrap = &c
				} else {
					// Fall back to a text string when the payload is a
					// raw string rather than the structured form.
					var raw string
					if err := json.Unmarshal(part.ServerToolResult.Content, &raw); err == nil {
						contentWrap = &anthropicModel.MessageContent{Content: &raw}
					}
				}
			}
			blocks = append(blocks, anthropicModel.MessageContentBlock{
				Type:         wireType,
				ToolUseID:    &toolUseID,
				Content:      contentWrap,
				IsError:      part.ServerToolResult.IsError,
				CacheControl: convertCacheControl(part.CacheControl),
			})
		}
	}

	// Add tool calls if present
	for _, toolCall := range msg.ToolCalls {
		input := json.RawMessage("{}")
		if toolCall.Function.Arguments != "" {
			if json.Valid([]byte(toolCall.Function.Arguments)) {
				input = json.RawMessage(toolCall.Function.Arguments)
			}
		}
		blocks = append(blocks, anthropicModel.MessageContentBlock{
			Type:         "tool_use",
			ID:           toolCall.ID,
			Name:         &toolCall.Function.Name,
			Input:        input,
			CacheControl: convertCacheControl(toolCall.CacheControl),
		})
	}

	if len(blocks) == 0 {
		return anthropicModel.MessageContent{}
	}

	return anthropicModel.MessageContent{MultipleContent: blocks}
}

func convertImageURLToBlock(part model.MessageContentPart) *anthropicModel.MessageContentBlock {
	if part.ImageURL == nil || part.ImageURL.URL == "" {
		return nil
	}

	url := part.ImageURL.URL
	if parsed := xurl.ParseDataURL(url); parsed != nil {
		return &anthropicModel.MessageContentBlock{
			Type: "image",
			Source: &anthropicModel.ImageSource{
				Type:      "base64",
				MediaType: parsed.MediaType,
				Data:      parsed.Data,
			},
			CacheControl: convertCacheControl(part.CacheControl),
		}
	}

	return &anthropicModel.MessageContentBlock{
		Type: "image",
		Source: &anthropicModel.ImageSource{
			Type: "url",
			URL:  part.ImageURL.URL,
		},
		CacheControl: convertCacheControl(part.CacheControl),
	}
}

// convertDocumentPartToBlock maps an internal MessageContentPart of type
// "document" into an Anthropic document content block. Anthropic accepts
// four source envelopes (base64 / url / text / content); we honour whatever
// the internal payload carries. Title / Context / Citations metadata is
// preserved, so citation-aware downstream callers keep working.
func convertDocumentPartToBlock(part model.MessageContentPart) *anthropicModel.MessageContentBlock {
	doc := part.Document
	if doc == nil {
		return nil
	}
	source := &anthropicModel.ImageSource{
		Type:      doc.Type,
		MediaType: doc.MediaType,
		Data:      doc.Data,
		URL:       doc.URL,
		Content:   doc.Content,
	}
	if doc.Type == "text" && doc.Data == "" && doc.Text != "" {
		source.Data = doc.Text
	}
	block := &anthropicModel.MessageContentBlock{
		Type:         "document",
		Source:       source,
		Title:        doc.Title,
		Context:      doc.Context,
		CacheControl: convertCacheControl(part.CacheControl),
	}
	if doc.Citations != nil {
		block.Citations = &anthropicModel.DocumentCitationsControl{Enabled: doc.Citations.Enabled}
	}
	return block
}

func convertTools(tools []model.Tool) []anthropicModel.Tool {
	result := make([]anthropicModel.Tool, 0, len(tools))
	for _, tool := range tools {
		switch tool.Type {
		case "function", "":
			result = append(result, anthropicModel.Tool{
				Name:         tool.Function.Name,
				Description:  tool.Function.Description,
				InputSchema:  tool.Function.Parameters,
				CacheControl: convertCacheControl(tool.CacheControl),
			})
		case "server_search", "code_execution", "url_context":
			// Anthropic exposes these via a different wire shape
			// (`{type:"web_search_20250305", ...}`) which is not yet
			// modelled by the anthropicModel.Tool struct. Drop with a
			// warning so the request still dispatches without these tools.
			continue
		default:
			continue
		}
	}
	return result
}

func convertStopSequences(stop *model.Stop) []string {
	if stop == nil {
		return nil
	}
	if stop.Stop != nil {
		return []string{*stop.Stop}
	}
	if len(stop.MultipleStop) > 0 {
		return stop.MultipleStop
	}
	return nil
}

func convertCacheControl(cc *model.CacheControl) *anthropicModel.CacheControl {
	if cc == nil {
		return nil
	}
	// Unknown values were already normalised at the inbound boundary; do a final safety check
	// so a misbehaving intermediate cannot sneak a provider-rejected value past us.
	if cc.Type != "" && cc.Type != model.CacheControlTypeEphemeral {
		return nil
	}
	ttl := cc.TTL
	if ttl != "" && ttl != model.CacheTTL5m && ttl != model.CacheTTL1h {
		ttl = ""
	}
	return &anthropicModel.CacheControl{
		Type: cc.Type,
		TTL:  ttl,
	}
}

// collectAnthropicBetaHeaders scans the outbound MessageRequest for features
// that require an `anthropic-beta` header and returns the gated values.
// Today we only detect cache_control.ttl == "1h" (extended-cache-ttl-2025-04-11);
// other server-tool betas (web-search-2025-03-05, code-execution-2025-05-22,
// computer-use-2025-01-24) will slot in here once A-H5 lands.
//
// Returning a slice (de-duplicated, order-preserving) lets callers join with
// a comma; multiple beta tags are valid in a single header per the Anthropic
// beta-headers spec.
// Ref: https://docs.anthropic.com/en/api/beta-headers
// Ref: https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching
func collectAnthropicBetaHeaders(req *anthropicModel.MessageRequest) []string {
	if req == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 2)
	add := func(beta string) {
		if beta == "" {
			return
		}
		if _, ok := seen[beta]; ok {
			return
		}
		seen[beta] = struct{}{}
		out = append(out, beta)
	}

	inspect := func(cc *anthropicModel.CacheControl) {
		if cc == nil {
			return
		}
		if cc.TTL == model.CacheTTL1h {
			add("extended-cache-ttl-2025-04-11")
		}
	}

	if req.System != nil {
		for i := range req.System.MultiplePrompts {
			inspect(req.System.MultiplePrompts[i].CacheControl)
		}
	}
	for i := range req.Tools {
		inspect(req.Tools[i].CacheControl)
	}
	for i := range req.Messages {
		msg := &req.Messages[i]
		for j := range msg.Content.MultipleContent {
			inspect(msg.Content.MultipleContent[j].CacheControl)
		}
	}
	return out
}

// pruneCacheBreakpoints walks the Anthropic request after conversion and drops cache_control
// entries that exceed the provider-enforced ceiling. Anthropic currently allows up to 4
// breakpoints per request; we keep the first N in encounter order (system → tools → messages)
// because callers typically mark the most reusable prefixes first.
func pruneCacheBreakpoints(req *anthropicModel.MessageRequest) {
	if req == nil {
		return
	}

	kept := 0
	keepOrClear := func(cc **anthropicModel.CacheControl) {
		if cc == nil || *cc == nil {
			return
		}
		if kept >= model.AnthropicMaxCacheBreakpoints {
			*cc = nil
			return
		}
		kept++
	}

	if req.System != nil {
		for i := range req.System.MultiplePrompts {
			keepOrClear(&req.System.MultiplePrompts[i].CacheControl)
		}
	}
	for i := range req.Tools {
		keepOrClear(&req.Tools[i].CacheControl)
	}
	for i := range req.Messages {
		msg := &req.Messages[i]
		for j := range msg.Content.MultipleContent {
			keepOrClear(&msg.Content.MultipleContent[j].CacheControl)
		}
	}
}

func getThinkingBudget(effort string, budget *int64) *int64 {
	if budget != nil {
		return budget
	}

	var result int64
	switch effort {
	case anthropicModel.EffortLow:
		result = 1024
	case anthropicModel.EffortMedium:
		result = 8192
	case anthropicModel.EffortHigh:
		result = 32768
	default:
		result = 8192
	}
	return &result
}

// Response conversion functions

func convertToLLMResponse(resp *anthropicModel.Message) *model.InternalLLMResponse {
	if resp == nil {
		return &model.InternalLLMResponse{
			Object: "chat.completion",
		}
	}

	result := &model.InternalLLMResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Model:   resp.Model,
		Created: 0,
	}

	var (
		content           model.MessageContent
		thinkingText      *string
		thinkingSignature *string
		toolCalls         []model.ToolCall
		textParts         []string
		redactedBlocks    []string
		reasoningBlocks   []model.ReasoningBlock
	)

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			if block.Text != nil && *block.Text != "" {
				textParts = append(textParts, *block.Text)
				content.MultipleContent = append(content.MultipleContent, model.MessageContentPart{
					Type: "text",
					Text: block.Text,
				})
			}
		case "tool_use":
			if block.ID != "" && block.Name != nil {
				input := "{}"
				if len(block.Input) > 0 {
					input = string(block.Input)
				}
				toolCalls = append(toolCalls, model.ToolCall{
					ID:   block.ID,
					Type: "function",
					Function: model.FunctionCall{
						Name:      *block.Name,
						Arguments: input,
					},
				})
			}
		case "thinking":
			if block.Thinking != nil {
				thinkingText = block.Thinking
			}
			thinkingSignature = block.Signature
			rb := model.ReasoningBlock{
				Kind:     model.ReasoningBlockKindThinking,
				Index:    len(reasoningBlocks),
				Provider: "anthropic",
			}
			if block.Thinking != nil {
				rb.Text = *block.Thinking
			}
			if block.Signature != nil {
				rb.Signature = *block.Signature
			}
			reasoningBlocks = append(reasoningBlocks, rb)
		case "redacted_thinking":
			if block.Data != "" {
				redactedBlocks = append(redactedBlocks, block.Data)
				reasoningBlocks = append(reasoningBlocks, model.ReasoningBlock{
					Kind:     model.ReasoningBlockKindRedacted,
					Index:    len(reasoningBlocks),
					Data:     block.Data,
					Provider: "anthropic",
				})
			}
		case "server_tool_use":
			content.MultipleContent = append(content.MultipleContent, model.MessageContentPart{
				Type: "server_tool_use",
				ServerToolUse: &model.ServerToolUseBlock{
					ID:    block.ID,
					Name:  lo.FromPtr(block.Name),
					Input: block.Input,
				},
			})
		case "web_search_tool_result", "code_execution_tool_result":
			result := &model.ServerToolResultBlock{
				ToolUseID: lo.FromPtr(block.ToolUseID),
				IsError:   block.IsError,
			}
			if block.Content != nil {
				if block.Content.Content != nil {
					b, _ := json.Marshal(*block.Content.Content)
					result.Content = b
				} else if len(block.Content.MultipleContent) > 0 {
					b, _ := json.Marshal(block.Content.MultipleContent)
					result.Content = b
				}
			}
			content.MultipleContent = append(content.MultipleContent, model.MessageContentPart{
				Type:             "server_tool_result",
				ServerToolResult: result,
			})
		}
	}

	// If we only have text content, use simple string format
	if len(textParts) > 0 && len(content.MultipleContent) == len(textParts) {
		allText := strings.Join(textParts, "")
		content.Content = &allText
		content.MultipleContent = nil
	}

	message := &model.Message{
		Role:                   resp.Role,
		Content:                content,
		ToolCalls:              toolCalls,
		ReasoningContent:       thinkingText,
		ReasoningSignature:     thinkingSignature,
		RedactedThinkingBlocks: redactedBlocks,
		ReasoningBlocks:        reasoningBlocks,
	}

	choice := model.Choice{
		Index:        0,
		Message:      message,
		FinishReason: convertStopReason(resp.StopReason),
	}

	result.Choices = []model.Choice{choice}
	result.Usage = convertAnthropicUsage(resp.Usage)

	logAnthropicSignatureAudit("extract", reasoningBlocks)

	return result
}

// convertStopReason parses Anthropic's stop_reason into the canonical
// FinishReason (model.FinishReasonFromAnthropic) and returns a *string for
// Choice.FinishReason. Rich reasons such as "pause_turn" / "refusal" are
// preserved so downstream inbounds can distinguish them from a plain stop.
func convertStopReason(stopReason *string) *string {
	if stopReason == nil {
		return nil
	}
	reason := model.FinishReasonFromAnthropic(*stopReason)
	if reason.IsZero() {
		return nil
	}
	s := reason.String()
	return &s
}

// mapAnthropicErrorTypeToStatus maps Anthropic API error `type` strings to HTTP
// status codes so streaming error events can be surfaced with the correct code.
// Reference: https://docs.anthropic.com/en/api/errors
func mapAnthropicErrorTypeToStatus(errType string) int {
	switch errType {
	case "invalid_request_error":
		return http.StatusBadRequest
	case "authentication_error":
		return http.StatusUnauthorized
	case "permission_error":
		return http.StatusForbidden
	case "not_found_error":
		return http.StatusNotFound
	case "request_too_large":
		return http.StatusRequestEntityTooLarge
	case "rate_limit_error":
		return http.StatusTooManyRequests
	case "overloaded_error":
		return 529
	case "api_error":
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

func convertAnthropicUsage(usage *anthropicModel.Usage) *model.Usage {
	if usage == nil {
		return nil
	}

	result := &model.Usage{
		PromptTokens:             usage.InputTokens,
		CompletionTokens:         usage.OutputTokens,
		TotalTokens:              usage.InputTokens + usage.OutputTokens + usage.CacheReadInputTokens + usage.CacheCreationInputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
	}

	if usage.CacheCreation != nil {
		result.CacheCreation5mInputTokens = usage.CacheCreation.Ephemeral5mInputTokens
		result.CacheCreation1hInputTokens = usage.CacheCreation.Ephemeral1hInputTokens
	}

	if usage.CacheReadInputTokens > 0 {
		result.PromptTokensDetails = &model.PromptTokensDetails{
			CachedTokens: usage.CacheReadInputTokens,
		}
	}
	return result
}
