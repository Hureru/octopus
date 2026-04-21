package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

type APIFormat string

const (
	APIFormatOpenAIChatCompletion  APIFormat = "openai/chat_completions"
	APIFormatOpenAIResponse        APIFormat = "openai/responses"
	APIFormatOpenAIImageGeneration APIFormat = "openai/image_generation"
	APIFormatOpenAIEmbedding       APIFormat = "openai/embeddings"
	APIFormatGeminiContents        APIFormat = "gemini/contents"
	APIFormatAnthropicMessage      APIFormat = "anthropic/messages"
	APIFormatAiSDKText             APIFormat = "aisdk/text"
	APIFormatAiSDKDataStream       APIFormat = "aisdk/datastream"
)

const (
	TransformerMetadataOpenAIResponsesPassthroughRequired = "octopus_openai_responses_passthrough_required"
	TransformerMetadataOpenAIResponsesPassthroughReason   = "octopus_openai_responses_passthrough_reason"
)

// Request is the unified llm request model for AxonHub, to keep compatibility with major app and framework.
// It choose to base on the OpenAI chat completion request, but add some extra fields to support more features.
type InternalLLMRequest struct {
	// Messages is a list of messages to send to the llm model.
	// For chat completion requests, this field is required.
	// For embedding requests, this field should be empty and Input should be used instead.
	Messages []Message `json:"messages,omitempty" validator:"required,min=1"`

	// Embedding API 参数（与 Messages 互斥）
	// EmbeddingInput is the text or texts to get embeddings for.
	// For embedding requests, this field is required.
	// For chat completion requests, this field should be empty.
	EmbeddingInput *EmbeddingInput `json:"embedding_input,omitempty"` // string or string[]
	// EmbeddingDimensions is the number of dimensions for the embedding output.
	// Only supported for certain embedding models.
	EmbeddingDimensions *int64 `json:"embedding_dimensions,omitempty"`
	// EmbeddingEncodingFormat is the format of the embedding output.
	// Can be "float" or "base64". Defaults to "float".
	EmbeddingEncodingFormat *string `json:"embedding_encoding_format,omitempty"`

	// Model is the model ID used to generate the response.
	Model string `json:"model" validator:"required"`

	// Number between -2.0 and 2.0. Positive values penalize new tokens based on
	// their existing frequency in the text so far, decreasing the model's likelihood
	// to repeat the same line verbatim.
	//
	// See [OpenAI's
	// documentation](https://platform.openai.com/docs/api-reference/parameter-details)
	// for more information.
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`

	// Whether to return log probabilities of the output tokens or not. If true,
	// returns the log probabilities of each output token returned in the `content` of
	// `message`.
	Logprobs *bool `json:"logprobs,omitempty"`

	// An upper bound for the number of tokens that can be generated for a completion,
	// including visible output tokens and
	// [reasoning tokens](https://platform.openai.com/docs/guides/reasoning).
	MaxCompletionTokens *int64 `json:"max_completion_tokens,omitempty"`

	// The maximum number of [tokens](/tokenizer) that can be generated in the chat
	// completion. This value can be used to control
	// [costs](https://openai.com/api/pricing/) for text generated via API.
	//
	// This value is now deprecated in favor of `max_completion_tokens`, and is not
	// compatible with
	// [o-series models](https://platform.openai.com/docs/guides/reasoning).
	MaxTokens *int64 `json:"max_tokens,omitempty"`

	// How many chat completion choices to generate for each input message. Note that
	// you will be charged based on the number of generated tokens across all of the
	// choices. Keep `n` as `1` to minimize costs.
	// NOTE: Not supported, always 1.
	// N *int64 `json:"n,omitempty"`

	// Number between -2.0 and 2.0. Positive values penalize new tokens based on
	// whether they appear in the text so far, increasing the model's likelihood to
	// talk about new topics.
	PresencePenalty *float64 `json:"presence_penalty,omitempty"`

	// This feature is in Beta. If specified, our system will make a best effort to
	// sample deterministically, such that repeated requests with the same `seed` and
	// parameters should return the same result. Determinism is not guaranteed, and you
	// should refer to the `system_fingerprint` response parameter to monitor changes
	// in the backend.
	Seed *int64 `json:"seed,omitempty"`

	// Whether or not to store the output of this chat completion request for use in
	// our [model distillation](https://platform.openai.com/docs/guides/distillation)
	// or [evals](https://platform.openai.com/docs/guides/evals) products.
	//
	// Supports text and image inputs. Note: image inputs over 10MB will be dropped.
	Store *bool `json:"store,omitzero"`

	// What sampling temperature to use, between 0 and 2. Higher values like 0.8 will
	// make the output more random, while lower values like 0.2 will make it more
	// focused and deterministic. We generally recommend altering this or `top_p` but
	// not both.
	Temperature *float64 `json:"temperature,omitempty"`

	// An integer between 0 and 20 specifying the number of most likely tokens to
	// return at each token position, each with an associated log probability.
	// `logprobs` must be set to `true` if this parameter is used.
	TopLogprobs *int64 `json:"top_logprobs,omitzero"`

	// An alternative to sampling with temperature, called nucleus sampling, where the
	// model considers the results of the tokens with top_p probability mass. So 0.1
	// means only the tokens comprising the top 10% probability mass are considered.
	//
	// We generally recommend altering this or `temperature` but not both.
	TopP *float64 `json:"top_p,omitempty"`

	// Used by OpenAI to cache responses for similar requests to optimize your cache
	// hit rates. Replaces the `user` field.
	// [Learn more](https://platform.openai.com/docs/guides/prompt-caching).
	PromptCacheKey *bool `json:"prompt_cache_key,omitzero"`

	// A stable identifier used to help detect users of your application that may be
	// violating OpenAI's usage policies. The IDs should be a string that uniquely
	// identifies each user. We recommend hashing their username or email address, in
	// order to avoid sending us any identifying information.
	// [Learn more](https://platform.openai.com/docs/guides/safety-best-practices#safety-identifiers).
	SafetyIdentifier *string `json:"safety_identifier,omitzero"`

	// This field is being replaced by `safety_identifier` and `prompt_cache_key`. Use
	// `prompt_cache_key` instead to maintain caching optimizations. A stable
	// identifier for your end-users. Used to boost cache hit rates by better bucketing
	// similar requests and to help OpenAI detect and prevent abuse.
	// [Learn more](https://platform.openai.com/docs/guides/safety-best-practices#safety-identifiers).
	User *string `json:"user,omitempty"`

	// Parameters for audio output. Required when audio output is requested with
	// `modalities: ["audio"]`.
	// [Learn more](https://platform.openai.com/docs/guides/audio).
	// TODO
	// Audio ChatCompletionAudioParam `json:"audio,omitzero"`

	// Modify the likelihood of specified tokens appearing in the completion.
	//
	// Accepts a JSON object that maps tokens (specified by their token ID in the
	// tokenizer) to an associated bias value from -100 to 100. Mathematically, the
	// bias is added to the logits generated by the model prior to sampling. The exact
	// effect will vary per model, but values between -1 and 1 should decrease or
	// increase likelihood of selection; values like -100 or 100 should result in a ban
	// or exclusive selection of the relevant token.
	LogitBias map[string]int64 `json:"logit_bias,omitempty"`

	// Set of 16 key-value pairs that can be attached to an object. This can be useful
	// for storing additional information about the object in a structured format, and
	// querying for objects via API or the dashboard.
	//
	// Keys are strings with a maximum length of 64 characters. Values are strings with
	// a maximum length of 512 characters.
	Metadata map[string]string `json:"metadata,omitempty"`

	// Output types that you would like the model to generate. Most models are capable
	// of generating text, which is the default:
	//
	// `["text"]`
	// To generate audio, you can use:
	// `["text", "audio"]`
	// To generate image, you can use:
	// `["text", "image"]`
	// Please note that not all models support audio and image generation.
	// Any of "text", "audio", "image".
	Modalities []string `json:"modalities,omitempty"`

	Audio *struct {
		Format string `json:"format,omitempty"`
		Voice  string `json:"voice,omitempty"`
	} `json:"audio,omitempty"`

	// Controls effort on reasoning for reasoning models. It can be set to "low", "medium", or "high".
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// Reasoning budget for reasoning models.
	// Help fields， will not be sent to the llm service.
	ReasoningBudget *int64 `json:"-"`

	// AdaptiveThinking indicates the client requested adaptive thinking mode.
	// Help field, will not be sent to the llm service.
	AdaptiveThinking bool `json:"-"`

	// ThinkingDisplay passes through the Anthropic `thinking.display` value
	// ("summarized" | "omitted"). Help field, will not be sent directly to the llm service.
	ThinkingDisplay string `json:"-"`

	// EnableThinking is used by Alibaba Qwen models to enable thinking/reasoning output.
	EnableThinking *bool `json:"enable_thinking,omitempty"`

	// Specifies the processing type used for serving the request.
	ServiceTier *string `json:"service_tier,omitempty"`

	// Not supported with latest reasoning models `o3` and `o4-mini`.
	//
	// Up to 4 sequences where the API will stop generating further tokens. The
	// returned text will not contain the stop sequence.
	Stop *Stop `json:"stop,omitempty"` // string or []string

	Stream        *bool          `json:"stream,omitempty"`
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`

	// Static predicted output content, such as the content of a text file that is
	// being regenerated.
	// TODO
	// Prediction ChatCompletionPredictionContentParam `json:"prediction,omitempty"`

	// Whether to enable
	// [parallel function calling](https://platform.openai.com/docs/guides/function-calling#configuring-parallel-function-calling)
	// during tool use.
	ParallelToolCalls *bool       `json:"parallel_tool_calls,omitempty"`
	Tools             []Tool      `json:"tools,omitempty"`
	ToolChoice        *ToolChoice `json:"tool_choice,omitempty"`

	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`

	// Help fields， will not be sent to the llm service.

	// ExtraBody is helpful to extend the request for different providers.
	// It will not be sent to the OpenAI server.
	ExtraBody json.RawMessage `json:"extra_body,omitempty"`

	// RawRequest is the raw request from the client.
	RawRequest []byte `json:"-"`

	// RawAPIFormat is the original format of the request.
	// e.g. the request from the chat/completions endpoint is in the openai/chat_completion format.
	RawAPIFormat APIFormat `json:"-"`

	// TransformerMetadata stores transformer-specific metadata for preserving format during transformations.
	// This is a help field and will not be sent to the llm service.
	TransformerMetadata map[string]string `json:"-"`

	// TransformOptions stores transformer-specific options for preserving request format.
	// This is a help field and will not be sent to the llm service.
	TransformOptions TransformOptions `json:"-"`

	// Include specifies additional output data to include in the model response.
	// This is a help field and will not be sent to the llm service.
	// e.g., "file_search_call.results", "message.input_image.image_url", "reasoning.encrypted_content"
	Include []string `json:"-"`

	// Responses API pass-through fields (only meaningful for OpenAI Response outbound)
	PreviousResponseID       *string         `json:"-"`
	Background               *bool           `json:"-"`
	Prompt                   json.RawMessage `json:"-"`
	ResponsesPromptCacheKey  *string         `json:"-"`
	PromptCacheRetention     *string         `json:"-"`
	MaxToolCalls             *int64          `json:"-"`
	Conversation             json.RawMessage `json:"-"`
	ContextManagement        json.RawMessage `json:"-"`
	ResponsesStreamOptions   json.RawMessage `json:"-"`
	ReasoningSummary         *string         `json:"-"`
	ReasoningGenerateSummary *string         `json:"-"`
	RawInputItems            json.RawMessage `json:"-"` // Preserve unmappable input items for OpenAI passthrough

	// Query stores the original query parameters from the inbound request.
	// This is a help field and will not be sent to the llm service.
	Query url.Values `json:"-"`
}

func (r *InternalLLMRequest) Validate() error {
	if r.Model == "" {
		return errors.New("model is required")
	}

	// 检查是否是 embedding 请求
	isEmbeddingRequest := r.EmbeddingInput != nil
	isChatRequest := r.IsChatRequest()

	if isEmbeddingRequest && isChatRequest {
		return errors.New("cannot specify both messages and input")
	}

	if !isEmbeddingRequest && !isChatRequest {
		return errors.New("either messages or input is required")
	}

	// 验证 embedding 请求
	if isEmbeddingRequest {
		if r.EmbeddingInput.Single == nil && len(r.EmbeddingInput.Multiple) == 0 {
			return errors.New("input cannot be empty")
		}
	}

	// 验证 chat 请求
	if isChatRequest && len(r.Messages) == 0 && len(r.RawInputItems) == 0 {
		return errors.New("messages are required")
	}

	if len(r.Messages) > 0 {
		r.fillMissingToolCallIDsFromToolMessages()
		// r.fillMissingToolCallIDs()
	}

	return nil
}

func (r *InternalLLMRequest) fillMissingToolCallIDs() {
	usedIDs := make(map[string]struct{})
	for _, msg := range r.Messages {
		for _, tc := range msg.ToolCalls {
			if tc.ID == "" {
				continue
			}
			usedIDs[tc.ID] = struct{}{}
		}
	}

	sequence := 0
	for messageIndex := range r.Messages {
		for toolCallIndex := range r.Messages[messageIndex].ToolCalls {
			toolCall := &r.Messages[messageIndex].ToolCalls[toolCallIndex]
			if toolCall.ID != "" {
				continue
			}

			candidate := fmt.Sprintf("call_octopus_%d_%d", messageIndex, toolCallIndex)
			if _, exists := usedIDs[candidate]; exists {
				for {
					candidate = fmt.Sprintf("call_octopus_%d", sequence)
					sequence++
					if _, conflict := usedIDs[candidate]; !conflict {
						break
					}
				}
			}

			toolCall.ID = candidate
			usedIDs[candidate] = struct{}{}
		}
	}
}

func (r *InternalLLMRequest) fillMissingToolCallIDsFromToolMessages() {
	for msgIndex := 0; msgIndex < len(r.Messages); msgIndex++ {
		msg := &r.Messages[msgIndex]
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}

		candidates := make([]string, 0, len(msg.ToolCalls))
		for nextIndex := msgIndex + 1; nextIndex < len(r.Messages); nextIndex++ {
			nextMsg := r.Messages[nextIndex]
			if nextMsg.Role != "tool" {
				break
			}
			if nextMsg.ToolCallID == nil || *nextMsg.ToolCallID == "" {
				continue
			}
			candidates = append(candidates, *nextMsg.ToolCallID)
		}

		if len(candidates) == 0 {
			continue
		}

		used := make(map[string]struct{})
		for _, toolCall := range msg.ToolCalls {
			if toolCall.ID == "" {
				continue
			}
			used[toolCall.ID] = struct{}{}
		}

		candidateIndex := 0
		for toolCallIndex := range msg.ToolCalls {
			if msg.ToolCalls[toolCallIndex].ID != "" {
				continue
			}

			for candidateIndex < len(candidates) {
				candidate := candidates[candidateIndex]
				candidateIndex++
				if _, exists := used[candidate]; exists {
					continue
				}
				msg.ToolCalls[toolCallIndex].ID = candidate
				used[candidate] = struct{}{}
				break
			}
		}
	}
}

// IsEmbeddingRequest returns true if this is an embedding request.
func (r *InternalLLMRequest) IsEmbeddingRequest() bool {
	return r.EmbeddingInput != nil
}

// IsChatRequest returns true if this is a chat completion request.
func (r *InternalLLMRequest) IsChatRequest() bool {
	return len(r.Messages) > 0 || (r.RawAPIFormat == APIFormatOpenAIResponse && len(r.RawInputItems) > 0)
}

func (r *InternalLLMRequest) RequiresOpenAIResponsesPassthrough() bool {
	if r == nil || r.TransformerMetadata == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(r.TransformerMetadata[TransformerMetadataOpenAIResponsesPassthroughRequired]), "true")
}

func (r *InternalLLMRequest) MarkOpenAIResponsesPassthroughRequired(reason string) {
	if r == nil {
		return
	}
	if r.TransformerMetadata == nil {
		r.TransformerMetadata = map[string]string{}
	}
	r.TransformerMetadata[TransformerMetadataOpenAIResponsesPassthroughRequired] = "true"
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	if existing := strings.TrimSpace(r.TransformerMetadata[TransformerMetadataOpenAIResponsesPassthroughReason]); existing != "" {
		reason = existing + "," + reason
	}
	r.TransformerMetadata[TransformerMetadataOpenAIResponsesPassthroughReason] = reason
}

func (r *InternalLLMRequest) ClearHelpFields() {
	for i, msg := range r.Messages {
		msg.ClearHelpFields()
		r.Messages[i] = msg
	}

	r.ExtraBody = nil
	r.Include = nil
}

// NormalizeMessages applies Message.Normalize to every message in the
// request. Outbound transformers call this at the top of TransformRequest so
// that subsequent conversion code can assume messages carry valid, non-empty
// payloads.
func (r *InternalLLMRequest) NormalizeMessages() {
	for i := range r.Messages {
		r.Messages[i].Normalize()
	}
}

// EnforceMessageAlternation rewrites r.Messages so consecutive same-role
// turns are merged and provider-specific opening requirements are met.
// Intended to be called by outbound transformers whose upstream enforces
// strict user/assistant alternation (Anthropic and Gemini). Callers for
// lax providers (OpenAI) can safely skip this.
func (r *InternalLLMRequest) EnforceMessageAlternation(provider AlternationProvider) {
	r.Messages = EnforceAlternation(r.Messages, provider)
}

func (r *InternalLLMRequest) IsImageGenerationRequest() bool {
	return len(r.Modalities) > 0 && slices.Contains(r.Modalities, "image")
}

type TransformOptions struct {
	// ArrayInputs specifies whether the original input was an array.
	ArrayInputs *bool `json:"-"`
}

type StreamOptions struct {
	// If set, an additional chunk will be streamed before the data: [DONE] message.
	// The usage field on this chunk shows the token usage statistics for the entire request,
	// and the choices field will always be an empty array.
	// All other chunks will also include a usage field, but with a null value.
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type Stop struct {
	Stop         *string
	MultipleStop []string
}

func (s Stop) MarshalJSON() ([]byte, error) {
	if s.Stop != nil {
		return json.Marshal(s.Stop)
	}

	if len(s.MultipleStop) > 0 {
		return json.Marshal(s.MultipleStop)
	}

	return []byte("[]"), nil
}

func (s *Stop) UnmarshalJSON(data []byte) error {
	var str string

	err := json.Unmarshal(data, &str)
	if err == nil {
		s.Stop = &str
		return nil
	}

	var strs []string

	err = json.Unmarshal(data, &strs)
	if err == nil {
		s.MultipleStop = strs
		return nil
	}

	return errors.New("invalid stop type")
}

// Message represents a message in the conversation.
type Message struct {
	Role string `json:"role,omitempty"`
	// Content of the message.
	// string or []ContentPart, be careful about the omitzero tag, it required.
	// Some framework may depended on the behavior, we should not response the field if not present.
	Content MessageContent `json:"content,omitzero"`
	Name    *string        `json:"name,omitempty"`

	// The refusal message generated by the model.
	Refusal string `json:"refusal,omitempty"`

	// For tool call response.

	// The index of the message that the tool call is associated with.
	// Is is a help field, will not be sent to the llm service.
	MessageIndex *int    `json:"-"`
	ToolCallID   *string `json:"tool_call_id,omitempty"`
	// The name of the tool call.
	// Is is a help field, will not be sent to the llm service.
	ToolCallName *string `json:"-"`
	// This field is a help field, will not be sent to the llm service.
	ToolCallIsError *bool      `json:"-"`
	ToolCalls       []ToolCall `json:"tool_calls,omitempty"`

	// Images is used by some providers (e.g., Gemini via OpenAI compat) for image generation responses.
	// Images will be merged into Content.MultipleContent during response processing.
	Images []MessageContentPart `json:"images,omitempty"`

	Audio *struct {
		Data       string `json:"data,omitempty"`
		ExpiresAt  int64  `json:"expires_at,omitempty"`
		ID         string `json:"id,omitempty"`
		Transcript string `json:"transcript,omitempty"`
	} `json:"audio,omitempty"`

	// This property is used for the "reasoning" feature supported by deepseek-reasoner
	// the doc from deepseek:
	// - https://api-docs.deepseek.com/api/create-chat-completion#responses
	ReasoningContent *string `json:"reasoning_content,omitempty"`

	// Reasoning is used by some providers (e.g., OpenRouter, Ollama cloud) as an alternative to ReasoningContent.
	// Both fields serve the same purpose, use GetReasoningContent() to get the value.
	Reasoning *string `json:"reasoning,omitempty"`

	// Help field, will not be sent to the llm service, to adapt the anthropic think signature.
	// Deprecated: prefer ReasoningBlocks for multi-block provenance. Kept as a fallback so that
	// legacy single-block emitters (OpenRouter, Ollama compat) keep working.
	ReasoningSignature *string `json:"reasoning_signature,omitempty"`

	// RedactedThinkingBlocks stores opaque redacted_thinking blocks from Anthropic.
	// Deprecated: mirrored into ReasoningBlocks with Kind=ReasoningBlockKindRedacted. Kept for
	// backward compatibility with callers that read this field directly.
	RedactedThinkingBlocks []string `json:"-"`

	// ReasoningBlocks preserves the full order of thinking / redacted_thinking / signature blocks
	// from the assistant turn. Each entry carries its provider-specific signature verbatim so that
	// multi-turn tool-use scenarios (Anthropic extended thinking, Gemini 3 thoughtSignature) can be
	// replayed to the upstream without signature_mismatch / FAILED_PRECONDITION errors.
	ReasoningBlocks []ReasoningBlock `json:"-"`

	// CacheControl is used for provider-specific cache control (e.g., Anthropic).
	// This field is not serialized in JSON.
	CacheControl *CacheControl `json:"-"`
}

// ReasoningBlockKind enumerates the kinds of reasoning/thinking blocks we preserve.
type ReasoningBlockKind string

const (
	// ReasoningBlockKindThinking is a visible thinking block with optional signature (Anthropic)
	// or thought-signature-carrying Part (Gemini 3).
	ReasoningBlockKindThinking ReasoningBlockKind = "thinking"
	// ReasoningBlockKindRedacted is Anthropic's redacted_thinking block (opaque data, no text).
	ReasoningBlockKindRedacted ReasoningBlockKind = "redacted_thinking"
	// ReasoningBlockKindSignature is a standalone signature carrier used when the text part has
	// already been emitted separately (e.g. Gemini fn-call Part-level thoughtSignature).
	ReasoningBlockKindSignature ReasoningBlockKind = "thought_signature"
)

// ReasoningBlock preserves one thinking/redacted_thinking/thought_signature block verbatim.
// All fields are opaque to the aggregator; they are round-tripped to the upstream as-is.
type ReasoningBlock struct {
	Kind      ReasoningBlockKind `json:"kind,omitempty"`
	Index     int                `json:"index,omitempty"`
	Text      string             `json:"text,omitempty"`
	Signature string             `json:"signature,omitempty"`
	Data      string             `json:"data,omitempty"`
	Provider  string             `json:"provider,omitempty"`
}

// AppendReasoningBlock appends a reasoning block preserving insertion order.
// Index is auto-assigned based on the current slice length when the caller passes a negative Index.
func (m *Message) AppendReasoningBlock(block ReasoningBlock) {
	if block.Index < 0 {
		block.Index = len(m.ReasoningBlocks)
	}
	m.ReasoningBlocks = append(m.ReasoningBlocks, block)
}

// ReasoningBlocksByProvider returns the subset of blocks authored by the given provider.
// Pass an empty string to get the full slice.
func (m *Message) ReasoningBlocksByProvider(provider string) []ReasoningBlock {
	if provider == "" {
		return m.ReasoningBlocks
	}
	out := make([]ReasoningBlock, 0, len(m.ReasoningBlocks))
	for _, b := range m.ReasoningBlocks {
		if b.Provider == provider {
			out = append(out, b)
		}
	}
	return out
}

func (m *Message) ClearHelpFields() {
	m.ReasoningContent = nil
	m.Reasoning = nil
	m.ReasoningSignature = nil
	m.RedactedThinkingBlocks = nil
	m.ReasoningBlocks = nil
}

// Normalize prepares the message for dispatch to upstream providers. It
// performs two jobs:
//  1. Drop empty text parts from MultipleContent. Image / audio / file /
//     tool_use / tool_result / thinking parts are preserved verbatim.
//  2. If the message ends up with no content-carrying payload at all
//     (no text, no non-text parts, no tool_calls, no reasoning), insert a
//     single-space placeholder into Content.Content. This matches
//     Anthropic's strictest requirement — empty assistant / user messages
//     elicit a 400 — while remaining harmless for OpenAI and Gemini.
//
// Normalize is idempotent and safe to call before every outbound dispatch.
func (m *Message) Normalize() {
	if m == nil {
		return
	}

	if len(m.Content.MultipleContent) > 0 {
		filtered := m.Content.MultipleContent[:0]
		for _, part := range m.Content.MultipleContent {
			if isEmptyTextPart(part) {
				continue
			}
			filtered = append(filtered, part)
		}
		m.Content.MultipleContent = filtered
	}

	if m.hasAnyPayload() {
		return
	}

	space := " "
	m.Content.Content = &space
	m.Content.MultipleContent = nil
}

// hasAnyPayload reports whether the message carries any information that
// would reach the upstream provider. Used by Normalize to decide whether
// the single-space placeholder is necessary.
func (m *Message) hasAnyPayload() bool {
	if m.Content.Content != nil && *m.Content.Content != "" {
		return true
	}
	if len(m.Content.MultipleContent) > 0 {
		return true
	}
	if len(m.ToolCalls) > 0 {
		return true
	}
	if m.ToolCallID != nil && *m.ToolCallID != "" {
		return true
	}
	if m.ReasoningContent != nil && *m.ReasoningContent != "" {
		return true
	}
	if m.Reasoning != nil && *m.Reasoning != "" {
		return true
	}
	if len(m.ReasoningBlocks) > 0 {
		return true
	}
	if len(m.RedactedThinkingBlocks) > 0 {
		return true
	}
	return false
}

// isEmptyTextPart reports whether a MessageContentPart is a text-type entry
// whose Text is nil or empty. Non-text parts always return false so image /
// audio / file / tool_use payloads are never dropped.
func isEmptyTextPart(p MessageContentPart) bool {
	if p.Type != "" && p.Type != "text" {
		return false
	}
	return p.Text == nil || *p.Text == ""
}

// GetReasoningContent returns the reasoning content from either ReasoningContent or Reasoning field.
// Different providers use different field names for the same purpose.
func (m *Message) GetReasoningContent() string {
	if m.ReasoningContent != nil {
		return *m.ReasoningContent
	}
	if m.Reasoning != nil {
		return *m.Reasoning
	}
	return ""
}

// SetReasoningContent sets the reasoning content to the ReasoningContent field.
func (m *Message) SetReasoningContent(s string) {
	m.ReasoningContent = &s
}

type MessageContent struct {
	Content         *string              `json:"content,omitempty"`
	MultipleContent []MessageContentPart `json:"multiple_content,omitempty"`
}

func (c MessageContent) MarshalJSON() ([]byte, error) {
	if len(c.MultipleContent) > 0 {
		if len(c.MultipleContent) == 1 && c.MultipleContent[0].Type == "text" {
			return json.Marshal(c.MultipleContent[0].Text)
		}

		return json.Marshal(c.MultipleContent)
	}

	return json.Marshal(c.Content)
}

func (c *MessageContent) UnmarshalJSON(data []byte) error {
	var str string

	err := json.Unmarshal(data, &str)
	if err == nil {
		c.Content = &str
		return nil
	}

	var parts []MessageContentPart

	err = json.Unmarshal(data, &parts)
	if err == nil {
		c.MultipleContent = parts
		return nil
	}

	return errors.New("invalid content type")
}

// MessageContentPart represents different types of content (text, image, etc.)
type MessageContentPart struct {
	// Type is the type of the content part.
	// e.g. "text", "image_url", "input_audio", "file", "document",
	// "server_tool_use", "server_tool_result".
	Type string `json:"type"`
	// Text is the text content, required when type is "text"
	Text *string `json:"text,omitempty"`

	// ImageURL is the image URL content, required when type is "image_url"
	ImageURL *ImageURL `json:"image_url,omitempty"`

	// Audio is the audio content, required when type is "input_audio"
	Audio *Audio `json:"input_audio,omitempty"`

	// File is the file content, required when type is "file"
	File *File `json:"file,omitempty"`

	// Document is the document content, required when type is "document".
	// Mirrors Anthropic's document block — carries a PDF/text source plus
	// optional title/context metadata and citation hints. The json:"-" tag
	// keeps the block from leaking to OpenAI chat completions (which would
	// reject the field); provider-specific outbound paths read the Go
	// field directly.
	Document *DocumentSource `json:"-"`

	// ServerToolUse captures an Anthropic server_tool_use block (e.g.
	// web_search_20250305, code_execution_20250522) so it can be preserved
	// across retries and re-emitted verbatim. Non-Anthropic providers drop
	// the block and surface a warning. json:"-" for the same reason as
	// Document.
	ServerToolUse *ServerToolUseBlock `json:"-"`

	// ServerToolResult captures the result payload returned by Anthropic
	// after a server-side tool invocation (web_search_tool_result,
	// code_execution_tool_result). Shape mirrors tool_result but uses the
	// dedicated server_* name so routing logic can distinguish them.
	// json:"-" for the same reason as Document.
	ServerToolResult *ServerToolResultBlock `json:"-"`

	// CacheControl is used for provider-specific cache control (e.g., Anthropic).
	// This field is not serialized in JSON.
	CacheControl *CacheControl `json:"-"`
}

// DocumentSource mirrors the Anthropic document content block. A document
// carries a source (base64 / url / text / content array) plus optional
// title / context / citation metadata. Other providers that don't support
// native documents fall back to emitting either an inline PDF blob (Gemini
// inline_data application/pdf) or a text hint in the user turn.
type DocumentSource struct {
	// Type identifies the envelope: "base64", "url", "text", or "content".
	// "content" carries an array of sub-blocks (not yet fully supported —
	// passthrough only).
	Type string `json:"type"`

	// MediaType is the MIME type of the document payload.
	// Examples: "application/pdf", "text/plain".
	MediaType string `json:"media_type,omitempty"`

	// Data is the base64-encoded document (when Type=="base64").
	Data string `json:"data,omitempty"`

	// URL is the document URL (when Type=="url").
	URL string `json:"url,omitempty"`

	// Text is the raw text content (when Type=="text"). Providers that
	// cannot embed documents natively use this as the fallback payload.
	Text string `json:"text,omitempty"`

	// Content holds pre-chunked sub-blocks (when Type=="content"). This is
	// an opaque JSON payload — we preserve it for Anthropic passthrough but
	// do not interpret it.
	Content json.RawMessage `json:"content,omitempty"`

	// Title / Context are optional metadata hints Anthropic surfaces in
	// citation responses. Both are passed through unchanged.
	Title   string `json:"title,omitempty"`
	Context string `json:"context,omitempty"`

	// Citations configures Anthropic's citation generation. When Enabled
	// is true the model may emit citation references to this document.
	Citations *DocumentCitations `json:"citations,omitempty"`
}

// DocumentCitations matches Anthropic's document.citations sub-object.
type DocumentCitations struct {
	Enabled bool `json:"enabled,omitempty"`
}

// ServerToolUseBlock captures Anthropic's server_tool_use content block.
// Unlike a regular tool_use, the tool is invoked by Anthropic's backend
// (web search, code execution, etc.) and the invocation is already
// complete by the time the block reaches the client.
type ServerToolUseBlock struct {
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// ServerToolResultBlock captures the result of a server-side tool
// invocation (web_search_tool_result, code_execution_tool_result). The
// content field mirrors tool_result.content — Anthropic returns either a
// text string or an array of sub-blocks.
type ServerToolResultBlock struct {
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   *bool           `json:"is_error,omitempty"`
}

// ImageURL represents an image URL with optional detail level.
type ImageURL struct {
	// URL is the URL of the image.
	URL string `json:"url"`

	// Specifies the detail level of the image. Learn more in the
	// [Vision guide](https://platform.openai.com/docs/guides/vision#low-or-high-fidelity-image-understanding).
	//
	// Any of "auto", "low", "high".
	Detail *string `json:"detail,omitempty"`
}

type Audio struct {
	// The format of the encoded audio data. Currently supports "wav" and "mp3".
	//
	// Any of "wav", "mp3".
	Format string `json:"format"`

	// Base64 encoded audio data.
	Data string `json:"data"`
}

type File struct {
	// The filename of the file.
	Filename string `json:"filename"`
	// The base64 encoded data of the file.
	FileData string `json:"file_data"`
}

// ResponseFormat specifies the format of the response.
//
// OpenAI's `response_format: { type: "json_schema", json_schema: {...} }`
// payload has two parts — a wrapper (name / strict / description) and the
// actual schema. Schema holds the parsed form; RawSchema keeps the original
// bytes so providers that prefer passthrough (or un-parseable schemas) stay
// faithful to the client's intent. Callers should prefer Schema where
// possible and fall back to RawSchema as an escape hatch.
type ResponseFormat struct {
	// Any of "json_schema", "json_object", "text".
	Type string `json:"type"`

	// Name is the OpenAI json_schema.name field (required by the strict
	// OpenAI schema), carried through so outbound emitters can reproduce
	// the wrapper verbatim.
	Name string `json:"name,omitempty"`

	// Description mirrors json_schema.description.
	Description string `json:"description,omitempty"`

	// Strict mirrors json_schema.strict (OpenAI-only; null → client did
	// not specify).
	Strict *bool `json:"strict,omitempty"`

	// Schema is the parsed structured-output schema.
	Schema *Schema `json:"-"`

	// RawSchema preserves the original schema bytes for passthrough /
	// provider-specific forwarding when the typed Schema cannot capture
	// every keyword. Emitters should emit RawSchema only when Schema is
	// nil or when an explicit passthrough is requested.
	RawSchema json.RawMessage `json:"-"`

	// JSONSchema is the legacy field name kept for backward compatibility
	// with code paths that stored the raw schema bytes directly. New code
	// should populate Schema / RawSchema instead.
	JSONSchema json.RawMessage `json:"json_schema,omitempty"`
}

// UnmarshalJSON parses both the canonical OpenAI Responses wire shape
// (`{type, json_schema:{name,strict,schema}}`) and the legacy shape where
// `json_schema` was a bare schema object. It populates Schema/RawSchema
// uniformly so callers can treat them as the authoritative fields.
func (r *ResponseFormat) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type       string          `json:"type"`
		JSONSchema json.RawMessage `json:"json_schema,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.Type = raw.Type
	r.JSONSchema = raw.JSONSchema

	if len(raw.JSONSchema) == 0 {
		return nil
	}

	// First attempt: the OpenAI wrapper {name, strict, description, schema}.
	var wrapper struct {
		Name        string          `json:"name,omitempty"`
		Description string          `json:"description,omitempty"`
		Strict      *bool           `json:"strict,omitempty"`
		Schema      json.RawMessage `json:"schema,omitempty"`
	}
	if err := json.Unmarshal(raw.JSONSchema, &wrapper); err == nil && len(wrapper.Schema) > 0 {
		r.Name = wrapper.Name
		r.Description = wrapper.Description
		r.Strict = wrapper.Strict
		r.RawSchema = wrapper.Schema
		if parsed, err := ParseSchema(wrapper.Schema); err == nil {
			r.Schema = parsed
		}
		return nil
	}

	// Fallback: bare schema object (`json_schema: {type:"object", ...}`).
	r.RawSchema = raw.JSONSchema
	if parsed, err := ParseSchema(raw.JSONSchema); err == nil {
		r.Schema = parsed
	}
	return nil
}

// MarshalJSON re-serialises the canonical OpenAI Responses wire shape. When
// Schema is populated it's preferred over RawSchema; JSONSchema is kept as
// the outermost carrier so downstream code that only reads the legacy field
// still works.
func (r ResponseFormat) MarshalJSON() ([]byte, error) {
	type out struct {
		Type       string          `json:"type"`
		JSONSchema json.RawMessage `json:"json_schema,omitempty"`
	}
	o := out{Type: r.Type}

	schemaBytes := r.RawSchema
	if len(schemaBytes) == 0 && r.Schema != nil {
		if b, err := json.Marshal(r.Schema); err == nil {
			schemaBytes = b
		}
	}

	// If we have no schema content but a legacy JSONSchema blob, pass it
	// through unchanged.
	if len(schemaBytes) == 0 {
		o.JSONSchema = r.JSONSchema
		return json.Marshal(o)
	}

	// Re-wrap into {name, strict, description, schema} whenever any of
	// those metadata fields are set; otherwise emit the bare schema for
	// backward compatibility with callers that read json_schema directly.
	if r.Name != "" || r.Description != "" || r.Strict != nil {
		wrapper := struct {
			Name        string          `json:"name,omitempty"`
			Description string          `json:"description,omitempty"`
			Strict      *bool           `json:"strict,omitempty"`
			Schema      json.RawMessage `json:"schema,omitempty"`
		}{
			Name:        r.Name,
			Description: r.Description,
			Strict:      r.Strict,
			Schema:      schemaBytes,
		}
		b, err := json.Marshal(wrapper)
		if err != nil {
			return nil, err
		}
		o.JSONSchema = b
	} else {
		o.JSONSchema = schemaBytes
	}
	return json.Marshal(o)
}

// Response is the unified response model.
// To reduce the work of converting the response, we use the OpenAI response format.
// And other llm provider should convert the response to this format.
// NOTE: the OpenAI stream and non-stream response reuse same struct.
type InternalLLMResponse struct {
	ID string `json:"id"`

	// RawResponsesOutputItems preserves exact OpenAI Responses output items when available.
	// It is an internal helper field for exact replay reconstruction and is not part of API output.
	RawResponsesOutputItems json.RawMessage `json:"-"`

	// A list of chat completion choices. Can be more than one if `n` is greater
	// than 1.
	// For chat completion responses, this field is required.
	// For embedding responses, this field should be empty and EmbeddingData should be used instead.
	Choices []Choice `json:"choices,omitempty"`

	// Embedding API 响应（与 Choices 互斥）
	// EmbeddingData is the list of embedding objects.
	// For embedding responses, this field is required.
	// For chat completion responses, this field should be empty.
	EmbeddingData []EmbeddingObject `json:"embedding_data,omitempty"`

	// Object is the type of the response.
	// e.g. "chat.completion", "chat.completion.chunk", "list"
	Object string `json:"object"`

	// Created is the timestamp of when the response was created.
	Created int64 `json:"created"`

	// Model is the model used to generate the response.
	Model string `json:"model"`

	// An optional field that will only be present when you set stream_options: {"include_usage": true} in your request.
	// When present, it contains a null value except for the last chunk which contains the token usage statistics
	// for the entire request.
	Usage *Usage `json:"usage,omitempty"`

	// This fingerprint represents the backend configuration that the model runs with.
	//
	// Can be used in conjunction with the `seed` request parameter to understand when
	// backend changes have been made that might impact determinism.
	SystemFingerprint string `json:"system_fingerprint,omitempty"`

	// ServiceTier is the service tier of the response.
	// e.g. "free", "standard", "premium"
	ServiceTier string `json:"service_tier,omitempty"`

	// Error is the error information, will present if request to llm service failed with status >= 400.
	Error *ResponseError `json:"error,omitempty"`
}

func (r *InternalLLMResponse) ClearHelpFields() {
	for i, choice := range r.Choices {
		if choice.Message != nil {
			choice.Message.ClearHelpFields()
		}

		if choice.Delta != nil {
			choice.Delta.ClearHelpFields()
		}

		r.Choices[i] = choice
	}
}

// IsEmbeddingResponse returns true if this is an embedding response.
func (r *InternalLLMResponse) IsEmbeddingResponse() bool {
	return len(r.EmbeddingData) > 0
}

// IsChatResponse returns true if this is a chat completion response.
func (r *InternalLLMResponse) IsChatResponse() bool {
	return len(r.Choices) > 0
}

// Choice represents a choice in the response.
// Choice represents a choice in the response.
type Choice struct {
	// Index is the index of the choice in the list of choices.
	Index int `json:"index"`

	// Message is the message content, will present if stream is false
	Message *Message `json:"message,omitempty"`

	// Delta is the stream event content, will present if stream is true
	Delta *Message `json:"delta,omitempty"`

	// FinishReason is the reason the model stopped generating tokens.
	// e.g. "stop", "length", "content_filter", "function_call", "tool_calls"
	FinishReason *string `json:"finish_reason,omitempty"`

	Logprobs *LogprobsContent `json:"logprobs,omitempty"`
}

// LogprobsContent represents logprobs information.
type LogprobsContent struct {
	Content []TokenLogprob `json:"content"`
}

// TokenLogprob represents logprob for a token.
type TokenLogprob struct {
	Token       string       `json:"token"`
	Logprob     float64      `json:"logprob"`
	Bytes       []int        `json:"bytes,omitempty"`
	TopLogprobs []TopLogprob `json:"top_logprobs,omitempty"`
}

// TopLogprob represents top alternative tokens.
type TopLogprob struct {
	Token   string  `json:"token"`
	Logprob float64 `json:"logprob"`
	Bytes   []int   `json:"bytes,omitempty"`
}

type ResponseMeta struct {
	ID    string `json:"id"`
	Usage *Usage `json:"usage"`
}

// Usage Represents the total token usage per request to OpenAI.
// For embedding requests, CompletionTokens is always 0.
//
// Cache semantics (detection via HasAnthropicCacheSemantic):
//   - Anthropic path: PromptTokens excludes cached tokens; CacheReadInputTokens /
//     CacheCreationInputTokens carry the split. PromptTokensDetails.CachedTokens is
//     mirrored for OpenAI-style downstreams.
//   - OpenAI / Gemini path: PromptTokens already includes cached reads (OpenAI
//     convention). CacheReadInputTokens stays zero; PromptTokensDetails.CachedTokens
//     holds the cached subset.
//
// Use the Billable* and EffectiveInputTokens helpers instead of branching on the
// upstream provider directly.
type Usage struct {
	PromptTokens            int64                    `json:"prompt_tokens"`
	CompletionTokens        int64                    `json:"completion_tokens"`
	TotalTokens             int64                    `json:"total_tokens"`
	PromptTokensDetails     *PromptTokensDetails     `json:"prompt_tokens_details"`
	CompletionTokensDetails *CompletionTokensDetails `json:"completion_tokens_details"`

	// Output only. A detailed breakdown of the token count for each modality in the prompt.
	PromptModalityTokenDetails []ModalityTokenCount `json:"-"`
	// Output only. A detailed breakdown of the token count for each modality in the candidates.
	CompletionModalityTokenDetails []ModalityTokenCount `json:"-"`

	// Anthropic-style cache accounting. Presence of either CacheCreationInputTokens or
	// CacheReadInputTokens implies PromptTokens excludes cached tokens.
	CacheCreationInputTokens   int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheCreation5mInputTokens int64 `json:"cache_creation_5m_input_tokens,omitempty"`
	CacheCreation1hInputTokens int64 `json:"cache_creation_1h_input_tokens,omitempty"`
	CacheReadInputTokens       int64 `json:"cache_read_input_tokens,omitempty"`
}

func (u *Usage) GetCompletionTokens() *int64 {
	if u == nil {
		return nil
	}

	return &u.CompletionTokens
}

func (u *Usage) GetPromptTokens() *int64 {
	if u == nil {
		return nil
	}

	return &u.PromptTokens
}

// HasAnthropicCacheSemantic reports whether PromptTokens excludes cached tokens
// (the Anthropic Messages convention). When true, total input billing must add
// cache read/write on top of PromptTokens.
func (u *Usage) HasAnthropicCacheSemantic() bool {
	if u == nil {
		return false
	}
	return u.CacheCreationInputTokens > 0 || u.CacheReadInputTokens > 0
}

// BillableCacheReadInput returns the cache-read token count, preferring the
// explicit CacheReadInputTokens field and falling back to the OpenAI-style
// PromptTokensDetails.CachedTokens mirror.
func (u *Usage) BillableCacheReadInput() int64 {
	if u == nil {
		return 0
	}
	if u.CacheReadInputTokens > 0 {
		return u.CacheReadInputTokens
	}
	if u.PromptTokensDetails != nil {
		return u.PromptTokensDetails.CachedTokens
	}
	return 0
}

// BillableCacheWriteInput returns cache creation tokens (only populated on the
// Anthropic path).
func (u *Usage) BillableCacheWriteInput() int64 {
	if u == nil {
		return 0
	}
	return u.CacheCreationInputTokens
}

// BillableNonCachedInput returns the non-cached portion of the prompt used for
// standard input pricing. Semantics:
//   - Anthropic: PromptTokens already excludes cache, return as-is.
//   - OpenAI / Gemini: subtract cached tokens tracked in PromptTokensDetails.
func (u *Usage) BillableNonCachedInput() int64 {
	if u == nil {
		return 0
	}
	if u.HasAnthropicCacheSemantic() {
		if u.PromptTokens < 0 {
			return 0
		}
		return u.PromptTokens
	}
	cached := u.BillableCacheReadInput()
	n := u.PromptTokens - cached
	if n < 0 {
		return 0
	}
	return n
}

// EffectiveInputTokens returns the total input tokens counted against quota
// across providers: PromptTokens + CacheReadInputTokens + CacheCreationInputTokens.
// For OpenAI/Gemini (where PromptTokens already includes cached reads),
// CacheRead/Create stay zero so the result collapses to PromptTokens.
func (u *Usage) EffectiveInputTokens() int64 {
	if u == nil {
		return 0
	}
	return u.PromptTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
}

// CompletionTokensDetails Breakdown of tokens used in a completion.
type CompletionTokensDetails struct {
	AudioTokens              int64 `json:"audio_tokens"`
	ReasoningTokens          int64 `json:"reasoning_tokens"`
	AcceptedPredictionTokens int64 `json:"accepted_prediction_tokens"`
	RejectedPredictionTokens int64 `json:"rejected_prediction_tokens"`
}

// PromptTokensDetails Breakdown of tokens used in the prompt.
type PromptTokensDetails struct {
	AudioTokens  int64 `json:"audio_tokens"`
	CachedTokens int64 `json:"cached_tokens"`
}

// ResponseError represents an error response.
type ResponseError struct {
	StatusCode int         `json:"-"`
	Detail     ErrorDetail `json:"error"`
}

func (e ResponseError) Error() string {
	sb := strings.Builder{}
	if e.StatusCode != 0 {
		sb.WriteString(fmt.Sprintf("Request failed: %s, ", http.StatusText(e.StatusCode)))
	}

	if e.Detail.Message != "" {
		sb.WriteString("error: ")
		sb.WriteString(e.Detail.Message)
	}

	if e.Detail.Code != "" {
		sb.WriteString(", code: ")
		sb.WriteString(e.Detail.Code)
	}

	if e.Detail.Type != "" {
		sb.WriteString(", type: ")
		sb.WriteString(e.Detail.Type)
	}

	if e.Detail.RequestID != "" {
		sb.WriteString(", request_id: ")
		sb.WriteString(e.Detail.RequestID)
	}

	return sb.String()
}

// ErrorDetail represents error details.
type ErrorDetail struct {
	Code      string `json:"code,omitempty"`
	Message   string `json:"message"`
	Type      string `json:"type"`
	Param     string `json:"param,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// ModalityTokenCount Represents token counting info for a single modality.
type ModalityTokenCount struct {
	Modality string `json:"modality,omitempty"`
	// Number of tokens.
	TokenCount int64 `json:"token_count,omitempty"`
}

// Tool represents a function tool.
type Tool struct {
	// Type is the type of the tool.
	// Any of "function", "image_generation".
	Type            string           `json:"type"`
	Function        Function         `json:"function"`
	ImageGeneration *ImageGeneration `json:"image_generation,omitempty"`

	// CacheControl is used for provider-specific cache control (e.g., Anthropic).
	// This field is not serialized in JSON.
	CacheControl *CacheControl `json:"-"`
}

// CacheControl represents cache control configuration.
// Shared by Anthropic prompt caching (`{"type":"ephemeral","ttl":"5m"|"1h"}`) and any future
// providers that piggyback on the same shape. The struct is serialized so internal logs and
// tests can snapshot it; outbound transformers decide whether to forward it.
type CacheControl struct {
	Type string `json:"type,omitempty"`
	TTL  string `json:"ttl,omitempty"`
}

// CacheControlType and CacheControlTTL values currently accepted by Anthropic's Messages API.
// Keep as typed constants so typos fail at compile time; unknown values should still be
// rejected at the inbound boundary.
const (
	CacheControlTypeEphemeral = "ephemeral"
	CacheTTL5m                = "5m"
	CacheTTL1h                = "1h"
)

// AnthropicMaxCacheBreakpoints is the provider-enforced upper bound on `cache_control` breakpoints
// per Messages request. Excess breakpoints must be pruned by the outbound layer rather than
// surfacing an HTTP 400 to the caller.
const AnthropicMaxCacheBreakpoints = 4

// Validate returns an error if the CacheControl values are not acceptable for Anthropic.
// Nil receivers are treated as valid (absent).
func (c *CacheControl) Validate() error {
	if c == nil {
		return nil
	}
	if c.Type != "" && c.Type != CacheControlTypeEphemeral {
		return fmt.Errorf("invalid cache_control.type %q (only %q is supported)", c.Type, CacheControlTypeEphemeral)
	}
	if c.TTL != "" && c.TTL != CacheTTL5m && c.TTL != CacheTTL1h {
		return fmt.Errorf("invalid cache_control.ttl %q (only %q or %q are supported)", c.TTL, CacheTTL5m, CacheTTL1h)
	}
	return nil
}

type toolJSONMarshaller Tool

func (t Tool) MarshalJSON() ([]byte, error) {
	// TODO: find a better way to save the image generation tool to the request body.
	m := toolJSONMarshaller(t)
	// ImageGeneration is not a valid field for chat completion, so we should remove it from the request.
	m.ImageGeneration = nil

	return json.Marshal(m)
}

// Function represents a function definition.
type Function struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      *bool           `json:"strict,omitempty"`
}

// FunctionCall represents a function call (deprecated).
type FunctionCall struct {
	// The name of the function to call.
	Name string `json:"name"`

	// The arguments to call the function with, as generated by the model in JSON
	// format. Note that the model does not always generate valid JSON, and may
	// hallucinate parameters not defined by your function schema. Validate the
	// arguments in your code before calling your function.
	Arguments string `json:"arguments"`
}

// ToolCall represents a tool call in the response.
type ToolCall struct {
	ID string `json:"id,omitempty"`

	// The type of the tool. Currently, only `function` is supported.
	Type string `json:"type,omitempty"`

	Function FunctionCall `json:"function"`

	// Index is the index of the tool call in the list of tool calls.
	// Cannot use omitempty, as an index of 0 would be omitted, which can break consumers.
	Index int `json:"index"`

	// ThoughtSignature preserves Gemini's per-functionCall thoughtSignature so
	// multi-turn tool use can be replayed without losing the provider-required
	// signature binding. Help field only; never serialized to client JSON.
	ThoughtSignature string `json:"-"`

	// CacheControl is used for provider-specific cache control (e.g., Anthropic).
	CacheControl *CacheControl `json:"-"`
}

type ToolFunction struct {
	Name string `json:"name"`
}

// ToolChoice represents the tool choice parameter for function calling.
//
// Tool choice can be a string or a struct.
type ToolChoice struct {
	ToolChoice      *string          `json:"tool_choice,omitempty"`
	NamedToolChoice *NamedToolChoice `json:"named_tool_choice,omitempty"`
}

// NamedToolChoice is the structured form of tool_choice. It covers both the
// OpenAI-shaped `{type:"function", function:{name:"..."}}` payload and the
// Anthropic-shaped `{type:"tool"|"any"|"none", name:"...",
// disable_parallel_tool_use:true}` variants so the internal model is lossless
// across providers.
type NamedToolChoice struct {
	// Type selects the variant. Values used across providers:
	//   - OpenAI: "function"
	//   - Anthropic: "auto" | "any" | "none" | "tool"
	// The string mode ("auto"/"required"/"none") is still carried on
	// ToolChoice.ToolChoice; this struct form is for the rich variants.
	Type string `json:"type"`

	// Function mirrors the OpenAI-style payload. Optional: Anthropic-style
	// variants (any / none / tool) either omit this entirely or use Name
	// instead, so callers must nil-check before reading Function.Name.
	Function *ToolFunction `json:"function,omitempty"`

	// Name carries Anthropic's top-level `name` field when Type is "tool".
	// Also populated for OpenAI-style to mirror Function.Name so downstream
	// emitters can rely on a single field.
	Name *string `json:"name,omitempty"`

	// DisableParallelToolUse surfaces Anthropic's
	// `disable_parallel_tool_use` flag. When true the provider is asked to
	// emit at most one tool call per turn, regardless of Type.
	DisableParallelToolUse *bool `json:"disable_parallel_tool_use,omitempty"`
}

// ResolvedFunctionName returns the tool name to force, preferring the top
// level Name field (Anthropic wire shape) and falling back to Function.Name
// (OpenAI wire shape). An empty string means no specific tool was pinned.
func (n *NamedToolChoice) ResolvedFunctionName() string {
	if n == nil {
		return ""
	}
	if n.Name != nil && *n.Name != "" {
		return *n.Name
	}
	if n.Function != nil {
		return n.Function.Name
	}
	return ""
}

func (t ToolChoice) MarshalJSON() ([]byte, error) {
	if t.ToolChoice != nil {
		return json.Marshal(t.ToolChoice)
	}

	return json.Marshal(t.NamedToolChoice)
}

func (t *ToolChoice) UnmarshalJSON(data []byte) error {
	var str string

	err := json.Unmarshal(data, &str)
	if err == nil {
		t.ToolChoice = &str
		return nil
	}

	var named NamedToolChoice

	err = json.Unmarshal(data, &named)
	if err == nil {
		t.NamedToolChoice = &named
		return nil
	}

	return errors.New("invalid tool choice type")
}

// ImageGeneration is a permissive structure to carry image generation tool
// parameters. It mirrors the OpenRouter/OpenAI Responses API fields we care
// about, but is intentionally loose to allow forward-compatibility.
type ImageGeneration struct {
	// One of opaque, transparent.
	Background     string         `json:"background,omitempty"`
	InputFidelity  string         `json:"input_fidelity,omitempty"`
	InputImageMask map[string]any `json:"input_image_mask,omitempty"`
	// One of low, auto.
	Moderation string `json:"moderation,omitempty"`
	// The compression level (0-100%) for the generated images. Default: 100.
	OutputCompression *int64 `json:"output_compression,omitempty"`
	// One of png, webp, or jpeg. Default: png.
	OutputFormat string `json:"output_format,omitempty"`
	// The number of images to generate. Default: 1.
	PartialImages *int64 `json:"partial_images,omitempty"`
	// The quality of the image that will be generated.
	// auto (default value) will automatically select the best quality for the given model.
	// high, medium and low are supported for gpt-image-1.
	// hd and standard are supported for dall-e-3.
	// standard is the only option for dall-e-2.
	Quality string `json:"quality,omitempty"`
	// One of 256x256, 512x512, or 1024x1024. Default: 1024x1024.
	Size string `json:"size,omitempty"`

	// Whether to add a watermark to the generated image. Default: false.
	// It only works for the models support watermark, it will be ignored otherwise.
	Watermark bool `json:"watermark,omitempty"`
}

// EmbeddingInput represents the input for embedding requests.
// It can be a single string or an array of strings.
type EmbeddingInput struct {
	Single   *string
	Multiple []string
}

func (i EmbeddingInput) MarshalJSON() ([]byte, error) {
	if i.Single != nil {
		return json.Marshal(i.Single)
	}

	if len(i.Multiple) > 0 {
		return json.Marshal(i.Multiple)
	}

	return []byte("null"), nil
}

func (i *EmbeddingInput) UnmarshalJSON(data []byte) error {
	var str string

	err := json.Unmarshal(data, &str)
	if err == nil {
		i.Single = &str
		return nil
	}

	var strs []string

	err = json.Unmarshal(data, &strs)
	if err == nil {
		i.Multiple = strs
		return nil
	}

	return errors.New("invalid input type")
}

// EmbeddingObject represents a single embedding object in the response.
type EmbeddingObject struct {
	// The object type, always "embedding".
	Object string `json:"object"`
	// The index of this embedding in the list.
	Index int `json:"index"`
	// The embedding vector.
	Embedding Embedding `json:"embedding"`
}

// Embedding represents an embedding vector.
// It can be a float array or a base64-encoded string.
type Embedding struct {
	FloatArray   []float64
	Base64String *string
}

func (e Embedding) MarshalJSON() ([]byte, error) {
	if e.Base64String != nil {
		return json.Marshal(e.Base64String)
	}

	if len(e.FloatArray) > 0 {
		return json.Marshal(e.FloatArray)
	}

	return []byte("[]"), nil
}

func (e *Embedding) UnmarshalJSON(data []byte) error {
	var str string

	err := json.Unmarshal(data, &str)
	if err == nil {
		e.Base64String = &str
		return nil
	}

	var floats []float64

	err = json.Unmarshal(data, &floats)
	if err == nil {
		e.FloatArray = floats
		return nil
	}

	return errors.New("invalid embedding type")
}
