package relay

import (
	"encoding/json"
	"maps"
	"net/url"
	"strings"

	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
)

type wsConversationState struct {
	DownstreamSessionID string
	RequestModel   string
	ChannelID      int
	ChannelKeyID   int
	LastResponseID string
	ReplayWindowItems json.RawMessage
	Transcript     []transformerModel.Message
	ReplayAliases  []string
	ReplayPending  bool
}

func (s *wsConversationState) MatchesRequestModel(requestModel string) bool {
	if s == nil {
		return false
	}
	return strings.TrimSpace(s.RequestModel) == strings.TrimSpace(requestModel)
}

func (s *wsConversationState) CanAutoRestart(req *transformerModel.InternalLLMRequest) bool {
	if s == nil || req == nil {
		return false
	}
	if len(s.ReplayWindowItems) > 0 {
		if strings.TrimSpace(s.LastResponseID) == "" {
			return false
		}
		if req.PreviousResponseID == nil {
			return true
		}
		prevID := strings.TrimSpace(*req.PreviousResponseID)
		return prevID == "" || s.MatchesPreviousResponseID(prevID)
	}
	if s.ReplayPending && requestContainsToolOutputs(req) {
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
	return prevID == "" || s.MatchesPreviousResponseID(prevID)
}

func (s *wsConversationState) MatchesPreviousResponseID(responseID string) bool {
	if s == nil {
		return false
	}
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return false
	}
	if responseID == strings.TrimSpace(s.LastResponseID) {
		return true
	}
	for _, alias := range s.ReplayAliases {
		if responseID == strings.TrimSpace(alias) {
			return true
		}
	}
	return false
}

func (s *wsConversationState) ShouldRewritePreviousResponseID(responseID string) bool {
	if s == nil {
		return false
	}
	responseID = strings.TrimSpace(responseID)
	if responseID == "" || responseID == strings.TrimSpace(s.LastResponseID) {
		return false
	}
	for _, alias := range s.ReplayAliases {
		if responseID == strings.TrimSpace(alias) {
			return true
		}
	}
	return false
}

func (s *wsConversationState) ShouldUseNativeContinuation(req *transformerModel.InternalLLMRequest) bool {
	return false
}

func (s *wsConversationState) ShouldUseLocalReplay(req *transformerModel.InternalLLMRequest) bool {
	if s == nil || req == nil {
		return false
	}
	if len(s.ReplayWindowItems) == 0 || strings.TrimSpace(s.LastResponseID) == "" {
		return false
	}
	if req.PreviousResponseID == nil {
		return true
	}
	prevID := strings.TrimSpace(*req.PreviousResponseID)
	return prevID == "" || s.MatchesPreviousResponseID(prevID)
}

func (s *wsConversationState) MarkReplayRecovered(req *transformerModel.InternalLLMRequest) {
	if s == nil {
		return
	}
	s.ReplayPending = requestContainsToolOutputs(req)
}

func (s *wsConversationState) MarkNativeContinuationReady() {
	if s == nil {
		return
	}
	s.ReplayPending = false
}

func (s *wsConversationState) RememberReplayAlias(responseID string) {
	if s == nil {
		return
	}
	responseID = strings.TrimSpace(responseID)
	if responseID == "" || responseID == strings.TrimSpace(s.LastResponseID) {
		return
	}
	filtered := make([]string, 0, len(s.ReplayAliases)+1)
	filtered = append(filtered, responseID)
	for _, alias := range s.ReplayAliases {
		alias = strings.TrimSpace(alias)
		if alias == "" || alias == responseID {
			continue
		}
		filtered = append(filtered, alias)
		if len(filtered) >= 8 {
			break
		}
	}
	s.ReplayAliases = filtered
}

func (s *wsConversationState) BuildReplayRequest(req *transformerModel.InternalLLMRequest) *transformerModel.InternalLLMRequest {
	if s == nil || req == nil {
		return nil
	}
	replayed := cloneInternalRequest(req)
	replayed.PreviousResponseID = nil
	replayed.Conversation = nil
	replayed.RawInputItems = nil
	replayed.Messages = retainInstructionMessages(req.Messages)
	if mergedRawInputItems, ok := buildReplayRawInputItems(s.ReplayWindowItems, s.Transcript, req.RawInputItems, req.Messages); ok {
		replayed.RawInputItems = mergedRawInputItems
		replayed.TransformOptions.ArrayInputs = boolPtr(true)
	}
	if replayed.TransformerMetadata == nil {
		replayed.TransformerMetadata = map[string]string{}
	}
	replayed.TransformerMetadata["octopus_ws_execution_mode"] = "replay_exact"
	return replayed
}

func (s *wsConversationState) ApplySuccessfulTurn(req *transformerModel.InternalLLMRequest, resp *transformerModel.InternalLLMResponse) {
	if s == nil || req == nil || resp == nil {
		return
	}
	s.RequestModel = strings.TrimSpace(req.Model)
	if replayWindowItems, ok := buildNextReplayWindow(s.ReplayWindowItems, req, resp); ok {
		s.ReplayWindowItems = replayWindowItems
	}
	s.Transcript = append(s.Transcript, cloneMessages(req.Messages)...)
	s.Transcript = append(s.Transcript, assistantMessagesFromResponse(resp)...)
	if respID := strings.TrimSpace(resp.ID); respID != "" {
		s.LastResponseID = respID
	}
}

func cloneWSConversationState(state *wsConversationState) *wsConversationState {
	if state == nil {
		return nil
	}
	return &wsConversationState{
		DownstreamSessionID: strings.TrimSpace(state.DownstreamSessionID),
		RequestModel:   strings.TrimSpace(state.RequestModel),
		ChannelID:      state.ChannelID,
		ChannelKeyID:   state.ChannelKeyID,
		LastResponseID: strings.TrimSpace(state.LastResponseID),
		ReplayWindowItems: append(json.RawMessage(nil), state.ReplayWindowItems...),
		Transcript:     cloneMessages(state.Transcript),
		ReplayAliases:  append([]string(nil), state.ReplayAliases...),
		ReplayPending:  state.ReplayPending,
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

func boolPtr(value bool) *bool {
	return &value
}

func retainInstructionMessages(messages []transformerModel.Message) []transformerModel.Message {
	if len(messages) == 0 {
		return nil
	}
	kept := make([]transformerModel.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "system" || msg.Role == "developer" {
			kept = append(kept, cloneMessage(msg))
		}
	}
	return kept
}

func requestContainsToolOutputs(req *transformerModel.InternalLLMRequest) bool {
	if req == nil {
		return false
	}
	for _, msg := range req.Messages {
		if msg.Role == "tool" && msg.ToolCallID != nil && strings.TrimSpace(*msg.ToolCallID) != "" {
			return true
		}
	}
	return false
}

func buildReplayRawInputItems(
	replayWindowItems json.RawMessage,
	transcript []transformerModel.Message,
	currentRawInputItems json.RawMessage,
	currentMessages []transformerModel.Message,
) (json.RawMessage, bool) {
	currentItems, ok := buildRequestInputItems(currentRawInputItems, currentMessages)
	if !ok {
		return nil, false
	}
	if len(replayWindowItems) > 0 {
		return mergeRawJSONArray(replayWindowItems, currentItems)
	}
	if len(transcript) == 0 {
		return append(json.RawMessage(nil), currentItems...), true
	}
	transcriptItems, err := openaiOutbound.MarshalResponsesInputItems(transcript)
	if err != nil {
		return nil, false
	}
	return mergeRawJSONArray(transcriptItems, currentItems)
}

func buildRequestInputItems(currentRawInputItems json.RawMessage, currentMessages []transformerModel.Message) (json.RawMessage, bool) {
	if len(currentRawInputItems) > 0 {
		return append(json.RawMessage(nil), currentRawInputItems...), true
	}
	currentItems, err := openaiOutbound.MarshalResponsesInputItems(currentMessages)
	if err != nil || len(currentItems) == 0 {
		return nil, false
	}
	return currentItems, true
}

func buildNextReplayWindow(existing json.RawMessage, req *transformerModel.InternalLLMRequest, resp *transformerModel.InternalLLMResponse) (json.RawMessage, bool) {
	if req == nil || resp == nil {
		return nil, false
	}
	var base json.RawMessage
	if isExactReplayRequest(req) && len(req.RawInputItems) > 0 {
		base = append(json.RawMessage(nil), req.RawInputItems...)
	} else if currentItems, ok := buildRequestInputItems(req.RawInputItems, req.Messages); ok {
		if len(existing) > 0 {
			merged, ok := mergeRawJSONArray(existing, currentItems)
			if !ok {
				return nil, false
			}
			base = merged
		} else {
			base = currentItems
		}
	} else if len(existing) > 0 {
		base = append(json.RawMessage(nil), existing...)
	}
	if len(base) == 0 {
		return nil, false
	}
	if len(resp.RawResponsesOutputItems) == 0 {
		return base, true
	}
	return mergeRawJSONArray(base, resp.RawResponsesOutputItems)
}

func mergeRawJSONArray(parts ...json.RawMessage) (json.RawMessage, bool) {
	mergedItems := make([]json.RawMessage, 0)
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		decoded, err := decodeRawJSONArray(part)
		if err != nil {
			return nil, false
		}
		mergedItems = append(mergedItems, decoded...)
	}
	if len(mergedItems) == 0 {
		return nil, false
	}
	data, err := json.Marshal(mergedItems)
	if err != nil {
		return nil, false
	}
	return data, true
}

func isExactReplayRequest(req *transformerModel.InternalLLMRequest) bool {
	if req == nil || req.TransformerMetadata == nil {
		return false
	}
	return strings.TrimSpace(req.TransformerMetadata["octopus_ws_execution_mode"]) == "replay_exact"
}

func decodeRawJSONArray(data json.RawMessage) ([]json.RawMessage, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}
