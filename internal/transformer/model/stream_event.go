package model

import "encoding/json"

type StreamEventKind string

const (
	StreamEventKindMessageStart      StreamEventKind = "message_start"
	StreamEventKindContentBlockStart StreamEventKind = "content_block_start"
	StreamEventKindContentBlockStop  StreamEventKind = "content_block_stop"
	StreamEventKindTextDelta         StreamEventKind = "text_delta"
	StreamEventKindThinkingDelta     StreamEventKind = "thinking_delta"
	StreamEventKindSignatureDelta    StreamEventKind = "signature_delta"
	StreamEventKindToolCallStart     StreamEventKind = "tool_call_start"
	StreamEventKindToolCallDelta     StreamEventKind = "tool_call_delta"
	StreamEventKindToolCallStop      StreamEventKind = "tool_call_stop"
	StreamEventKindUsageDelta        StreamEventKind = "usage_delta"
	StreamEventKindMessageStop       StreamEventKind = "message_stop"
	StreamEventKindDone              StreamEventKind = "done"
	StreamEventKindError             StreamEventKind = "error"
)

type StreamEvent struct {
	Kind StreamEventKind `json:"kind"`

	ID    string `json:"id,omitempty"`
	Model string `json:"model,omitempty"`
	Index int    `json:"index,omitempty"`
	Role  string `json:"role,omitempty"`

	ContentBlock *StreamContentBlock `json:"content_block,omitempty"`
	Delta        *StreamDelta        `json:"delta,omitempty"`
	ToolCall     *ToolCall           `json:"tool_call,omitempty"`
	Usage        *Usage              `json:"usage,omitempty"`
	StopReason   FinishReason        `json:"stop_reason,omitempty"`
	StopSequence *string             `json:"stop_sequence,omitempty"`
	Error        *ResponseError      `json:"error,omitempty"`

	ProviderExtensions *ProviderExtensions `json:"provider_extensions,omitempty"`
}

type StreamContentBlock struct {
	Type string `json:"type,omitempty"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Text string `json:"text,omitempty"`
	Data string `json:"data,omitempty"`

	Input              json.RawMessage     `json:"input,omitempty"`
	ProviderExtensions *ProviderExtensions `json:"provider_extensions,omitempty"`
}

type StreamDelta struct {
	Text      string `json:"text,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Refusal   string `json:"refusal,omitempty"`

	ProviderExtensions *ProviderExtensions `json:"provider_extensions,omitempty"`
}

func StreamEventsFromInternalResponse(response *InternalLLMResponse) []StreamEvent {
	if response == nil {
		return nil
	}
	if response.Object == "[DONE]" {
		return []StreamEvent{{Kind: StreamEventKindDone}}
	}
	if response.Error != nil {
		return []StreamEvent{{Kind: StreamEventKindError, ID: response.ID, Model: response.Model, Error: response.Error}}
	}
	events := make([]StreamEvent, 0, len(response.Choices)+1)
	for _, choice := range response.Choices {
		if choice.Delta != nil {
			delta := choice.Delta
			if delta.Role != "" {
				events = append(events, StreamEvent{Kind: StreamEventKindMessageStart, ID: response.ID, Model: response.Model, Index: choice.Index, Role: delta.Role})
			}
			for _, block := range delta.ReasoningBlocks {
				switch block.Kind {
				case ReasoningBlockKindThinking:
					if block.Text != "" {
						events = append(events, StreamEvent{Kind: StreamEventKindThinkingDelta, ID: response.ID, Model: response.Model, Index: choice.Index, Delta: &StreamDelta{Thinking: block.Text, Signature: block.Signature}})
					} else if block.Signature != "" {
						events = append(events, StreamEvent{Kind: StreamEventKindSignatureDelta, ID: response.ID, Model: response.Model, Index: choice.Index, Delta: &StreamDelta{Signature: block.Signature}})
					}
				case ReasoningBlockKindSignature:
					if block.Signature != "" {
						events = append(events, StreamEvent{Kind: StreamEventKindSignatureDelta, ID: response.ID, Model: response.Model, Index: choice.Index, Delta: &StreamDelta{Signature: block.Signature}})
					}
				case ReasoningBlockKindRedacted:
					if block.Data != "" {
						events = append(events, StreamEvent{Kind: StreamEventKindContentBlockStart, ID: response.ID, Model: response.Model, Index: choice.Index, ContentBlock: &StreamContentBlock{Type: string(ReasoningBlockKindRedacted), Data: block.Data}})
						events = append(events, StreamEvent{Kind: StreamEventKindContentBlockStop, ID: response.ID, Model: response.Model, Index: choice.Index, ContentBlock: &StreamContentBlock{Type: string(ReasoningBlockKindRedacted)}})
					}
				}
			}
			if len(delta.ReasoningBlocks) == 0 {
				if reasoning := delta.GetReasoningContent(); reasoning != "" {
					events = append(events, StreamEvent{Kind: StreamEventKindThinkingDelta, ID: response.ID, Model: response.Model, Index: choice.Index, Delta: &StreamDelta{Thinking: reasoning}})
				}
				if delta.ReasoningSignature != nil && *delta.ReasoningSignature != "" {
					events = append(events, StreamEvent{Kind: StreamEventKindSignatureDelta, ID: response.ID, Model: response.Model, Index: choice.Index, Delta: &StreamDelta{Signature: *delta.ReasoningSignature}})
				}
				for _, data := range delta.RedactedThinkingBlocks {
					if data != "" {
						events = append(events, StreamEvent{Kind: StreamEventKindContentBlockStart, ID: response.ID, Model: response.Model, Index: choice.Index, ContentBlock: &StreamContentBlock{Type: string(ReasoningBlockKindRedacted), Data: data}})
						events = append(events, StreamEvent{Kind: StreamEventKindContentBlockStop, ID: response.ID, Model: response.Model, Index: choice.Index, ContentBlock: &StreamContentBlock{Type: string(ReasoningBlockKindRedacted)}})
					}
				}
			}
			if delta.Content.Content != nil && *delta.Content.Content != "" {
				events = append(events, StreamEvent{Kind: StreamEventKindTextDelta, ID: response.ID, Model: response.Model, Index: choice.Index, Delta: &StreamDelta{Text: *delta.Content.Content}})
			}
			if delta.Refusal != "" {
				events = append(events, StreamEvent{Kind: StreamEventKindTextDelta, ID: response.ID, Model: response.Model, Index: choice.Index, Delta: &StreamDelta{Refusal: delta.Refusal}})
			}
			for _, toolCall := range delta.ToolCalls {
				toolCall := toolCall
				event := StreamEvent{Kind: StreamEventKindToolCallDelta, ID: response.ID, Model: response.Model, Index: toolCall.Index, ToolCall: &toolCall}
				if toolCall.Function.Arguments != "" {
					event.Delta = &StreamDelta{Arguments: toolCall.Function.Arguments}
				}
				events = append(events, event)
			}
		}
		if choice.FinishReason != nil {
			event := StreamEvent{Kind: StreamEventKindMessageStop, ID: response.ID, Model: response.Model, Index: choice.Index, StopReason: ParseFinishReason(*choice.FinishReason), StopSequence: choice.StopSequence}
			if len(response.RawResponsesOutputItems) > 0 {
				event.ProviderExtensions = &ProviderExtensions{OpenAI: &OpenAIExtension{RawResponseItems: response.RawResponsesOutputItems}}
			}
			events = append(events, event)
		}
	}
	if response.Usage != nil {
		event := StreamEvent{Kind: StreamEventKindUsageDelta, ID: response.ID, Model: response.Model, Usage: response.Usage}
		if len(response.RawResponsesOutputItems) > 0 {
			event.ProviderExtensions = &ProviderExtensions{OpenAI: &OpenAIExtension{RawResponseItems: response.RawResponsesOutputItems}}
		}
		events = append(events, event)
	}
	return events
}

func InternalResponseFromStreamEvents(events []StreamEvent) *InternalLLMResponse {
	if len(events) == 0 {
		return nil
	}
	response := &InternalLLMResponse{Object: "chat.completion.chunk"}
	choices := make(map[int]*Choice)
	for _, event := range events {
		if event.Kind == StreamEventKindDone {
			return &InternalLLMResponse{Object: "[DONE]"}
		}
		if event.ID != "" {
			response.ID = event.ID
		}
		if event.Model != "" {
			response.Model = event.Model
		}
		if event.Usage != nil {
			response.Usage = event.Usage
		}
		if event.ProviderExtensions != nil && event.ProviderExtensions.OpenAI != nil && len(event.ProviderExtensions.OpenAI.RawResponseItems) > 0 {
			response.RawResponsesOutputItems = event.ProviderExtensions.OpenAI.RawResponseItems
		}
		if event.Kind == StreamEventKindUsageDelta {
			continue
		}
		if event.Kind == StreamEventKindError {
			response.Error = event.Error
			continue
		}
		choice := choices[event.Index]
		if choice == nil {
			choice = &Choice{Index: event.Index, Delta: &Message{}}
			choices[event.Index] = choice
		}
		switch event.Kind {
		case StreamEventKindMessageStart:
			choice.Delta.Role = event.Role
		case StreamEventKindContentBlockStart:
			if event.ContentBlock != nil && event.ContentBlock.Type == string(ReasoningBlockKindRedacted) && event.ContentBlock.Data != "" {
				choice.Delta.RedactedThinkingBlocks = append(choice.Delta.RedactedThinkingBlocks, event.ContentBlock.Data)
				choice.Delta.AppendReasoningBlock(ReasoningBlock{Kind: ReasoningBlockKindRedacted, Index: -1, Data: event.ContentBlock.Data})
			}
		case StreamEventKindTextDelta:
			if event.Delta != nil {
				if event.Delta.Text != "" {
					text := event.Delta.Text
					choice.Delta.Content.Content = &text
				}
				if event.Delta.Refusal != "" {
					choice.Delta.Refusal = event.Delta.Refusal
				}
			}
		case StreamEventKindThinkingDelta:
			if event.Delta != nil && (event.Delta.Thinking != "" || event.Delta.Signature != "") {
				if event.Delta.Thinking != "" {
					thinking := event.Delta.Thinking
					choice.Delta.ReasoningContent = &thinking
				}
				choice.Delta.AppendReasoningBlock(ReasoningBlock{Kind: ReasoningBlockKindThinking, Index: -1, Text: event.Delta.Thinking, Signature: event.Delta.Signature})
			}
		case StreamEventKindSignatureDelta:
			if event.Delta != nil && event.Delta.Signature != "" {
				signature := event.Delta.Signature
				choice.Delta.ReasoningSignature = &signature
				choice.Delta.AppendReasoningBlock(ReasoningBlock{Kind: ReasoningBlockKindSignature, Index: -1, Signature: signature})
			}
		case StreamEventKindToolCallStart, StreamEventKindToolCallDelta:
			if event.ToolCall != nil {
				toolCall := *event.ToolCall
				if event.Delta != nil && event.Delta.Arguments != "" {
					toolCall.Function.Arguments = event.Delta.Arguments
				}
				choice.Delta.ToolCalls = MergeToolCallDelta(choice.Delta.ToolCalls, toolCall)
			}
		case StreamEventKindMessageStop:
			if event.StopReason != "" {
				reason := event.StopReason.String()
				choice.FinishReason = &reason
			}
			choice.StopSequence = event.StopSequence
		}
	}
	for idx := 0; idx < len(choices); idx++ {
		if choice := choices[idx]; choice != nil {
			response.Choices = append(response.Choices, *choice)
		}
	}
	if len(response.Choices) == 0 && response.Usage == nil && response.Error == nil {
		return nil
	}
	return response
}
