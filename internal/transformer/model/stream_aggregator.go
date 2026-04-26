package model

type StreamAggregator struct {
	chunks []*InternalLLMResponse
}

func (a *StreamAggregator) Add(chunk *InternalLLMResponse) {
	if chunk == nil || chunk.Object == "[DONE]" {
		return
	}
	a.chunks = append(a.chunks, chunk)
}

func (a *StreamAggregator) Reset() {
	a.chunks = nil
}

func (a *StreamAggregator) Response() *InternalLLMResponse {
	if a == nil || len(a.chunks) == 0 {
		return nil
	}

	firstChunk := a.chunks[0]
	result := &InternalLLMResponse{
		ID:                firstChunk.ID,
		Object:            "chat.completion",
		Created:           firstChunk.Created,
		Model:             firstChunk.Model,
		SystemFingerprint: firstChunk.SystemFingerprint,
		ServiceTier:       firstChunk.ServiceTier,
	}
	choicesMap := make(map[int]*Choice)

	for _, chunk := range a.chunks {
		if chunk == nil {
			continue
		}
		if chunk.ID != "" {
			result.ID = chunk.ID
		}
		if chunk.Model != "" {
			result.Model = chunk.Model
		}
		if chunk.Usage != nil {
			result.Usage = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			existingChoice := choicesMap[choice.Index]
			if existingChoice == nil {
				existingChoice = &Choice{Index: choice.Index, Message: &Message{}}
				choicesMap[choice.Index] = existingChoice
			}
			mergeChoiceDelta(existingChoice, choice)
		}
	}

	result.Choices = make([]Choice, 0, len(choicesMap))
	for idx := 0; idx < len(choicesMap); idx++ {
		if choice := choicesMap[idx]; choice != nil {
			result.Choices = append(result.Choices, *choice)
		}
	}
	return result
}

func (a *StreamAggregator) BuildAndReset() *InternalLLMResponse {
	response := a.Response()
	a.Reset()
	return response
}

func mergeChoiceDelta(existingChoice *Choice, choice Choice) {
	if choice.Delta != nil {
		delta := choice.Delta
		if delta.Role != "" {
			existingChoice.Message.Role = delta.Role
		}
		if delta.Content.Content != nil {
			if existingChoice.Message.Content.Content == nil {
				existingChoice.Message.Content.Content = new(string)
			}
			*existingChoice.Message.Content.Content += *delta.Content.Content
		}
		if len(delta.Content.MultipleContent) > 0 {
			existingChoice.Message.Content.MultipleContent = append(existingChoice.Message.Content.MultipleContent, delta.Content.MultipleContent...)
		}
		if len(delta.Images) > 0 {
			existingChoice.Message.Content.MultipleContent = append(existingChoice.Message.Content.MultipleContent, delta.Images...)
		}
		if delta.Audio != nil {
			if existingChoice.Message.Audio == nil {
				existingChoice.Message.Audio = &struct {
					Data       string `json:"data,omitempty"`
					ExpiresAt  int64  `json:"expires_at,omitempty"`
					ID         string `json:"id,omitempty"`
					Transcript string `json:"transcript,omitempty"`
				}{}
			}
			if delta.Audio.ID != "" {
				existingChoice.Message.Audio.ID = delta.Audio.ID
			}
			if delta.Audio.ExpiresAt > 0 {
				existingChoice.Message.Audio.ExpiresAt = delta.Audio.ExpiresAt
			}
			existingChoice.Message.Audio.Data += delta.Audio.Data
			existingChoice.Message.Audio.Transcript += delta.Audio.Transcript
		}
		if reasoning := delta.GetReasoningContent(); reasoning != "" {
			if existingChoice.Message.ReasoningContent == nil {
				existingChoice.Message.ReasoningContent = new(string)
			}
			*existingChoice.Message.ReasoningContent += reasoning
		}
		for _, toolCall := range delta.ToolCalls {
			existingChoice.Message.ToolCalls = MergeToolCallDelta(existingChoice.Message.ToolCalls, toolCall)
		}
		if delta.Refusal != "" {
			existingChoice.Message.Refusal = delta.Refusal
		}
	}
	if choice.FinishReason != nil {
		existingChoice.FinishReason = choice.FinishReason
	}
	if choice.Logprobs != nil {
		if existingChoice.Logprobs == nil {
			existingChoice.Logprobs = &LogprobsContent{}
		}
		existingChoice.Logprobs.Content = append(existingChoice.Logprobs.Content, choice.Logprobs.Content...)
	}
}

func MergeToolCallDelta(toolCalls []ToolCall, delta ToolCall) []ToolCall {
	for i, tc := range toolCalls {
		if tc.Index == delta.Index {
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
	return append(toolCalls, delta)
}
