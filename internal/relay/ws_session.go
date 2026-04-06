package relay

import (
	"maps"
	"net/url"
	"strings"

	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

type wsConversationState struct {
	LastResponseID string
	Transcript     []transformerModel.Message
}

func (s *wsConversationState) CanAutoRestart(req *transformerModel.InternalLLMRequest) bool {
	if s == nil || req == nil {
		return false
	}
	if strings.TrimSpace(s.LastResponseID) == "" || len(s.Transcript) == 0 {
		return false
	}
	if !requiresUpstreamWSContinuation(req) {
		return false
	}
	if req.PreviousResponseID == nil {
		return true
	}
	prevID := strings.TrimSpace(*req.PreviousResponseID)
	return prevID == "" || prevID == strings.TrimSpace(s.LastResponseID)
}

func (s *wsConversationState) BuildReplayRequest(req *transformerModel.InternalLLMRequest) *transformerModel.InternalLLMRequest {
	if s == nil || req == nil {
		return nil
	}
	replayed := cloneInternalRequest(req)
	replayed.Messages = append(cloneMessages(s.Transcript), cloneMessages(req.Messages)...)
	replayed.PreviousResponseID = nil
	replayed.Conversation = nil
	return replayed
}

func (s *wsConversationState) ApplySuccessfulTurn(req *transformerModel.InternalLLMRequest, resp *transformerModel.InternalLLMResponse) {
	if s == nil || req == nil || resp == nil {
		return
	}
	s.Transcript = append(s.Transcript, cloneMessages(req.Messages)...)
	s.Transcript = append(s.Transcript, assistantMessagesFromResponse(resp)...)
	if respID := strings.TrimSpace(resp.ID); respID != "" {
		s.LastResponseID = respID
	}
}

func assistantMessagesFromResponse(resp *transformerModel.InternalLLMResponse) []transformerModel.Message {
	if resp == nil || len(resp.Choices) == 0 {
		return nil
	}
	result := make([]transformerModel.Message, 0, len(resp.Choices))
	for _, choice := range resp.Choices {
		if choice.Message == nil {
			continue
		}
		result = append(result, cloneMessage(*choice.Message))
	}
	return result
}

func cloneInternalRequest(req *transformerModel.InternalLLMRequest) *transformerModel.InternalLLMRequest {
	if req == nil {
		return nil
	}
	cloned := *req
	cloned.Messages = cloneMessages(req.Messages)
	cloned.Modalities = append([]string(nil), req.Modalities...)
	cloned.Tools = append([]transformerModel.Tool(nil), req.Tools...)
	cloned.Include = append([]string(nil), req.Include...)
	cloned.LogitBias = maps.Clone(req.LogitBias)
	cloned.Metadata = maps.Clone(req.Metadata)
	cloned.TransformerMetadata = maps.Clone(req.TransformerMetadata)
	cloned.Query = cloneQuery(req.Query)
	cloned.RawRequest = append([]byte(nil), req.RawRequest...)
	cloned.ExtraBody = append([]byte(nil), req.ExtraBody...)
	cloned.Prompt = append([]byte(nil), req.Prompt...)
	cloned.Conversation = append([]byte(nil), req.Conversation...)
	cloned.ContextManagement = append([]byte(nil), req.ContextManagement...)
	cloned.ResponsesStreamOptions = append([]byte(nil), req.ResponsesStreamOptions...)
	cloned.RawInputItems = append([]byte(nil), req.RawInputItems...)
	return &cloned
}

func cloneMessages(messages []transformerModel.Message) []transformerModel.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]transformerModel.Message, len(messages))
	for i, message := range messages {
		cloned[i] = cloneMessage(message)
	}
	return cloned
}

func cloneMessage(message transformerModel.Message) transformerModel.Message {
	cloned := message
	cloned.Name = cloneStringPointer(message.Name)
	cloned.ToolCallID = cloneStringPointer(message.ToolCallID)
	cloned.ToolCallName = cloneStringPointer(message.ToolCallName)
	cloned.ReasoningContent = cloneStringPointer(message.ReasoningContent)
	cloned.Reasoning = cloneStringPointer(message.Reasoning)
	cloned.ReasoningSignature = cloneStringPointer(message.ReasoningSignature)
	cloned.ToolCallIsError = cloneBoolPointer(message.ToolCallIsError)
	cloned.Content = cloneMessageContent(message.Content)
	cloned.ToolCalls = append([]transformerModel.ToolCall(nil), message.ToolCalls...)
	cloned.Images = cloneContentParts(message.Images)
	cloned.RedactedThinkingBlocks = append([]string(nil), message.RedactedThinkingBlocks...)
	if message.Audio != nil {
		audio := *message.Audio
		cloned.Audio = &audio
	}
	return cloned
}

func cloneMessageContent(content transformerModel.MessageContent) transformerModel.MessageContent {
	return transformerModel.MessageContent{
		Content:         cloneStringPointer(content.Content),
		MultipleContent: cloneContentParts(content.MultipleContent),
	}
}

func cloneContentParts(parts []transformerModel.MessageContentPart) []transformerModel.MessageContentPart {
	if len(parts) == 0 {
		return nil
	}
	cloned := make([]transformerModel.MessageContentPart, len(parts))
	for i, part := range parts {
		cloned[i] = part
		cloned[i].Text = cloneStringPointer(part.Text)
		if part.ImageURL != nil {
			imageURL := *part.ImageURL
			imageURL.Detail = cloneStringPointer(part.ImageURL.Detail)
			cloned[i].ImageURL = &imageURL
		}
		if part.Audio != nil {
			audio := *part.Audio
			cloned[i].Audio = &audio
		}
		if part.File != nil {
			file := *part.File
			cloned[i].File = &file
		}
	}
	return cloned
}

func cloneQuery(values url.Values) url.Values {
	if len(values) == 0 {
		return nil
	}
	cloned := make(url.Values, len(values))
	for key, value := range values {
		cloned[key] = append([]string(nil), value...)
	}
	return cloned
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
