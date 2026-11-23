package relay

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/url"
	"slices"
	"strings"

	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
	"github.com/tmaxmax/go-sse"
)

type wsConversationState struct {
	RequestModel      string
	ChannelID         int
	ChannelKeyID      int
	LastResponseID    string
	ReplayWindowItems json.RawMessage
	Transcript        []transformerModel.Message
}

// BuildReplayRequest transforms a continuation request into a self-contained replay request
// by merging historical context from the stored state and removing previous_response_id.
//
// Returns nil if the transformation fails (e.g., unable to build replay raw input items).
// Callers should fall back to the original request when nil is returned.
func (s *wsConversationState) BuildReplayRequest(req *transformerModel.InternalLLMRequest) *transformerModel.InternalLLMRequest {
	if s == nil || req == nil {
		return nil
	}
	replayed := cloneInternalRequest(req)
	responsesOptions := replayed.GetOpenAIResponsesOptions()
	responsesOptions.PreviousResponseID = nil
	responsesOptions.Conversation = nil
	responsesOptions.RawInputItems = nil
	replayed.SetOpenAIResponsesOptions(responsesOptions)
	replayed.SetOpenAIRawInputItems(nil)
	// Responses 渠道用下面合并好的原始条目；其他渠道从 Messages 生成请求，所以 Messages 也要是完整对话。
	// 顶层的 instructions 只用这一轮的（上一轮的不会带到下一轮），放在最前面。
	instructions, input := splitInstructions(req)
	replayed.Messages = cloneMessages(slices.Concat(instructions, s.Transcript, input))

	// Merge historical replay window and current request into raw input items
	mergedRawInputItems, ok := buildReplayRawInputItems(s.ReplayWindowItems, s.Transcript, req.OpenAIRawInputItems(), req.Messages)
	if !ok || len(mergedRawInputItems) == 0 {
		// Merge failed - return nil to signal caller to use original request
		return nil
	}

	replayed.SetOpenAIRawInputItems(mergedRawInputItems)
	replayed.TransformOptions.ArrayInputs = boolPtr(true)
	if replayed.TransformerMetadata == nil {
		replayed.TransformerMetadata = map[string]string{}
	}
	replayed.MarkOpenAIExactReplayRequest()
	return replayed
}

// continuesUpstreamHistory 报告请求是不是接着上游存着的历史（带 previous_response_id 或 conversation）。
// 这样的请求原样转发时本地只看得到这一轮，成功了也不能拿它建存档。
func continuesUpstreamHistory(req *transformerModel.InternalLLMRequest) bool {
	return req.OpenAIPreviousResponseID() != "" || (len(req.Conversation) > 0 && string(req.Conversation) != "null")
}

func (s *wsConversationState) ApplySuccessfulTurn(req *transformerModel.InternalLLMRequest, resp *transformerModel.InternalLLMResponse) {
	if s == nil || req == nil || resp == nil {
		return
	}
	s.RequestModel = strings.TrimSpace(req.Model)
	if replayWindowItems, ok := buildNextReplayWindow(s.ReplayWindowItems, req, resp); ok {
		s.ReplayWindowItems = replayWindowItems
	}
	if req.IsOpenAIExactReplayRequest() {
		// 回放请求（HTTP 入站传进来的是它）的 Messages 已经是完整对话，不能再接在旧记录后面
		s.Transcript = nil
	}
	_, input := splitInstructions(req) // instructions 不是对话的一部分，不记
	s.Transcript = append(s.Transcript, cloneMessages(input)...)
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
		RequestModel:      strings.TrimSpace(state.RequestModel),
		ChannelID:         state.ChannelID,
		ChannelKeyID:      state.ChannelKeyID,
		LastResponseID:    strings.TrimSpace(state.LastResponseID),
		ReplayWindowItems: append(json.RawMessage(nil), state.ReplayWindowItems...),
		Transcript:        cloneMessages(state.Transcript),
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
	cloned.ProviderExtensions = transformerModel.CloneProviderExtensions(req.ProviderExtensions)
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
	cloned.ReasoningBlocks = append([]transformerModel.ReasoningBlock(nil), message.ReasoningBlocks...)
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

// splitInstructions 把请求的消息分成两段：顶层 instructions 变成的那一条（没有就是空的），和输入条目变成的其余消息。
// 输入条目里的 system / developer 消息属于后一段，它们是对话的一部分。
func splitInstructions(req *transformerModel.InternalLLMRequest) (instructions, input []transformerModel.Message) {
	if len(req.Messages) > 0 && req.TransformerMetadata[transformerModel.TransformerMetadataOpenAIResponsesInstructions] == "true" {
		return req.Messages[:1], req.Messages[1:]
	}
	return nil, req.Messages
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
	// RawInputItems remains the authoritative runtime source for Responses replay
	// because it preserves exact upstream items that Messages cannot always rebuild.
	if len(currentRawInputItems) > 0 {
		return append(json.RawMessage(nil), currentRawInputItems...), true
	}
	// 没有新输入不算失败：预热之后的第一个请求只带预热 ID，输入是空的。
	currentItems, err := openaiOutbound.MarshalResponsesInputItems(currentMessages)
	return currentItems, err == nil
}

func buildNextReplayWindow(existing json.RawMessage, req *transformerModel.InternalLLMRequest, resp *transformerModel.InternalLLMResponse) (json.RawMessage, bool) {
	if req == nil || resp == nil {
		return nil, false
	}
	var base json.RawMessage
	if rawInputItems := req.OpenAIRawInputItems(); req.IsOpenAIExactReplayRequest() && len(rawInputItems) > 0 {
		base = rawInputItems
	} else if currentItems, ok := buildRequestInputItems(req.OpenAIRawInputItems(), req.Messages); ok {
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
	outputItems := resp.RawResponsesOutputItems
	if len(outputItems) == 0 {
		// 非 Responses 上游没有原始输出条目：用回复内容生成，否则下一轮的历史里没有模型的回复。
		// 推理内容不带：别家的推理签名发给 Responses 上游会被拒绝。
		replies := assistantMessagesFromResponse(resp)
		for i := range replies {
			replies[i].ReasoningContent = nil
		}
		outputItems, _ = openaiOutbound.MarshalResponsesInputItems(replies)
	}
	if len(outputItems) == 0 {
		return base, true
	}
	return mergeRawJSONArray(base, outputItems)
}

// buildRawReplayBody 把客户端的原始请求改成自包含的形式：去掉续传字段，input 换成“存档 + 新输入”，
// 其余字段原样保留。失败时返回 nil，调用方会退回到经内部结构转换的请求。
func buildRawReplayBody(clientBody []byte, mergedInput json.RawMessage) []byte {
	var payload map[string]json.RawMessage
	if len(mergedInput) == 0 || json.Unmarshal(clientBody, &payload) != nil {
		return nil
	}
	delete(payload, "previous_response_id")
	delete(payload, "conversation")
	payload["input"] = mergedInput
	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return body
}

// responsesOutputCollector 从上游事件里收集模型输出条目的原始 JSON。
// 续传回放要把它们原样带回去；经内部结构重新生成会丢掉不认识的字段（比如自定义工具调用的 input）。
type responsesOutputCollector struct {
	done     []json.RawMessage // response.output_item.done 里的条目，按 output_index 排
	indexes  []int             // done 里每一条的 output_index；上游没给时是它到达的次序
	terminal json.RawMessage   // 结束事件自带的非空 output
}

func (c *responsesOutputCollector) observe(event []byte) {
	var parsed struct {
		Type     string          `json:"type"`
		Item     json.RawMessage `json:"item"`
		Index    *int            `json:"output_index"`
		Response struct {
			Output json.RawMessage `json:"output"`
		} `json:"response"`
	}
	if json.Unmarshal(event, &parsed) != nil {
		return
	}
	switch parsed.Type {
	case "response.output_item.done":
		if len(parsed.Item) > 0 {
			index := len(c.done)
			if parsed.Index != nil {
				index = *parsed.Index
			}
			// 条目几乎总是按顺序到的，所以从后往前找位置
			at := len(c.done)
			for at > 0 && c.indexes[at-1] > index {
				at--
			}
			c.done = slices.Insert(c.done, at, parsed.Item)
			c.indexes = slices.Insert(c.indexes, at, index)
		}
	case "response.completed", "response.incomplete":
		if items, err := decodeRawJSONArray(parsed.Response.Output); err == nil && len(items) > 0 {
			c.terminal = parsed.Response.Output
		}
	}
}

// items 优先用结束事件自带的 output；有些上游的结束事件里 output 是空的，这时用逐条收集的。
func (c *responsesOutputCollector) items() json.RawMessage {
	if len(c.terminal) > 0 {
		return c.terminal
	}
	if len(c.done) == 0 {
		return nil
	}
	data, err := json.Marshal(c.done)
	if err != nil {
		return nil
	}
	return data
}

func collectRawResponsesOutputItems(rawStream []byte) json.RawMessage {
	var collector responsesOutputCollector
	for ev, err := range sse.Read(bytes.NewReader(rawStream), &sse.ReadConfig{MaxEventSize: maxSSEEventSize}) {
		if err != nil {
			break
		}
		collector.observe([]byte(ev.Data))
	}
	return collector.items()
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
