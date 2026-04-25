package anthropic

import (
	"encoding/json"
	"fmt"
)

// MessageRequest represents the Anthropic Messages API request format.
type MessageRequest struct {
	MaxTokens int64          `json:"max_tokens" validate:"required,gte=1"`
	Messages  []MessageParam `json:"messages"   validate:"required"`
	Model     string         `json:"model,omitempty"      validate:"required"`

	// The version of the Anthropic API to use.
	//
	// It is required for bedrock and vertex.
	AnthropicVersion string `json:"anthropic_version,omitempty"`

	// Amount of randomness injected into the response.
	//
	// Defaults to `1.0`. Ranges from `0.0` to `1.0`. Use `temperature` closer to `0.0`
	// for analytical / multiple choice, and closer to `1.0` for creative and
	// generative tasks.
	//
	// Note that even with `temperature` of `0.0`, the results will not be fully
	// deterministic.
	Temperature *float64 `json:"temperature,omitempty"`

	// Only sample from the top K options for each subsequent token.
	//
	// Used to remove "long tail" low probability responses.
	// [Learn more technical details here](https://towardsdatascience.com/how-to-sample-from-language-models-682bceb97277).
	//
	// Recommended for advanced use cases only. You usually only need to use
	// `temperature`.
	TopK *int64 `json:"top_k,omitempty"`

	// Use nucleus sampling.
	//
	// In nucleus sampling, we compute the cumulative distribution over all the options
	// for each subsequent token in decreasing probability order and cut it off once it
	// reaches a particular probability specified by `top_p`. You should either alter
	// `temperature` or `top_p`, but not both.
	//
	// Recommended for advanced use cases only. You usually only need to use
	// `temperature`.
	TopP *float64 `json:"top_p,omitempty"`

	// An object describing metadata about the request.
	Metadata *AnthropicMetadata `json:"metadata,omitempty"`

	// Determines whether to use priority capacity (if available) or standard capacity
	// for this request.
	//
	// Anthropic offers different levels of service for your API requests. See
	// [service-tiers](https://docs.anthropic.com/en/api/service-tiers) for details.
	//
	// Any of "auto", "standard_only".
	ServiceTier string `json:"service_tier,omitempty"`

	// Custom text sequences that will cause the model to stop generating.
	//
	// Our models will normally stop when they have naturally completed their turn,
	// which will result in a response `stop_reason` of `"end_turn"`.
	//
	// If you want the model to stop generating when it encounters custom strings of
	// text, you can use the `stop_sequences` parameter. If the model encounters one of
	// the custom sequences, the response `stop_reason` value will be `"stop_sequence"`
	// and the response `stop_sequence` value will contain the matched stop sequence.
	StopSequences []string `json:"stop_sequences,omitempty"`

	// System is an optional system prompt.
	System *SystemPrompt `json:"system,omitempty"`

	// Thinking is an optional thinking configuration.
	Thinking *Thinking `json:"thinking,omitempty"`

	// OutputConfig is an optional output configuration for adaptive thinking.
	OutputConfig *OutputConfig `json:"output_config,omitempty"`

	// Tools is an optional array of tools.
	Tools []Tool `json:"tools,omitempty"`
	// ToolChoice is an optional tool choice configuration.
	ToolChoice *ToolChoice `json:"tool_choice,omitempty"`

	// Stream is an optional flag to enable streaming.
	Stream *bool `json:"stream,omitempty"`

	// MCPServers carries the Anthropic MCP connector payload
	// (mcp-client-2025-11-20 beta). Each entry is an object with
	// type/url/name/authorization_token/tool_configuration. Preserved as
	// raw JSON so spec-level fields (that change faster than Octopus
	// releases) round-trip verbatim. A-H6.
	// Ref: https://platform.claude.com/docs/en/agents-and-tools/mcp-connector
	MCPServers json.RawMessage `json:"mcp_servers,omitempty"`

	// Container configures Anthropic's code-execution sandbox (Claude 4
	// container). Preserved as raw JSON for the same reason as
	// MCPServers. A-H6.
	Container json.RawMessage `json:"container,omitempty"`
}

type AnthropicMetadata struct {
	UserID string `json:"user_id,omitempty"`
}

type SystemPrompt struct {
	Prompt *string `json:"prompt,omitempty"`
	// MultiplePrompts is an optional array of system prompts.
	MultiplePrompts []SystemPromptPart `json:"multiple_prompts,omitempty"`
}

func (s *SystemPrompt) MarshalJSON() ([]byte, error) {
	if s.Prompt != nil {
		return json.Marshal(s.Prompt)
	}

	if len(s.MultiplePrompts) > 0 {
		return json.Marshal(s.MultiplePrompts)
	}

	return []byte("null"), nil
}

func (s *SystemPrompt) UnmarshalJSON(data []byte) error {
	var str string

	err := json.Unmarshal(data, &str)
	if err == nil {
		s.Prompt = &str
		return nil
	}

	var parts []SystemPromptPart

	err = json.Unmarshal(data, &parts)
	if err == nil {
		s.MultiplePrompts = parts
		return nil
	}

	return fmt.Errorf("invalid system prompt format")
}

type SystemPromptPart struct {
	// Type must be "text".
	Type         string        `json:"type" validate:"required,oneof=text"`
	Text         string        `json:"text" validate:"required"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// Thinking type constants
const (
	ThinkingTypeEnabled  = "enabled"
	ThinkingTypeDisabled = "disabled"
	ThinkingTypeAdaptive = "adaptive"
)

// Effort level constants for OutputConfig
const (
	EffortMax    = "max"
	EffortXHigh  = "xhigh"
	EffortHigh   = "high"
	EffortMedium = "medium"
	EffortLow    = "low"
)

// Thinking display constants
const (
	ThinkingDisplaySummarized = "summarized"
	ThinkingDisplayOmitted    = "omitted"
)

type Thinking struct {
	Type         string `json:"type"                    validate:"required,oneof=enabled disabled adaptive"`
	BudgetTokens *int64 `json:"budget_tokens,omitempty" validate:"required_if=Type enabled"`
	Display      string `json:"display,omitempty"       validate:"omitempty,oneof=summarized omitted"`
}

type OutputConfig struct {
	Effort string `json:"effort,omitempty" validate:"omitempty,oneof=max xhigh high medium low"`
}

type ToolChoice struct {
	Type string `json:"type" validate:"required,oneof=auto none tool any"`

	// DisableParallelToolUse is an optional flag to disable parallel tool use.
	DisableParallelToolUse *bool `json:"disable_parallel_tool_use,omitempty"`

	// Name is an optional name of the tool to use, it is required when Type is tool.
	Name *string `json:"name,omitempty" validate:"required_if=Type tool"`
}

// Tool represents a tool definition for Anthropic API.
//
// Anthropic distinguishes between custom (function) tools and server-side tools
// (web_search_*, code_execution_*, computer_*). Server tools carry a distinct
// wire shape with per-type parameters (max_uses, allowed_domains, display_*).
// Instead of enumerating every spec variant, the struct preserves the full raw
// JSON in RawBody so inbound → outbound round-trips stay lossless via custom
// Marshal/Unmarshal.
type Tool struct {
	// Type carries the tool type string. Empty / "function" / "custom" are
	// function-style tools; anything else is a server tool whose wire body we
	// preserve in RawBody.
	Type         string          `json:"type,omitempty"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *CacheControl   `json:"cache_control,omitempty"`

	// DeferLoading marks a tool as lazily-loaded for the tool-search-tool
	// beta (tool-search-tool-2025-10-19). When any tool in a request has
	// this true, collectAnthropicBetaHeaders adds the matching beta
	// header. For server tools the flag lives inside RawBody so MarshalJSON
	// does not need special-casing. A-H7.
	DeferLoading *bool `json:"defer_loading,omitempty"`

	// RawBody preserves the full incoming JSON for server tools so outbound
	// can passthrough spec-specific fields without per-variant modelling.
	// Populated by UnmarshalJSON; consumed by MarshalJSON.
	RawBody json.RawMessage `json:"-"`
}

// IsServerTool reports whether the Type marks this as a Anthropic server-side
// tool (web_search_*, code_execution_*, computer_*, ...).
func (t Tool) IsServerTool() bool {
	return t.Type != "" && t.Type != "function" && t.Type != "custom"
}

// MarshalJSON renders server tools directly from their raw body (optionally
// re-injecting cache_control) so spec-specific fields survive. Function-style
// tools use the default struct marshaling.
func (t Tool) MarshalJSON() ([]byte, error) {
	if t.IsServerTool() && len(t.RawBody) > 0 {
		if t.CacheControl == nil {
			return t.RawBody, nil
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(t.RawBody, &m); err != nil {
			return t.RawBody, nil
		}
		ccBytes, err := json.Marshal(t.CacheControl)
		if err == nil {
			m["cache_control"] = ccBytes
		}
		return json.Marshal(m)
	}
	type alias Tool
	return json.Marshal(alias(t))
}

// UnmarshalJSON populates the struct from either shape and — for server
// tools — retains the full raw body so it can be replayed verbatim later.
func (t *Tool) UnmarshalJSON(data []byte) error {
	type alias Tool
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*t = Tool(a)
	if t.IsServerTool() {
		buf := make([]byte, len(data))
		copy(buf, data)
		t.RawBody = buf
	}
	return nil
}

type CacheControl struct {
	Type string `json:"type" validate:"required,oneof=ephemeral"`
	// The time-to-live for the cache control breakpoint.
	//
	// This may be one the following values:
	//
	// 5m: 5 minutes
	// 1h: 1 hour
	// Defaults to 5m.
	TTL string `json:"ttl,omitempty"`
}

// InputSchema represents the JSON schema for tool input.
type InputSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
}

// MessageParam represents a message in Anthropic format.
type MessageParam struct {
	Role    string         `json:"role"`
	Content MessageContent `json:"content"`
}

// MessageContent supports both string and array formats.
type MessageContent struct {
	Content         *string               `json:"content,omitempty"`
	MultipleContent []MessageContentBlock `json:"multiple_content,omitempty"`
}

func (m MessageContent) ExtractTrivalBlocks(cacheControl *CacheControl) []MessageContentBlock {
	var contentBlocks []MessageContentBlock
	if m.Content != nil && *m.Content != "" {
		contentBlocks = append(contentBlocks, MessageContentBlock{
			Type:         "text",
			Text:         m.Content,
			CacheControl: cacheControl,
		})
	} else if len(m.MultipleContent) > 0 {
		for _, part := range m.MultipleContent {
			if part.Type == "text" && part.Text != nil && *part.Text != "" {
				contentBlocks = append(contentBlocks, part)
			}

			if part.Type == "image_url" {
				contentBlocks = append(contentBlocks, part)
			}
		}
	}

	return contentBlocks
}

func (c MessageContent) MarshalJSON() ([]byte, error) {
	if c.Content != nil {
		return json.Marshal(c.Content)
	}

	return json.Marshal(c.MultipleContent)
}

func (c *MessageContent) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return fmt.Errorf("content cannot be null")
	}

	var blocks []MessageContentBlock

	err := json.Unmarshal(data, &blocks)
	if err == nil {
		c.MultipleContent = blocks
		c.Content = nil
		return nil
	}

	var block MessageContentBlock

	err = json.Unmarshal(data, &block)
	if err == nil && block.Type != "" {
		c.MultipleContent = []MessageContentBlock{block}
		c.Content = nil
		return nil
	}

	var str string

	err = json.Unmarshal(data, &str)
	if err == nil {
		c.Content = &str
		c.MultipleContent = nil
		return nil
	}

	return fmt.Errorf("invalid content type")
}

// MessageContentBlock represents different types of content blocks.
type MessageContentBlock struct {
	// Any of "text", "image", "document", "thinking", "redacted_thinking",
	// "tool_use", "server_tool_use", "tool_result", "web_search_tool_result",
	// "code_execution_tool_result".
	Type string `json:"type"`

	// Text will be present if type is "text".
	// Use pointer to distinguish between "not set" (nil, omitted) and "set to empty" (non-nil, included).
	Text *string `json:"text,omitempty"`

	// Thinking will be present if type is "thinking".
	// Use pointer to distinguish between "not set" (nil, omitted) and "set to empty" (non-nil, included).
	Thinking *string `json:"thinking,omitempty"`

	// Signature will be present if type is "thinking".
	// Use pointer to distinguish between "not set" (nil, omitted) and "set to empty" (non-nil, included).
	Signature *string `json:"signature,omitempty"`

	// Data will be present if type is "redacted_thinking".
	Data string `json:"data,omitempty"`

	// Image / Document source
	Source *ImageSource `json:"source,omitempty"`

	// Document-block metadata (Type == "document").
	Title     string                    `json:"title,omitempty"`
	Context   string                    `json:"context,omitempty"`
	Citations *DocumentCitationsControl `json:"citations,omitempty"`

	// Tool use request
	// tool_use or server_tool_use
	ID           string            `json:"id,omitempty"`
	Name         *string           `json:"name,omitempty"`
	Input        json.RawMessage   `json:"input,omitempty"`
	CacheControl *CacheControl     `json:"cache_control,omitempty"`
	Octopus      *OctopusExtension `json:"_octopus,omitempty"`

	// Tool result fields
	ToolUseID *string `json:"tool_use_id,omitempty"`
	// The content of the tool result.
	// Type can be "text" or "image".
	Content *MessageContent `json:"content,omitempty"`
	IsError *bool           `json:"is_error,omitempty"`
}

type OctopusExtension struct {
	ProviderExtensions *ProviderExtensions `json:"provider_extensions,omitempty"`
}

type ProviderExtensions struct {
	Gemini *GeminiExtension `json:"gemini,omitempty"`
}

type GeminiExtension struct {
	ThoughtSignature string `json:"thought_signature,omitempty"`
}

// DocumentCitationsControl mirrors document.citations on Anthropic document
// blocks. A single `enabled` flag today; we preserve the struct shape for
// forward-compatibility as Anthropic extends citation metadata.
type DocumentCitationsControl struct {
	Enabled bool `json:"enabled,omitempty"`
}

// ImageSource represents the `source` sub-object of an Anthropic content
// block. It serves image / document / url_pdf blocks; Type selects the
// carrier. Fields Data/URL/Text/Content are populated depending on Type.
type ImageSource struct {
	// Type is the source envelope.
	// For image blocks: "base64" | "url"
	// For document blocks: "base64" | "url" | "text" | "content"
	Type string `json:"type"`
	// MediaType is the media type of the payload.
	// Image values: image/png, image/jpeg, image/gif, image/webp
	// Document values: application/pdf, text/plain
	MediaType string `json:"media_type,omitempty"`

	// Data is the base64 payload (Type=="base64") or raw text
	// (Type=="text" for document blocks).
	Data string `json:"data,omitempty"`

	// URL is the payload URL (Type=="url").
	URL string `json:"url,omitempty"`

	// Content holds Anthropic's pre-chunked document content blocks when
	// Type=="content". Preserved as raw JSON for passthrough.
	Content json.RawMessage `json:"content,omitempty"`
}

// StreamEvent represents events in Anthropic streaming response.
type StreamEvent struct {
	// Any of "message_start", "message_delta", "message_stop", "content_block_start",
	// "content_block_delta", "content_block_stop", "error".
	Type string `json:"type"`

	// Message will be present if type is "message_start".
	Message *StreamMessage `json:"message,omitempty"`

	// Index will be present if type is "content_block_start" or "content_block_delta".
	Index *int64 `json:"index,omitempty"`

	// ContentBlock will be present if type is "content_block_start".
	ContentBlock *MessageContentBlock `json:"content_block,omitempty"`

	// Delta will be present if type is "message_delta" or "content_block_delta".
	Delta *StreamDelta `json:"delta,omitempty"`

	Usage *Usage `json:"usage,omitempty"`

	// Error will be present if type is "error".
	Error *ErrorDetail `json:"error,omitempty"`
}

// StreamDelta represents delta in streaming response.
type StreamDelta struct {
	// Type is the type of delta.
	// Any of "text_delta", "input_json_delta", "citations_delta", "thinking_delta",
	// "signature_delta".
	Type *string `json:"type,omitempty"`

	// Text will be present if type is "text_delta".
	Text *string `json:"text,omitempty"`

	// PartialJSON will be present if type is "input_json_delta".
	PartialJSON *string `json:"partial_json,omitempty"`

	// Thinking will be present if type is "thinking_delta".
	Thinking *string `json:"thinking,omitempty"`

	// Signature will be present if type is "signature_delta".
	Signature *string `json:"signature,omitempty"`

	// For "message_delta"
	// Any of "end_turn", "max_tokens", "stop_sequence", "tool_use", "pause_turn",
	// "refusal".
	StopReason *string `json:"stop_reason,omitempty"`

	// For "message_delta"
	StopSequence *string `json:"stop_sequence,omitempty"`
}

// StreamMessage represents the message part of a stream event.
type StreamMessage struct {
	ID      string                `json:"id"`
	Type    string                `json:"type"`
	Role    string                `json:"role"`
	Content []MessageContentBlock `json:"content"`
	Model   string                `json:"model"`
	Usage   *Usage                `json:"usage,omitempty"`
}

// Message represents the Anthropic Messages API response format.
type Message struct {
	ID      string                `json:"id"`
	Type    string                `json:"type"`
	Role    string                `json:"role"`
	Content []MessageContentBlock `json:"content"`
	Model   string                `json:"model"`
	// Any of "end_turn", "max_tokens", "stop_sequence", "tool_use", "pause_turn",
	// "refusal".
	StopReason *string `json:"stop_reason,omitempty"`
	// Which custom stop sequence was generated, if any.
	//
	// This value will be a non-null string if one of your custom stop sequences was
	// generated.
	StopSequence *string `json:"stop_sequence,omitempty"`
	Usage        *Usage  `json:"usage,omitempty"`
}

type ErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// AnthropicError follow the https://platform.claude.com/docs/en/api/errors
type AnthropicError struct {
	Type       string      `json:"type,omitempty"`
	StatusCode int         `json:"-"`
	RequestID  string      `json:"request_id"`
	Error      ErrorDetail `json:"error"`
}

// Usage represents usage information in Anthropic format.
type Usage struct {
	// The number of input tokens which were used to bill.
	InputTokens int64 `json:"input_tokens,omitempty"`

	// The number of output tokens which were used.
	OutputTokens int64 `json:"output_tokens,omitempty"`

	// The number of input tokens used to create the cache entry.
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`

	// The number of input tokens read from the cache.
	CacheReadInputTokens int64 `json:"cache_read_input_tokens,omitempty"`

	// Breakdown of cache creation tokens by TTL bucket (extended-cache-ttl beta).
	CacheCreation *CacheCreationUsage `json:"cache_creation,omitempty"`

	// Available options: standard, priority, batch
	ServiceTier string `json:"service_tier,omitempty"`
}

// CacheCreationUsage breaks cache_creation_input_tokens down by TTL bucket when
// Anthropic's extended cache TTL beta is enabled.
type CacheCreationUsage struct {
	Ephemeral5mInputTokens int64 `json:"ephemeral_5m_input_tokens,omitempty"`
	Ephemeral1hInputTokens int64 `json:"ephemeral_1h_input_tokens,omitempty"`
}
