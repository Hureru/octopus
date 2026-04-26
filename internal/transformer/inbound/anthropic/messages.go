package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/tokenizer"
	"github.com/bestruirui/octopus/internal/utils/xurl"
	"github.com/samber/lo"
)

type MessagesInbound struct {
	// Stream state tracking
	hasStarted                bool
	hasTextContentStarted     bool
	hasThinkingContentStarted bool
	hasToolContentStarted     bool
	hasFinished               bool
	messageStopped            bool
	messageID                 string
	modelName                 string
	contentIndex              int64
	stopReason                *string
	stopSequence              *string
	toolCallIndices           map[int]bool // Track which tool call indices we've seen
	inputToken                int64

	streamAggregator model.StreamAggregator
	// storedResponse stores the non-stream response
	storedResponse *model.InternalLLMResponse
}

func geminiThoughtSignatureExtension(signature string) *OctopusExtension {
	return model.GeminiThoughtSignatureExtension(signature)
}

func geminiThoughtSignatureFromExtension(ext *OctopusExtension) string {
	return model.GeminiThoughtSignatureFromExtension(ext)
}

func (i *MessagesInbound) TransformRequest(ctx context.Context, body []byte) (*model.InternalLLMRequest, error) {
	var anthropicReq MessageRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		return nil, err
	}
	if anthropicReq.MaxTokens < 1 {
		anthropicReq.MaxTokens = 1
	}
	chatReq := &model.InternalLLMRequest{
		Model:               anthropicReq.Model,
		MaxTokens:           &anthropicReq.MaxTokens,
		Temperature:         anthropicReq.Temperature,
		TopP:                anthropicReq.TopP,
		TopK:                anthropicReq.TopK,
		Stream:              anthropicReq.Stream,
		Metadata:            map[string]string{},
		RawAPIFormat:        model.APIFormatAnthropicMessage,
		TransformerMetadata: map[string]string{},
	}
	if tier := strings.TrimSpace(anthropicReq.ServiceTier); tier != "" {
		chatReq.ServiceTier = &tier
	}
	if anthropicReq.Metadata != nil {
		if userID := strings.TrimSpace(anthropicReq.Metadata.UserID); userID != "" {
			chatReq.TransformerMetadata["anthropic_user_id"] = userID
		}
	}
	// mcp_servers / container (A-H6): preserve the raw payload for
	// round-trip on the Anthropic→Anthropic same-protocol path. Triggers
	// the mcp-client-2025-11-20 beta header downstream (A-H7).
	if len(anthropicReq.MCPServers) > 0 {
		chatReq.AnthropicMCPServers = append(chatReq.AnthropicMCPServers[:0], anthropicReq.MCPServers...)
	}
	if len(anthropicReq.Container) > 0 {
		chatReq.AnthropicContainer = append(chatReq.AnthropicContainer[:0], anthropicReq.Container...)
	}

	// Convert messages
	messages := make([]model.Message, 0, len(anthropicReq.Messages))

	// Add system message if present
	if anthropicReq.System != nil {
		if anthropicReq.System.Prompt != nil {
			systemContent := anthropicReq.System.Prompt
			messages = append(messages, model.Message{
				Role: "system",
				Content: model.MessageContent{
					Content: systemContent,
				},
			})
			i.inputToken += int64(tokenizer.CountTokens(*systemContent, chatReq.Model))
		} else if len(anthropicReq.System.MultiplePrompts) > 0 {
			// Mark that system was originally in array format
			chatReq.TransformerMetadata["anthropic_system_array_format"] = "true"

			for _, prompt := range anthropicReq.System.MultiplePrompts {
				msg := model.Message{
					Role: "system",
					Content: model.MessageContent{
						Content: &prompt.Text,
					},
					CacheControl: convertToLLMCacheControl(prompt.CacheControl),
				}
				i.inputToken += int64(tokenizer.CountTokens(prompt.Text, chatReq.Model))
				messages = append(messages, msg)
			}
		}
	}

	// Convert Anthropic messages to ChatCompletionMessage
	for msgIndex, msg := range anthropicReq.Messages {
		chatMsg := model.Message{
			Role: msg.Role,
		}

		var (
			hasContent    bool
			hasToolResult bool
		)

		// Convert content

		if msg.Content.Content != nil {
			chatMsg.Content = model.MessageContent{
				Content: msg.Content.Content,
			}
			hasContent = true
			i.inputToken += int64(tokenizer.CountTokens(*msg.Content.Content, chatReq.Model))
		} else if len(msg.Content.MultipleContent) > 0 {
			contentParts := make([]model.MessageContentPart, 0, len(msg.Content.MultipleContent))

			var (
				reasoningContent      string
				hasReasoningInContent bool
			)

			var reasoningSignature string

			for _, block := range msg.Content.MultipleContent {
				switch block.Type {
				case "thinking":
					// Keep thinking content in MultipleContent to preserve order
					thinkingText := ""
					if block.Thinking != nil && *block.Thinking != "" {
						thinkingText = *block.Thinking
						reasoningContent = thinkingText
						hasReasoningInContent = true
					}

					sig := ""
					if block.Signature != nil && *block.Signature != "" {
						sig = *block.Signature
						reasoningSignature = sig
					}

					// Preserve per-block provenance so multi-thinking-block assistant turns can
					// be replayed to Anthropic without flattening to a single signature.
					chatMsg.AppendReasoningBlock(model.ReasoningBlock{
						Kind:      model.ReasoningBlockKindThinking,
						Index:     -1,
						Text:      thinkingText,
						Signature: sig,
						Provider:  "anthropic",
					})
				case "redacted_thinking":
					if block.Data != "" {
						chatMsg.RedactedThinkingBlocks = append(chatMsg.RedactedThinkingBlocks, block.Data)
						chatMsg.AppendReasoningBlock(model.ReasoningBlock{
							Kind:     model.ReasoningBlockKindRedacted,
							Index:    -1,
							Data:     block.Data,
							Provider: "anthropic",
						})
						hasContent = true
					}
				case "text":
					contentParts = append(contentParts, model.MessageContentPart{
						Type:         "text",
						Text:         block.Text,
						CacheControl: convertToLLMCacheControl(block.CacheControl),
					})
					i.inputToken += int64(tokenizer.CountTokens(*block.Text, chatReq.Model))
					hasContent = true
				case "image":
					if block.Source != nil {
						part := model.MessageContentPart{
							Type:         "image_url",
							CacheControl: convertToLLMCacheControl(block.CacheControl),
						}
						if block.Source.Type == "base64" {
							// Convert Anthropic image format to OpenAI format
							imageURL := fmt.Sprintf("data:%s;base64,%s", block.Source.MediaType, block.Source.Data)
							part.ImageURL = &model.ImageURL{
								URL: imageURL,
							}
						} else {
							part.ImageURL = &model.ImageURL{
								URL: block.Source.URL,
							}
						}

						contentParts = append(contentParts, part)
						hasContent = true
					}
				case "tool_result":
					hasToolResult = true
					toolMsg := model.Message{
						Role:            "tool",
						MessageIndex:    lo.ToPtr(msgIndex),
						ToolCallID:      block.ToolUseID,
						CacheControl:    convertToLLMCacheControl(block.CacheControl),
						ToolCallIsError: block.IsError,
					}

					if block.Content != nil {
						if block.Content.Content != nil {
							toolMsg.Content = model.MessageContent{
								Content: block.Content.Content,
							}
						} else if len(block.Content.MultipleContent) > 0 {
							// Handle multiple content blocks in tool_result
							// Keep as MultipleContent to preserve the original format
							toolContentParts := make([]model.MessageContentPart, 0, len(block.Content.MultipleContent))
							for _, contentBlock := range block.Content.MultipleContent {
								if contentBlock.Type == "text" {
									toolContentParts = append(toolContentParts, model.MessageContentPart{
										Type: "text",
										Text: contentBlock.Text,
									})
									i.inputToken += int64(tokenizer.CountTokens(*contentBlock.Text, chatReq.Model))
								}
							}

							toolMsg.Content = model.MessageContent{
								MultipleContent: toolContentParts,
							}
						}
					}

					messages = append(messages, toolMsg)
				case "tool_use":
					toolCall := model.ToolCall{
						ID:   block.ID,
						Type: "function",
						Function: model.FunctionCall{
							Name:      lo.FromPtr(block.Name),
							Arguments: string(block.Input),
						},
						CacheControl: convertToLLMCacheControl(block.CacheControl),
					}
					if block.Octopus != nil {
						toolCall.ProviderExtensions = block.Octopus.ProviderExtensions
					}
					if sig := geminiThoughtSignatureFromExtension(block.Octopus); sig != "" {
						toolCall.ThoughtSignature = sig
					}
					chatMsg.ToolCalls = append(chatMsg.ToolCalls, toolCall)
					hasContent = true
				case "document":
					part := convertDocumentBlockToLLM(block)
					if part != nil {
						contentParts = append(contentParts, *part)
						hasContent = true
					}
				case "server_tool_use":
					contentParts = append(contentParts, model.MessageContentPart{
						Type: "server_tool_use",
						ServerToolUse: &model.ServerToolUseBlock{
							ID:    block.ID,
							Name:  lo.FromPtr(block.Name),
							Input: block.Input,
						},
						CacheControl: convertToLLMCacheControl(block.CacheControl),
					})
					hasContent = true
				case "web_search_tool_result", "code_execution_tool_result":
					result := &model.ServerToolResultBlock{
						ToolUseID: lo.FromPtr(block.ToolUseID),
						IsError:   block.IsError,
						BlockType: block.Type,
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
					contentParts = append(contentParts, model.MessageContentPart{
						Type:             "server_tool_result",
						ServerToolResult: result,
						CacheControl:     convertToLLMCacheControl(block.CacheControl),
					})
					hasContent = true
				}
			}

			// Check if it's a simple text-only message (single text block)
			if len(contentParts) == 1 && contentParts[0].Type == "text" {
				// Convert single text block to simple content format for compatibility
				chatMsg.Content = model.MessageContent{
					Content: contentParts[0].Text,
				}
				// Preserve cache control at message level when simplifying
				if contentParts[0].CacheControl != nil {
					chatMsg.CacheControl = contentParts[0].CacheControl
				}

				hasContent = true
			} else if len(contentParts) > 0 {
				chatMsg.Content = model.MessageContent{
					MultipleContent: contentParts,
				}
				hasContent = true
			}

			// Assign reasoning content and signature if present
			if reasoningContent != "" && hasReasoningInContent {
				chatMsg.ReasoningContent = &reasoningContent
			}

			if reasoningSignature != "" {
				chatMsg.ReasoningSignature = &reasoningSignature
			}
		}

		if !hasContent {
			continue
		}

		// If this message had tool_result blocks, set MessageIndex so we can match it later
		if hasToolResult {
			chatMsg.MessageIndex = lo.ToPtr(msgIndex)
		}

		messages = append(messages, chatMsg)
	}

	chatReq.Messages = messages

	// Convert tools
	if len(anthropicReq.Tools) > 0 {
		tools := make([]model.Tool, 0, len(anthropicReq.Tools))
		for _, tool := range anthropicReq.Tools {
			if tool.IsServerTool() {
				// Server-side tool (web_search_*, code_execution_*, computer_*).
				// Preserve the raw spec body so the outbound path can replay
				// the wire payload verbatim; Type drives beta header selection.
				llmTool := model.Tool{
					Type: tool.Type,
					Function: model.Function{
						Name: tool.Name,
					},
					CacheControl:        convertToLLMCacheControl(tool.CacheControl),
					AnthropicServerSpec: tool.RawBody,
				}
				tools = append(tools, llmTool)
				i.inputToken += int64(tokenizer.CountTokens(tool.Name, chatReq.Model))
				continue
			}
			llmTool := model.Tool{
				Type: "function",
				Function: model.Function{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
				CacheControl: convertToLLMCacheControl(tool.CacheControl),
			}
			tools = append(tools, llmTool)
			i.inputToken += int64(tokenizer.CountTokens(tool.Name, chatReq.Model))
			i.inputToken += int64(tokenizer.CountTokens(tool.Description, chatReq.Model))
			i.inputToken += int64(tokenizer.CountTokens(string(tool.InputSchema), chatReq.Model))
		}
		i.inputToken += int64(len(tools) * 3)

		chatReq.Tools = tools
	}

	// Convert tool_choice
	if anthropicReq.ToolChoice != nil {
		chatReq.ToolChoice = convertToolChoiceFromAnthropic(anthropicReq.ToolChoice)
	}

	// Convert stop sequences
	if len(anthropicReq.StopSequences) > 0 {
		if len(anthropicReq.StopSequences) == 1 {
			chatReq.Stop = &model.Stop{
				Stop: &anthropicReq.StopSequences[0],
			}
		} else {
			chatReq.Stop = &model.Stop{
				MultipleStop: anthropicReq.StopSequences,
			}
		}
	}

	// Convert thinking configuration to reasoning effort and preserve budget
	if anthropicReq.Thinking != nil {
		if anthropicReq.Thinking.Display != "" {
			chatReq.ThinkingDisplay = anthropicReq.Thinking.Display
		}
		switch anthropicReq.Thinking.Type {
		case ThinkingTypeEnabled:
			if anthropicReq.Thinking.BudgetTokens != nil {
				chatReq.ReasoningEffort = thinkingBudgetToReasoningEffort(*anthropicReq.Thinking.BudgetTokens)
				chatReq.ReasoningBudget = anthropicReq.Thinking.BudgetTokens
			} else {
				log.Warnf("thinking type is 'enabled' but budget_tokens is nil, thinking will be ignored")
			}
		case ThinkingTypeAdaptive:
			effort := EffortHigh
			if anthropicReq.OutputConfig != nil && anthropicReq.OutputConfig.Effort != "" {
				effort = anthropicReq.OutputConfig.Effort
			}
			chatReq.ReasoningEffort = effort
			chatReq.AdaptiveThinking = true
		case ThinkingTypeDisabled:
			// Explicitly disabled, nothing to do
		default:
			log.Warnf("unknown thinking type: %s", anthropicReq.Thinking.Type)
		}
	}
	return chatReq, nil
}

// convertToolChoiceFromAnthropic converts the wire-level Anthropic
// ToolChoice into the provider-agnostic internal representation. The string
// form is used for {auto,none,any} which are the simple modes; the named
// form preserves `tool + name` (Anthropic) and `disable_parallel_tool_use`
// so outbound emitters can reproduce them verbatim when the upstream is
// also Anthropic.
func convertToolChoiceFromAnthropic(src *ToolChoice) *model.ToolChoice {
	if src == nil {
		return nil
	}
	switch src.Type {
	case "auto", "none", "any":
		if src.DisableParallelToolUse == nil {
			mode := src.Type
			return &model.ToolChoice{ToolChoice: &mode}
		}
		return &model.ToolChoice{
			NamedToolChoice: &model.NamedToolChoice{
				Type:                   src.Type,
				DisableParallelToolUse: src.DisableParallelToolUse,
			},
		}
	case "tool":
		named := &model.NamedToolChoice{
			Type:                   "tool",
			DisableParallelToolUse: src.DisableParallelToolUse,
		}
		if src.Name != nil {
			name := *src.Name
			named.Name = &name
			named.Function = &model.ToolFunction{Name: name}
		}
		return &model.ToolChoice{NamedToolChoice: named}
	default:
		return nil
	}
}

// convertDocumentBlockToLLM maps an Anthropic document content block into
// an internal MessageContentPart of type "document". The wire `source`
// carries either a base64/url/text payload or a pre-chunked content array;
// Title / Context / Citations metadata is preserved verbatim.
func convertDocumentBlockToLLM(block MessageContentBlock) *model.MessageContentPart {
	if block.Source == nil {
		return nil
	}
	doc := &model.DocumentSource{
		Type:      block.Source.Type,
		MediaType: block.Source.MediaType,
		Data:      block.Source.Data,
		URL:       block.Source.URL,
		Content:   block.Source.Content,
		Title:     block.Title,
		Context:   block.Context,
	}
	// The wire shape carries text in source.data when type == "text"; split
	// it out into the dedicated Text field so converters can distinguish
	// raw text from a base64 blob.
	if doc.Type == "text" {
		doc.Text = doc.Data
		doc.Data = ""
	}
	if block.Citations != nil {
		doc.Citations = &model.DocumentCitations{Enabled: block.Citations.Enabled}
	}
	return &model.MessageContentPart{
		Type:         "document",
		Document:     doc,
		CacheControl: convertToLLMCacheControl(block.CacheControl),
	}
}

func (i *MessagesInbound) TransformResponse(ctx context.Context, response *model.InternalLLMResponse) ([]byte, error) {
	// Store the response for later retrieval
	i.storedResponse = response

	resp := &Message{
		ID:    response.ID,
		Type:  "message",
		Role:  "assistant",
		Model: response.Model,
	}

	// Convert choices to content blocks
	if len(response.Choices) > 0 {
		choice := response.Choices[0]

		var message *model.Message

		if choice.Message != nil {
			message = choice.Message
		} else if choice.Delta != nil {
			message = choice.Delta
		}

		if message != nil {
			var contentBlocks []MessageContentBlock

			// Prefer per-block reasoning provenance when available so multiple thinking /
			// redacted_thinking blocks from the upstream can be replayed in order. Fall back to
			// the legacy flat fields when ReasoningBlocks is empty (non-Anthropic upstream).
			if len(message.ReasoningBlocks) > 0 {
				for _, rb := range message.ReasoningBlocks {
					switch rb.Kind {
					case model.ReasoningBlockKindThinking:
						block := MessageContentBlock{Type: "thinking"}
						if rb.Text != "" {
							t := rb.Text
							block.Thinking = &t
						}
						if rb.Signature != "" {
							s := rb.Signature
							block.Signature = &s
						}
						contentBlocks = append(contentBlocks, block)
					case model.ReasoningBlockKindRedacted:
						if rb.Data != "" {
							contentBlocks = append(contentBlocks, MessageContentBlock{
								Type: "redacted_thinking",
								Data: rb.Data,
							})
						}
					}
				}
			} else {
				// Handle reasoning content (thinking) first if present
				if message.ReasoningContent != nil && *message.ReasoningContent != "" {
					thinkingBlock := MessageContentBlock{
						Type:     "thinking",
						Thinking: message.ReasoningContent,
					}
					if message.ReasoningSignature != nil && *message.ReasoningSignature != "" {
						thinkingBlock.Signature = message.ReasoningSignature
					}
					// No fallback magic string — if signature is absent (non-Anthropic upstream),
					// Signature remains nil and is omitted via omitempty.

					contentBlocks = append(contentBlocks, thinkingBlock)
				}

				// Handle redacted thinking blocks
				for _, data := range message.RedactedThinkingBlocks {
					contentBlocks = append(contentBlocks, MessageContentBlock{
						Type: "redacted_thinking",
						Data: data,
					})
				}
			}

			// Handle regular content
			if message.Content.Content != nil && *message.Content.Content != "" {
				contentBlocks = append(contentBlocks, MessageContentBlock{
					Type: "text",
					Text: message.Content.Content,
				})
			} else if len(message.Content.MultipleContent) > 0 {
				for _, part := range message.Content.MultipleContent {
					switch part.Type {
					case "text":
						if part.Text != nil {
							contentBlocks = append(contentBlocks, MessageContentBlock{
								Type: "text",
								Text: part.Text,
							})
						}
					case "image_url":
						if part.ImageURL != nil && part.ImageURL.URL != "" {
							// Convert OpenAI image format to Anthropic format
							url := part.ImageURL.URL
							if parsed := xurl.ParseDataURL(url); parsed != nil {
								contentBlocks = append(contentBlocks, MessageContentBlock{
									Type: "image",
									Source: &ImageSource{
										Type:      "base64",
										MediaType: parsed.MediaType,
										Data:      parsed.Data,
									},
								})
							} else {
								contentBlocks = append(contentBlocks, MessageContentBlock{
									Type: "image",
									Source: &ImageSource{
										Type: "url",
										URL:  part.ImageURL.URL,
									},
								})
							}
						}
					}
				}
			}

			// Handle tool calls
			if len(message.ToolCalls) > 0 {
				for _, toolCall := range message.ToolCalls {
					var input json.RawMessage
					if toolCall.Function.Arguments != "" {
						// Attempt to use the provided arguments; repair if invalid, fallback to {}
						if json.Valid([]byte(toolCall.Function.Arguments)) {
							input = json.RawMessage(toolCall.Function.Arguments)
						} else {
							input = json.RawMessage("{}")
						}
					} else {
						input = json.RawMessage("{}")
					}

					block := MessageContentBlock{
						Type:  "tool_use",
						ID:    toolCall.ID,
						Name:  &toolCall.Function.Name,
						Input: input,
					}
					if ext := geminiThoughtSignatureExtension(toolCall.GetGeminiExtensions().ThoughtSignature); ext != nil {
						block.Octopus = ext
					}
					contentBlocks = append(contentBlocks, block)
				}
			}

			resp.Content = contentBlocks
		}

		// Convert finish reason
		if choice.FinishReason != nil {
			reason := model.ParseFinishReason(*choice.FinishReason)
			if wire := reason.ToAnthropic(); wire != "" {
				resp.StopReason = &wire
			} else {
				resp.StopReason = choice.FinishReason
			}
		}

		if choice.StopSequence != nil {
			resp.StopSequence = choice.StopSequence
		}
	}

	// Convert usage
	if response.Usage != nil {
		resp.Usage = i.convertUsage(response.Usage)
	}

	return json.Marshal(resp)
}

func (i *MessagesInbound) TransformStream(ctx context.Context, stream *model.InternalLLMResponse) ([]byte, error) {
	// Handle upstream error event: forward as Anthropic SSE `event: error` and
	// terminate the stream. Reference:
	// https://docs.anthropic.com/en/api/messages-streaming#error-events
	if stream != nil && stream.Error != nil {
		errType := stream.Error.Detail.Type
		if errType == "" {
			errType = "api_error"
		}
		errPayload := StreamEvent{
			Type: "error",
			Error: &ErrorDetail{
				Type:    errType,
				Message: stream.Error.Detail.Message,
			},
		}
		data, err := json.Marshal(errPayload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal error event: %w", err)
		}
		i.messageStopped = true
		return formatSSEEvent("error", data), nil
	}

	// Handle [DONE] marker
	if stream.Object == "[DONE]" {
		if i.hasFinished && !i.messageStopped {
			events, err := i.finalizeStreamMessage(nil)
			if err != nil {
				return nil, err
			}
			if len(events) == 0 {
				return nil, nil
			}
			return joinSSEEvents(events), nil
		}
		return nil, nil
	}

	// Store the chunk for aggregation
	i.streamAggregator.Add(stream)

	var events [][]byte

	// Initialize message ID and model from first chunk
	if i.messageID == "" && stream.ID != "" {
		i.messageID = stream.ID
	}
	if i.modelName == "" && stream.Model != "" {
		i.modelName = stream.Model
	}

	// Generate message_start event if this is the first chunk
	if !i.hasStarted {
		i.hasStarted = true

		usage := &Usage{
			InputTokens:  i.inputToken,
			OutputTokens: 1,
		}
		if stream.Usage != nil {
			usage = i.convertUsage(stream.Usage)
		}

		startEvent := StreamEvent{
			Type: "message_start",
			Message: &StreamMessage{
				ID:      i.messageID,
				Type:    "message",
				Role:    "assistant",
				Model:   i.modelName,
				Content: []MessageContentBlock{},
				Usage:   usage,
			},
		}

		data, err := json.Marshal(startEvent)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal message_start event: %w", err)
		}
		events = append(events, formatSSEEvent("message_start", data))
	}

	// Process the current chunk
	if len(stream.Choices) > 0 {
		choice := stream.Choices[0]

		// Handle reasoning content (thinking) delta
		if choice.Delta != nil && choice.Delta.ReasoningContent != nil && *choice.Delta.ReasoningContent != "" {
			// If the tool content has started before the thinking content, we need to stop it
			if i.hasToolContentStarted {
				i.hasToolContentStarted = false

				stopEvent := StreamEvent{
					Type:  "content_block_stop",
					Index: &i.contentIndex,
				}
				data, err := json.Marshal(stopEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_stop", data))

				i.contentIndex++
			}

			// Generate content_block_start if this is the first thinking content
			if !i.hasThinkingContentStarted {
				i.hasThinkingContentStarted = true

				startEvent := StreamEvent{
					Type:  "content_block_start",
					Index: &i.contentIndex,
					ContentBlock: &MessageContentBlock{
						Type:      "thinking",
						Thinking:  lo.ToPtr(""),
						Signature: lo.ToPtr(""),
					},
				}
				data, err := json.Marshal(startEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_start event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_start", data))
			}

			// Generate content_block_delta for thinking
			deltaEvent := StreamEvent{
				Type:  "content_block_delta",
				Index: &i.contentIndex,
				Delta: &StreamDelta{
					Type:     lo.ToPtr("thinking_delta"),
					Thinking: choice.Delta.ReasoningContent,
				},
			}
			data, err := json.Marshal(deltaEvent)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal content_block_delta event: %w", err)
			}
			events = append(events, formatSSEEvent("content_block_delta", data))
		}

		// Add signature delta if signature is available
		if choice.Delta != nil && choice.Delta.ReasoningSignature != nil && *choice.Delta.ReasoningSignature != "" {
			sigEvent := StreamEvent{
				Type:  "content_block_delta",
				Index: &i.contentIndex,
				Delta: &StreamDelta{
					Type:      lo.ToPtr("signature_delta"),
					Signature: choice.Delta.ReasoningSignature,
				},
			}
			data, err := json.Marshal(sigEvent)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal signature_delta event: %w", err)
			}
			events = append(events, formatSSEEvent("content_block_delta", data))
		}

		// Handle redacted thinking blocks (complete blocks, not deltas)
		if choice.Delta != nil && len(choice.Delta.RedactedThinkingBlocks) > 0 {
			// Close any open thinking content block first
			if i.hasThinkingContentStarted {
				i.hasThinkingContentStarted = false
				stopEvent := StreamEvent{
					Type:  "content_block_stop",
					Index: &i.contentIndex,
				}
				stopData, err := json.Marshal(stopEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_stop", stopData))
				i.contentIndex++
			}

			for _, rtData := range choice.Delta.RedactedThinkingBlocks {
				startEvent := StreamEvent{
					Type:  "content_block_start",
					Index: &i.contentIndex,
					ContentBlock: &MessageContentBlock{
						Type: "redacted_thinking",
						Data: rtData,
					},
				}
				startData, err := json.Marshal(startEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_start event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_start", startData))

				stopEvent := StreamEvent{
					Type:  "content_block_stop",
					Index: &i.contentIndex,
				}
				stopData, err := json.Marshal(stopEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_stop", stopData))
				i.contentIndex++
			}
		}

		// Handle content delta
		if choice.Delta != nil && choice.Delta.Content.Content != nil && *choice.Delta.Content.Content != "" {
			// If the thinking content has started before the text content, we need to stop it
			if i.hasThinkingContentStarted {
				i.hasThinkingContentStarted = false

				stopEvent := StreamEvent{
					Type:  "content_block_stop",
					Index: &i.contentIndex,
				}
				data, err := json.Marshal(stopEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_stop", data))

				i.contentIndex++
			}

			// If the tool content has started before the content block, we need to stop it
			if i.hasToolContentStarted {
				i.hasToolContentStarted = false

				stopEvent := StreamEvent{
					Type:  "content_block_stop",
					Index: &i.contentIndex,
				}
				data, err := json.Marshal(stopEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_stop", data))

				i.contentIndex++
			}

			// Generate content_block_start if this is the first content
			if !i.hasTextContentStarted {
				i.hasTextContentStarted = true

				startEvent := StreamEvent{
					Type:  "content_block_start",
					Index: &i.contentIndex,
					ContentBlock: &MessageContentBlock{
						Type: "text",
						Text: lo.ToPtr(""),
					},
				}
				data, err := json.Marshal(startEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_start event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_start", data))
			}

			// Generate content_block_delta
			deltaEvent := StreamEvent{
				Type:  "content_block_delta",
				Index: &i.contentIndex,
				Delta: &StreamDelta{
					Type: lo.ToPtr("text_delta"),
					Text: choice.Delta.Content.Content,
				},
			}
			data, err := json.Marshal(deltaEvent)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal content_block_delta event: %w", err)
			}
			events = append(events, formatSSEEvent("content_block_delta", data))
		}

		// Handle tool calls
		if choice.Delta != nil && len(choice.Delta.ToolCalls) > 0 {
			// If the thinking content has started before the tool content, we need to stop it
			if i.hasThinkingContentStarted {
				i.hasThinkingContentStarted = false

				stopEvent := StreamEvent{
					Type:  "content_block_stop",
					Index: &i.contentIndex,
				}
				data, err := json.Marshal(stopEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_stop", data))

				i.contentIndex++
			}

			// If the text content has started before the tool content, we need to stop it
			if i.hasTextContentStarted {
				i.hasTextContentStarted = false

				stopEvent := StreamEvent{
					Type:  "content_block_stop",
					Index: &i.contentIndex,
				}
				data, err := json.Marshal(stopEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_stop", data))

				i.contentIndex++
			}

			// Initialize tool call index tracking if needed
			if i.toolCallIndices == nil {
				i.toolCallIndices = make(map[int]bool)
			}

			for _, deltaToolCall := range choice.Delta.ToolCalls {
				toolCallIndex := deltaToolCall.Index

				// Initialize tool call if it doesn't exist
				if !i.toolCallIndices[toolCallIndex] {
					// 只有当此前确实已经打开过一个 tool_use 块（即将开启第二个或之后的
					// 工具块）时才发 stop；用 toolCallIndex>0 判断不可靠，因为上游
					// （尤其是 Responses API）把 OutputIndex 写入该字段，首个工具块
					// 的 OutputIndex 往往已经大于 0（前面可能先出现 message/reasoning
					// item），此时若发出 content_block_stop 会引用一个从未打开的块，
					// 触发客户端 "Content block not found"。
					if i.hasToolContentStarted {
						stopEvent := StreamEvent{
							Type:  "content_block_stop",
							Index: &i.contentIndex,
						}
						data, err := json.Marshal(stopEvent)
						if err != nil {
							return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
						}
						events = append(events, formatSSEEvent("content_block_stop", data))

						i.contentIndex++
					}

					i.toolCallIndices[toolCallIndex] = true
					i.hasToolContentStarted = true

					startBlock := &MessageContentBlock{
						Type:  "tool_use",
						ID:    deltaToolCall.ID,
						Name:  &deltaToolCall.Function.Name,
						Input: json.RawMessage("{}"),
					}
					if ext := geminiThoughtSignatureExtension(deltaToolCall.ThoughtSignature); ext != nil {
						startBlock.Octopus = ext
					}

					startEvent := StreamEvent{
						Type:         "content_block_start",
						Index:        &i.contentIndex,
						ContentBlock: startBlock,
					}
					data, err := json.Marshal(startEvent)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal content_block_start event: %w", err)
					}
					events = append(events, formatSSEEvent("content_block_start", data))

					// If the tool call has arguments, we need to generate a content_block_delta
					if deltaToolCall.Function.Arguments != "" {
						deltaEvent := StreamEvent{
							Type:  "content_block_delta",
							Index: &i.contentIndex,
							Delta: &StreamDelta{
								Type:        lo.ToPtr("input_json_delta"),
								PartialJSON: &deltaToolCall.Function.Arguments,
							},
						}
						data, err := json.Marshal(deltaEvent)
						if err != nil {
							return nil, fmt.Errorf("failed to marshal content_block_delta event: %w", err)
						}
						events = append(events, formatSSEEvent("content_block_delta", data))
					}
				} else {
					// Generate content_block_delta for input_json_delta
					deltaEvent := StreamEvent{
						Type:  "content_block_delta",
						Index: &i.contentIndex,
						Delta: &StreamDelta{
							Type:        lo.ToPtr("input_json_delta"),
							PartialJSON: &deltaToolCall.Function.Arguments,
						},
					}
					data, err := json.Marshal(deltaEvent)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal content_block_delta event: %w", err)
					}
					events = append(events, formatSSEEvent("content_block_delta", data))
				}
			}
		}

		// Handle finish reason
		if choice.FinishReason != nil && !i.hasFinished {
			i.hasFinished = true

			if i.hasOpenContentBlock() {
				stopEvent := StreamEvent{
					Type:  "content_block_stop",
					Index: &i.contentIndex,
				}
				data, err := json.Marshal(stopEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_stop", data))
				i.resetOpenContentState()
			}

			// Convert finish reason to Anthropic format
			stopReason := model.ParseFinishReason(*choice.FinishReason).ToAnthropic()
			if stopReason == "" {
				stopReason = "end_turn"
			}

			// Store the stop reason, but don't generate message_delta yet
			// We'll wait for the usage chunk to combine them
			i.stopReason = &stopReason
			if choice.StopSequence != nil {
				i.stopSequence = choice.StopSequence
			}
		}
	}

	// Handle usage chunk after finish_reason
	if stream.Usage != nil && i.hasFinished && !i.messageStopped {
		finalEvents, err := i.finalizeStreamMessage(stream.Usage)
		if err != nil {
			return nil, err
		}
		events = append(events, finalEvents...)
	}

	if len(events) == 0 {
		return nil, nil
	}

	return joinSSEEvents(events), nil
}

func (i *MessagesInbound) TransformStreamEvents(ctx context.Context, events []model.StreamEvent) ([]byte, error) {
	if len(events) == 0 {
		return nil, nil
	}
	if stream := model.InternalResponseFromStreamEvents(events); stream != nil && stream.Object != "[DONE]" {
		i.streamAggregator.Add(stream)
	}

	var firstUsage *model.Usage
	for _, event := range events {
		if event.Usage != nil {
			firstUsage = event.Usage
			break
		}
	}

	var out [][]byte
	ensureStarted := func(event model.StreamEvent) error {
		if event.ID != "" {
			i.messageID = event.ID
		}
		if event.Model != "" {
			i.modelName = event.Model
		}
		if i.hasStarted {
			return nil
		}
		i.hasStarted = true
		usage := &Usage{InputTokens: i.inputToken, OutputTokens: 1}
		if firstUsage != nil {
			usage = i.convertUsage(firstUsage)
		}
		startEvent := StreamEvent{
			Type: "message_start",
			Message: &StreamMessage{
				ID:      i.messageID,
				Type:    "message",
				Role:    "assistant",
				Model:   i.modelName,
				Content: []MessageContentBlock{},
				Usage:   usage,
			},
		}
		data, err := json.Marshal(startEvent)
		if err != nil {
			return fmt.Errorf("failed to marshal message_start event: %w", err)
		}
		out = append(out, formatSSEEvent("message_start", data))
		return nil
	}
	closeOpenBlock := func() error {
		if !i.hasOpenContentBlock() {
			return nil
		}
		stopEvent := StreamEvent{Type: "content_block_stop", Index: &i.contentIndex}
		data, err := json.Marshal(stopEvent)
		if err != nil {
			return fmt.Errorf("failed to marshal content_block_stop event: %w", err)
		}
		out = append(out, formatSSEEvent("content_block_stop", data))
		i.resetOpenContentState()
		i.contentIndex++
		return nil
	}
	startText := func() error {
		if i.hasTextContentStarted {
			return nil
		}
		if err := closeOpenBlock(); err != nil {
			return err
		}
		i.hasTextContentStarted = true
		startEvent := StreamEvent{Type: "content_block_start", Index: &i.contentIndex, ContentBlock: &MessageContentBlock{Type: "text", Text: lo.ToPtr("")}}
		data, err := json.Marshal(startEvent)
		if err != nil {
			return fmt.Errorf("failed to marshal content_block_start event: %w", err)
		}
		out = append(out, formatSSEEvent("content_block_start", data))
		return nil
	}
	startThinking := func() error {
		if i.hasThinkingContentStarted {
			return nil
		}
		if err := closeOpenBlock(); err != nil {
			return err
		}
		i.hasThinkingContentStarted = true
		startEvent := StreamEvent{Type: "content_block_start", Index: &i.contentIndex, ContentBlock: &MessageContentBlock{Type: "thinking", Thinking: lo.ToPtr(""), Signature: lo.ToPtr("")}}
		data, err := json.Marshal(startEvent)
		if err != nil {
			return fmt.Errorf("failed to marshal content_block_start event: %w", err)
		}
		out = append(out, formatSSEEvent("content_block_start", data))
		return nil
	}
	startTool := func(toolCall model.ToolCall) error {
		if i.toolCallIndices == nil {
			i.toolCallIndices = make(map[int]bool)
		}
		if i.toolCallIndices[toolCall.Index] {
			return nil
		}
		if err := closeOpenBlock(); err != nil {
			return err
		}
		i.toolCallIndices[toolCall.Index] = true
		i.hasToolContentStarted = true
		startBlock := &MessageContentBlock{Type: "tool_use", ID: toolCall.ID, Name: &toolCall.Function.Name, Input: json.RawMessage("{}")}
		if ext := geminiThoughtSignatureExtension(toolCall.GetGeminiExtensions().ThoughtSignature); ext != nil {
			startBlock.Octopus = ext
		}
		startEvent := StreamEvent{Type: "content_block_start", Index: &i.contentIndex, ContentBlock: startBlock}
		data, err := json.Marshal(startEvent)
		if err != nil {
			return fmt.Errorf("failed to marshal content_block_start event: %w", err)
		}
		out = append(out, formatSSEEvent("content_block_start", data))
		return nil
	}

	for _, event := range events {
		if event.ID != "" {
			i.messageID = event.ID
		}
		if event.Model != "" {
			i.modelName = event.Model
		}
		switch event.Kind {
		case model.StreamEventKindMessageStart:
			if err := ensureStarted(event); err != nil {
				return nil, err
			}
		case model.StreamEventKindTextDelta:
			if err := ensureStarted(event); err != nil {
				return nil, err
			}
			if event.Delta == nil || event.Delta.Text == "" {
				continue
			}
			if err := startText(); err != nil {
				return nil, err
			}
			text := event.Delta.Text
			deltaEvent := StreamEvent{Type: "content_block_delta", Index: &i.contentIndex, Delta: &StreamDelta{Type: lo.ToPtr("text_delta"), Text: &text}}
			data, err := json.Marshal(deltaEvent)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal content_block_delta event: %w", err)
			}
			out = append(out, formatSSEEvent("content_block_delta", data))
		case model.StreamEventKindThinkingDelta:
			if err := ensureStarted(event); err != nil {
				return nil, err
			}
			if event.Delta == nil {
				continue
			}
			if event.Delta.Thinking != "" {
				if err := startThinking(); err != nil {
					return nil, err
				}
				thinking := event.Delta.Thinking
				deltaEvent := StreamEvent{Type: "content_block_delta", Index: &i.contentIndex, Delta: &StreamDelta{Type: lo.ToPtr("thinking_delta"), Thinking: &thinking}}
				data, err := json.Marshal(deltaEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_delta event: %w", err)
				}
				out = append(out, formatSSEEvent("content_block_delta", data))
			}
			if event.Delta.Signature != "" {
				if err := startThinking(); err != nil {
					return nil, err
				}
				signature := event.Delta.Signature
				deltaEvent := StreamEvent{Type: "content_block_delta", Index: &i.contentIndex, Delta: &StreamDelta{Type: lo.ToPtr("signature_delta"), Signature: &signature}}
				data, err := json.Marshal(deltaEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal signature_delta event: %w", err)
				}
				out = append(out, formatSSEEvent("content_block_delta", data))
			}
		case model.StreamEventKindSignatureDelta:
			if err := ensureStarted(event); err != nil {
				return nil, err
			}
			if event.Delta == nil || event.Delta.Signature == "" {
				continue
			}
			if err := startThinking(); err != nil {
				return nil, err
			}
			signature := event.Delta.Signature
			deltaEvent := StreamEvent{Type: "content_block_delta", Index: &i.contentIndex, Delta: &StreamDelta{Type: lo.ToPtr("signature_delta"), Signature: &signature}}
			data, err := json.Marshal(deltaEvent)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal signature_delta event: %w", err)
			}
			out = append(out, formatSSEEvent("content_block_delta", data))
		case model.StreamEventKindContentBlockStart:
			if err := ensureStarted(event); err != nil {
				return nil, err
			}
			if event.ContentBlock == nil || event.ContentBlock.Type != "redacted_thinking" || event.ContentBlock.Data == "" {
				continue
			}
			if err := closeOpenBlock(); err != nil {
				return nil, err
			}
			startEvent := StreamEvent{Type: "content_block_start", Index: &i.contentIndex, ContentBlock: &MessageContentBlock{Type: "redacted_thinking", Data: event.ContentBlock.Data}}
			data, err := json.Marshal(startEvent)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal content_block_start event: %w", err)
			}
			out = append(out, formatSSEEvent("content_block_start", data))
			stopEvent := StreamEvent{Type: "content_block_stop", Index: &i.contentIndex}
			stopData, err := json.Marshal(stopEvent)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
			}
			out = append(out, formatSSEEvent("content_block_stop", stopData))
			i.contentIndex++
		case model.StreamEventKindToolCallStart:
			if err := ensureStarted(event); err != nil {
				return nil, err
			}
			if event.ToolCall != nil {
				if err := startTool(*event.ToolCall); err != nil {
					return nil, err
				}
			}
		case model.StreamEventKindToolCallDelta:
			if err := ensureStarted(event); err != nil {
				return nil, err
			}
			if event.ToolCall == nil {
				continue
			}
			if err := startTool(*event.ToolCall); err != nil {
				return nil, err
			}
			arguments := event.ToolCall.Function.Arguments
			if event.Delta != nil && event.Delta.Arguments != "" {
				arguments = event.Delta.Arguments
			}
			if arguments == "" {
				continue
			}
			deltaEvent := StreamEvent{Type: "content_block_delta", Index: &i.contentIndex, Delta: &StreamDelta{Type: lo.ToPtr("input_json_delta"), PartialJSON: &arguments}}
			data, err := json.Marshal(deltaEvent)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal content_block_delta event: %w", err)
			}
			out = append(out, formatSSEEvent("content_block_delta", data))
		case model.StreamEventKindToolCallStop, model.StreamEventKindContentBlockStop:
			if err := closeOpenBlock(); err != nil {
				return nil, err
			}
		case model.StreamEventKindMessageStop:
			if err := ensureStarted(event); err != nil {
				return nil, err
			}
			if err := closeOpenBlock(); err != nil {
				return nil, err
			}
			stopReason := event.StopReason.ToAnthropic()
			if stopReason == "" {
				stopReason = "end_turn"
			}
			i.stopReason = &stopReason
			i.stopSequence = event.StopSequence
			i.hasFinished = true
		case model.StreamEventKindUsageDelta:
			if event.Usage != nil && i.hasFinished && !i.messageStopped {
				finalEvents, err := i.finalizeStreamMessage(event.Usage)
				if err != nil {
					return nil, err
				}
				out = append(out, finalEvents...)
			}
		case model.StreamEventKindDone:
			if i.hasFinished && !i.messageStopped {
				finalEvents, err := i.finalizeStreamMessage(nil)
				if err != nil {
					return nil, err
				}
				out = append(out, finalEvents...)
			}
		case model.StreamEventKindError:
			if event.Error == nil {
				continue
			}
			errType := event.Error.Detail.Type
			if errType == "" {
				errType = "api_error"
			}
			errPayload := StreamEvent{Type: "error", Error: &ErrorDetail{Type: errType, Message: event.Error.Detail.Message}}
			data, err := json.Marshal(errPayload)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal error event: %w", err)
			}
			i.messageStopped = true
			out = append(out, formatSSEEvent("error", data))
		}
	}

	if len(out) == 0 {
		return nil, nil
	}
	return joinSSEEvents(out), nil
}

func (i *MessagesInbound) hasOpenContentBlock() bool {
	return i.hasTextContentStarted || i.hasThinkingContentStarted || i.hasToolContentStarted
}

func (i *MessagesInbound) resetOpenContentState() {
	i.hasTextContentStarted = false
	i.hasThinkingContentStarted = false
	i.hasToolContentStarted = false
}

func (i *MessagesInbound) finalizeStreamMessage(usage *model.Usage) ([][]byte, error) {
	if i.messageStopped {
		return nil, nil
	}

	msgDeltaEvent := StreamEvent{
		Type: "message_delta",
	}

	if i.stopReason != nil {
		msgDeltaEvent.Delta = &StreamDelta{
			StopReason:   i.stopReason,
			StopSequence: i.stopSequence,
		}
	}

	if usage != nil {
		msgDeltaEvent.Usage = i.convertUsage(usage)
	}

	data, err := json.Marshal(msgDeltaEvent)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message_delta event: %w", err)
	}

	msgStopEvent := StreamEvent{
		Type: "message_stop",
	}
	stopData, err := json.Marshal(msgStopEvent)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message_stop event: %w", err)
	}

	i.messageStopped = true
	return [][]byte{
		formatSSEEvent("message_delta", data),
		formatSSEEvent("message_stop", stopData),
	}, nil
}

func joinSSEEvents(events [][]byte) []byte {
	result := make([]byte, 0)
	for idx, event := range events {
		if idx > 0 {
			result = append(result, '\n')
		}
		result = append(result, event...)
	}
	return result
}

func (i *MessagesInbound) convertUsage(usage *model.Usage) *Usage {
	anthropicUsage := &Usage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
	}
	if usage.HasAnthropicCacheSemantic() {
		anthropicUsage.CacheCreationInputTokens = usage.CacheCreationInputTokens
		anthropicUsage.CacheReadInputTokens = usage.CacheReadInputTokens
		if usage.CacheCreation5mInputTokens > 0 || usage.CacheCreation1hInputTokens > 0 {
			anthropicUsage.CacheCreation = &CacheCreationUsage{
				Ephemeral5mInputTokens: usage.CacheCreation5mInputTokens,
				Ephemeral1hInputTokens: usage.CacheCreation1hInputTokens,
			}
		}
	} else if usage.PromptTokensDetails != nil && usage.PromptTokensDetails.CachedTokens > 0 {
		anthropicUsage.CacheReadInputTokens = usage.PromptTokensDetails.CachedTokens
		anthropicUsage.InputTokens -= anthropicUsage.CacheReadInputTokens
		if anthropicUsage.InputTokens < 0 {
			anthropicUsage.InputTokens = 0
		}
	}
	return anthropicUsage
}

// GetInternalResponse returns the complete internal response for logging, statistics, etc.
// For streaming: aggregates all stored stream chunks into a complete response
// For non-streaming: returns the stored response
func (i *MessagesInbound) GetInternalResponse(ctx context.Context) (*model.InternalLLMResponse, error) {
	if i.storedResponse != nil {
		return i.storedResponse, nil
	}
	return i.streamAggregator.BuildAndReset(), nil
}

// mergeToolCall merges a tool call delta into the existing tool calls slice
func mergeToolCall(toolCalls []model.ToolCall, delta model.ToolCall) []model.ToolCall {
	// Find existing tool call by index
	for i, tc := range toolCalls {
		if tc.Index == delta.Index {
			// Merge the delta into existing tool call
			if delta.ID != "" {
				toolCalls[i].ID = delta.ID
			}
			if delta.Type != "" {
				toolCalls[i].Type = delta.Type
			}
			if delta.Function.Name != "" {
				toolCalls[i].Function.Name += delta.Function.Name
			}
			if delta.Function.Arguments != "" {
				toolCalls[i].Function.Arguments += delta.Function.Arguments
			}
			return toolCalls
		}
	}

	// New tool call, add it
	return append(toolCalls, delta)
}

// formatSSEEvent 格式化为完整的 SSE 事件格式
func formatSSEEvent(eventType string, data []byte) []byte {
	return []byte(fmt.Sprintf("event:%s\ndata:%s\n\n", eventType, string(data)))
}
