package openai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/samber/lo"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// ResponseOutbound implements the Outbound interface for OpenAI Responses API.
type ResponseOutbound struct {
	// Stream state tracking
	streamID    string
	streamModel string
	initialized bool
	outputItems map[int]ResponsesItem
}

func (o *ResponseOutbound) TransformRequest(ctx context.Context, request *model.InternalLLMRequest, baseUrl, key string) (*http.Request, error) {
	if request == nil {
		return nil, fmt.Errorf("request is nil")
	}

	request.NormalizeMessages()

	// Convert to Responses API request format
	responsesReq := ConvertToResponsesRequest(request)

	body, err := json.Marshal(responsesReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal responses api request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	applyOpenAIOrgProjectHeaders(req, request)

	// Parse and set URL
	parsedUrl, err := url.Parse(strings.TrimSuffix(baseUrl, "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}
	parsedUrl.Path = parsedUrl.Path + "/responses"
	req.URL = parsedUrl
	req.Method = http.MethodPost

	return req, nil
}

// TransformRequestRaw keeps the original OpenAI Responses payload intact and only rewrites
// the top-level model to the selected upstream model.
func (o *ResponseOutbound) TransformRequestRaw(ctx context.Context, rawBody []byte, modelName, baseUrl, key string, query url.Values) (*http.Request, error) {
	if len(rawBody) == 0 {
		return nil, fmt.Errorf("raw body is empty")
	}
	if strings.TrimSpace(modelName) != "" {
		rewrittenBody, err := rewriteRawResponsesRequestModel(rawBody, modelName)
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

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	// OpenAI-Organization / OpenAI-Project are forwarded by the relay's
	// copyHeaders on the raw-passthrough path, so no explicit application
	// is needed here (O-M7).

	parsedURL, err := url.Parse(strings.TrimSuffix(baseUrl, "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}
	parsedURL.Path = parsedURL.Path + "/responses"
	if query != nil {
		parsedURL.RawQuery = query.Encode()
	}
	req.URL = parsedURL
	req.Method = http.MethodPost

	return req, nil
}

func rewriteRawResponsesRequestModel(rawBody []byte, modelName string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return nil, fmt.Errorf("failed to decode raw responses request: %w", err)
	}
	payload["model"] = strings.TrimSpace(modelName)
	rewrittenBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode raw responses request: %w", err)
	}
	return rewrittenBody, nil
}

func (o *ResponseOutbound) TransformResponse(ctx context.Context, response *http.Response) (*model.InternalLLMResponse, error) {
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
		var errResp struct {
			Error model.ErrorDetail `json:"error"`
		}
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
			return nil, &model.ResponseError{
				StatusCode: response.StatusCode,
				Detail:     errResp.Error,
			}
		}
		return nil, fmt.Errorf("HTTP error %d: %s", response.StatusCode, string(body))
	}

	var resp ResponsesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal responses api response: %w", err)
	}

	// Convert to internal response
	return convertToLLMResponseFromResponses(&resp), nil
}

func (o *ResponseOutbound) TransformStreamEvent(ctx context.Context, eventData []byte) ([]model.StreamEvent, error) {
	if len(eventData) == 0 {
		return nil, nil
	}
	if bytes.HasPrefix(eventData, []byte("[DONE]")) {
		return []model.StreamEvent{{Kind: model.StreamEventKindDone}}, nil
	}

	if !o.initialized {
		o.initialized = true
		o.outputItems = make(map[int]ResponsesItem)
	}

	var streamEvent ResponsesStreamEvent
	if err := json.Unmarshal(eventData, &streamEvent); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stream event: %w", err)
	}

	if streamEvent.Response != nil {
		if streamEvent.Response.ID != "" {
			o.streamID = streamEvent.Response.ID
		}
		if streamEvent.Response.Model != "" {
			o.streamModel = streamEvent.Response.Model
		}
	}

	var events []model.StreamEvent
	base := model.StreamEvent{ID: o.streamID, Model: o.streamModel, Index: 0}

	switch streamEvent.Type {
	case "response.created", "response.in_progress":
		events = append(events, model.StreamEvent{Kind: model.StreamEventKindMessageStart, ID: base.ID, Model: base.Model, Index: base.Index, Role: "assistant"})

	case "response.output_text.delta":
		o.mergeOutputTextDelta(streamEvent)
		if streamEvent.Delta != "" {
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindTextDelta, ID: base.ID, Model: base.Model, Index: base.Index, Delta: &model.StreamDelta{Text: streamEvent.Delta}})
		}

	case "response.function_call_arguments.delta":
		o.mergeFunctionCallDelta(streamEvent)
		if streamEvent.Delta != "" {
			toolCall := model.ToolCall{
				Index: streamEvent.OutputIndex,
				ID:    streamEvent.CallID,
				Type:  "function",
				Function: model.FunctionCall{
					Name: streamEvent.Name,
				},
			}
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindToolCallStart, ID: base.ID, Model: base.Model, Index: streamEvent.OutputIndex, ToolCall: &toolCall})
			toolCall.Function.Arguments = streamEvent.Delta
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindToolCallDelta, ID: base.ID, Model: base.Model, Index: streamEvent.OutputIndex, ToolCall: &toolCall, Delta: &model.StreamDelta{Arguments: streamEvent.Delta}})
		}

	case "response.output_item.added":
		o.mergeOutputItemAdded(streamEvent)
		if streamEvent.Item != nil && streamEvent.Item.Type == "function_call" {
			toolCall := model.ToolCall{
				Index: streamEvent.OutputIndex,
				ID:    streamEvent.Item.CallID,
				Type:  "function",
				Function: model.FunctionCall{
					Name: streamEvent.Item.Name,
				},
			}
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindToolCallStart, ID: base.ID, Model: base.Model, Index: streamEvent.OutputIndex, ToolCall: &toolCall})
		}

	case "response.reasoning_summary_text.delta":
		o.mergeReasoningDelta(streamEvent)
		if streamEvent.Delta != "" {
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindThinkingDelta, ID: base.ID, Model: base.Model, Index: base.Index, Delta: &model.StreamDelta{Thinking: streamEvent.Delta}})
		}

	case "response.refusal.delta":
		if streamEvent.Delta != "" {
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindTextDelta, ID: base.ID, Model: base.Model, Index: base.Index, Delta: &model.StreamDelta{Refusal: streamEvent.Delta}})
		}

	case "response.refusal.done":
		return nil, nil

	case "response.completed":
		if streamEvent.Response != nil {
			if len(streamEvent.Response.Output) > 0 {
				if rawOutput, marshalErr := json.Marshal(sanitizeResponsesItems(streamEvent.Response.Output)); marshalErr == nil {
					base.ProviderExtensions = &model.ProviderExtensions{OpenAI: &model.OpenAIExtension{RawResponseItems: rawOutput}}
				}
			} else if rawOutput, ok := o.marshalTrackedOutputItems(); ok {
				base.ProviderExtensions = &model.ProviderExtensions{OpenAI: &model.OpenAIExtension{RawResponseItems: rawOutput}}
			}
			finishReason, respErr := normalizeResponsesFinishReason(streamEvent.Response.Status, streamEvent.Response.Error)
			if respErr != nil {
				events = append(events, model.StreamEvent{Kind: model.StreamEventKindError, ID: base.ID, Model: base.Model, Error: respErr})
				return events, nil
			}
			if finishReason != nil && *finishReason == "stop" && o.responseCarriesFunctionCall(streamEvent.Response) {
				finishReason = lo.ToPtr("tool_calls")
			}
			stopEvent := model.StreamEvent{Kind: model.StreamEventKindMessageStop, ID: base.ID, Model: base.Model, Index: base.Index, StopReason: model.ParseFinishReason(lo.FromPtr(finishReason)), ProviderExtensions: base.ProviderExtensions}
			events = append(events, stopEvent)
			if streamEvent.Response.Usage != nil {
				usage := convertResponsesUsage(streamEvent.Response.Usage)
				usageEvent := model.StreamEvent{Kind: model.StreamEventKindUsageDelta, ID: base.ID, Model: base.Model, Usage: usage, ProviderExtensions: base.ProviderExtensions}
				events = append(events, usageEvent)
			}
		}

	case "response.failed", "response.incomplete", "error":
		var reason *string
		var respErr *model.ResponseError
		switch streamEvent.Type {
		case "response.incomplete":
			reason = lo.ToPtr("length")
		default:
			reason = lo.ToPtr("stop")
		}
		if streamEvent.Response != nil && streamEvent.Response.Error != nil {
			respErr = &model.ResponseError{
				Detail: model.ErrorDetail{
					Code:    fmt.Sprintf("%d", streamEvent.Response.Error.Code),
					Message: streamEvent.Response.Error.Message,
				},
			}
		} else if streamEvent.Code != "" || streamEvent.Message != "" {
			respErr = &model.ResponseError{
				Detail: model.ErrorDetail{
					Code:    streamEvent.Code,
					Message: streamEvent.Message,
				},
			}
		}
		if respErr != nil {
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindError, ID: base.ID, Model: base.Model, Error: respErr})
		} else {
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindMessageStop, ID: base.ID, Model: base.Model, Index: base.Index, StopReason: model.ParseFinishReason(lo.FromPtr(reason))})
		}

	default:
		return nil, nil
	}

	return events, nil
}

func (o *ResponseOutbound) TransformStream(ctx context.Context, eventData []byte) (*model.InternalLLMResponse, error) {
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
		o.initialized = true
		o.outputItems = make(map[int]ResponsesItem)
	}

	// Parse the streaming event
	var streamEvent ResponsesStreamEvent
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
	case "response.created", "response.in_progress":
		if streamEvent.Response != nil {
			o.streamID = streamEvent.Response.ID
			o.streamModel = streamEvent.Response.Model
			resp.ID = o.streamID
			resp.Model = o.streamModel
		}
		resp.Choices = []model.Choice{
			{
				Index: 0,
				Delta: &model.Message{
					Role: "assistant",
				},
			},
		}

	case "response.output_text.delta":
		o.mergeOutputTextDelta(streamEvent)
		resp.Choices = []model.Choice{
			{
				Index: 0,
				Delta: &model.Message{
					Role: "assistant",
					Content: model.MessageContent{
						Content: lo.ToPtr(streamEvent.Delta),
					},
				},
			},
		}

	case "response.function_call_arguments.delta":
		o.mergeFunctionCallDelta(streamEvent)
		resp.Choices = []model.Choice{
			{
				Index: 0,
				Delta: &model.Message{
					Role: "assistant",
					ToolCalls: []model.ToolCall{
						{
							Index: streamEvent.OutputIndex,
							ID:    streamEvent.CallID,
							Type:  "function",
							Function: model.FunctionCall{
								Name:      streamEvent.Name,
								Arguments: streamEvent.Delta,
							},
						},
					},
				},
			},
		}

	case "response.output_item.added":
		o.mergeOutputItemAdded(streamEvent)
		if streamEvent.Item != nil && streamEvent.Item.Type == "function_call" {
			resp.Choices = []model.Choice{
				{
					Index: 0,
					Delta: &model.Message{
						Role: "assistant",
						ToolCalls: []model.ToolCall{
							{
								Index: streamEvent.OutputIndex,
								ID:    streamEvent.Item.CallID,
								Type:  "function",
								Function: model.FunctionCall{
									Name: streamEvent.Item.Name,
								},
							},
						},
					},
				},
			}
		} else {
			return nil, nil
		}

	case "response.reasoning_summary_text.delta":
		o.mergeReasoningDelta(streamEvent)
		resp.Choices = []model.Choice{
			{
				Index: 0,
				Delta: &model.Message{
					Role:             "assistant",
					ReasoningContent: lo.ToPtr(streamEvent.Delta),
				},
			},
		}

	case "response.refusal.delta":
		// Responses surfaces safety refusals as a distinct content-part stream
		// (response.refusal.delta / response.refusal.done). Forward the delta
		// on Choice.Delta.Refusal so downstream Chat Completions / Responses
		// inbounds can emit the matching refusal events — the inbound already
		// knows how to open a refusal content part when it sees this field.
		resp.Choices = []model.Choice{
			{
				Index: 0,
				Delta: &model.Message{
					Role:    "assistant",
					Refusal: streamEvent.Delta,
				},
			},
		}

	case "response.refusal.done":
		// `done` carries the full refusal text which has already been streamed
		// through `delta` events. Re-emitting it would double-count on the
		// inbound accumulator, so drop it here — the inbound closes the
		// refusal content part via its own terminal flow (closeMessageItem /
		// finish_reason).
		return nil, nil

	case "response.completed":
		if streamEvent.Response != nil {
			if len(streamEvent.Response.Output) > 0 {
				if rawOutput, marshalErr := json.Marshal(sanitizeResponsesItems(streamEvent.Response.Output)); marshalErr == nil {
					resp.RawResponsesOutputItems = rawOutput
				}
			} else if rawOutput, ok := o.marshalTrackedOutputItems(); ok {
				resp.RawResponsesOutputItems = rawOutput
			}
			finishReason, respErr := normalizeResponsesFinishReason(streamEvent.Response.Status, streamEvent.Response.Error)
			if respErr != nil {
				resp.Error = respErr
			}
			// A response may "complete" while still carrying function_call
			// items in its output. The non-streaming path already applies
			// this same override (see convertToLLMResponseFromResponses);
			// mirror it here so the streaming Chat Completions finish_reason
			// correctly signals "tool_calls" — Agent SDKs rely on the value
			// to decide whether to run the requested tool.
			if finishReason != nil && *finishReason == "stop" && o.responseCarriesFunctionCall(streamEvent.Response) {
				finishReason = lo.ToPtr("tool_calls")
			}
			resp.Choices = []model.Choice{
				{
					Index:        0,
					FinishReason: finishReason,
				},
			}
			if streamEvent.Response.Usage != nil {
				resp.Usage = convertResponsesUsage(streamEvent.Response.Usage)
			}
		}

	case "response.failed", "response.incomplete", "error":
		// Synthesize a terminal chunk with a legal finish_reason. Previously
		// we emitted FinishReason="error" which is not in the OpenAI Chat
		// Completions enum (stop | length | tool_calls | content_filter |
		// function_call); strict downstream SDKs (Pydantic AI, OpenAI Python)
		// reject the entire response. Pick a legal value and surface the
		// original cause via ResponseError so the inbound layer can forward
		// it to the client.
		var reason *string
		var respErr *model.ResponseError
		switch streamEvent.Type {
		case "response.incomplete":
			reason = lo.ToPtr("length")
		default:
			reason = lo.ToPtr("stop")
		}
		if streamEvent.Response != nil && streamEvent.Response.Error != nil {
			respErr = &model.ResponseError{
				Detail: model.ErrorDetail{
					Code:    fmt.Sprintf("%d", streamEvent.Response.Error.Code),
					Message: streamEvent.Response.Error.Message,
				},
			}
		} else if streamEvent.Code != "" || streamEvent.Message != "" {
			respErr = &model.ResponseError{
				Detail: model.ErrorDetail{
					Code:    streamEvent.Code,
					Message: streamEvent.Message,
				},
			}
		}
		resp.Error = respErr
		resp.Choices = []model.Choice{
			{
				Index:        0,
				FinishReason: reason,
			},
		}

	default:
		// Skip unhandled events
		return nil, nil
	}

	return resp, nil
}

// ResponsesRequest represents the OpenAI Responses API request format.
type ResponsesRequest struct {
	Model             string                `json:"model"`
	Instructions      string                `json:"instructions,omitempty"`
	Input             ResponsesInput        `json:"input"`
	Tools             []ResponsesTool       `json:"tools,omitempty"`
	ToolChoice        *ResponsesToolChoice  `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool                 `json:"parallel_tool_calls,omitempty"`
	Stream            *bool                 `json:"stream,omitempty"`
	Text              *ResponsesTextOptions `json:"text,omitempty"`
	Store             *bool                 `json:"store,omitempty"`
	ServiceTier       *string               `json:"service_tier,omitempty"`
	Truncation        *string               `json:"truncation,omitempty"`
	User              *string               `json:"user,omitempty"`
	Metadata          map[string]string     `json:"metadata,omitempty"`
	MaxOutputTokens   *int64                `json:"max_output_tokens,omitempty"`
	Temperature       *float64              `json:"temperature,omitempty"`
	TopP              *float64              `json:"top_p,omitempty"`
	Reasoning         *ResponsesReasoning   `json:"reasoning,omitempty"`

	// Pass-through fields
	PreviousResponseID   *string         `json:"previous_response_id,omitempty"`
	Background           *bool           `json:"background,omitempty"`
	Prompt               json.RawMessage `json:"prompt,omitempty"`
	PromptCacheKey       *string         `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention *string         `json:"prompt_cache_retention,omitempty"`
	SafetyIdentifier     *string         `json:"safety_identifier,omitempty"`
	MaxToolCalls         *int64          `json:"max_tool_calls,omitempty"`
	Conversation         json.RawMessage `json:"conversation,omitempty"`
	ContextManagement    json.RawMessage `json:"context_management,omitempty"`
	StreamOptions        json.RawMessage `json:"stream_options,omitempty"`
	Include              []string        `json:"include,omitempty"`
	TopLogprobs          *int64          `json:"top_logprobs,omitempty"`
}

type ResponsesInput struct {
	Text  *string
	Items []ResponsesItem
	Raw   json.RawMessage
}

func (i ResponsesInput) MarshalJSON() ([]byte, error) {
	if len(i.Raw) > 0 {
		return json.Marshal(json.RawMessage(i.Raw))
	}
	if i.Text != nil {
		return json.Marshal(i.Text)
	}
	return json.Marshal(i.Items)
}

func (i *ResponsesInput) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		i.Text = &text
		return nil
	}
	var items []ResponsesItem
	if err := json.Unmarshal(data, &items); err == nil {
		i.Items = items
		return nil
	}
	return fmt.Errorf("invalid input format")
}

type ResponsesItem struct {
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type,omitempty"`
	Role     string          `json:"role,omitempty"`
	Content  *ResponsesInput `json:"content,omitempty"`
	Status   *string         `json:"status,omitempty"`
	Text     *string         `json:"text,omitempty"`
	Refusal  *string         `json:"refusal,omitempty"`
	ImageURL *string         `json:"image_url,omitempty"`
	Detail   *string         `json:"detail,omitempty"`

	// Annotations for output_text content
	Annotations []ResponsesAnnotation `json:"annotations,omitempty"`

	// Function call fields
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`

	// Function call output
	Output *ResponsesInput `json:"output,omitempty"`

	// Image generation fields
	Result       *string `json:"result,omitempty"`
	Background   *string `json:"background,omitempty"`
	OutputFormat *string `json:"output_format,omitempty"`
	Quality      *string `json:"quality,omitempty"`
	Size         *string `json:"size,omitempty"`

	// Reasoning fields
	Summary          []ResponsesReasoningSummary `json:"summary,omitempty"`
	EncryptedContent *string                     `json:"encrypted_content,omitempty"`

	// Multimodal input passthrough for Responses→Responses routing. O-H6.
	FileID     *string              `json:"file_id,omitempty"`
	Filename   *string              `json:"filename,omitempty"`
	FileData   *string              `json:"file_data,omitempty"`
	FileURL    *string              `json:"file_url,omitempty"`
	InputAudio *ResponsesInputAudio `json:"input_audio,omitempty"`
}

// ResponsesInputAudio mirrors OpenAI's nested `input_audio` object for audio
// content parts on Responses input. O-H6.
type ResponsesInputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format,omitempty"`
}

type ResponsesReasoningSummary struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ResponsesAnnotation struct {
	Type       string  `json:"type"`
	StartIndex *int    `json:"start_index,omitempty"`
	EndIndex   *int    `json:"end_index,omitempty"`
	URL        *string `json:"url,omitempty"`
	Title      *string `json:"title,omitempty"`
	FileID     *string `json:"file_id,omitempty"`
	Filename   *string `json:"filename,omitempty"`
}

type ResponsesTool struct {
	Type              string         `json:"type,omitempty"`
	Name              string         `json:"name,omitempty"`
	Description       string         `json:"description,omitempty"`
	Parameters        map[string]any `json:"parameters,omitempty"`
	Strict            *bool          `json:"strict,omitempty"`
	Background        string         `json:"background,omitempty"`
	OutputFormat      string         `json:"output_format,omitempty"`
	Quality           string         `json:"quality,omitempty"`
	Size              string         `json:"size,omitempty"`
	OutputCompression *int64         `json:"output_compression,omitempty"`
}

type ResponsesToolChoice struct {
	Mode *string `json:"mode,omitempty"`
	Type *string `json:"type,omitempty"`
	Name *string `json:"name,omitempty"`
}

func (t ResponsesToolChoice) MarshalJSON() ([]byte, error) {
	// If only Mode is set and it's a simple mode like "auto", "none", "required"
	if t.Mode != nil && t.Type == nil && t.Name == nil {
		return json.Marshal(*t.Mode)
	}
	// Otherwise, serialize as an object
	type Alias ResponsesToolChoice
	return json.Marshal(Alias(t))
}

type ResponsesTextOptions struct {
	Format    *ResponsesTextFormat `json:"format,omitempty"`
	Verbosity *string              `json:"verbosity,omitempty"`
}

type ResponsesTextFormat struct {
	Type   string          `json:"type,omitempty"`
	Name   string          `json:"name,omitempty"`
	Schema json.RawMessage `json:"schema,omitempty"`
}

type ResponsesReasoning struct {
	Effort          string  `json:"effort,omitempty"`
	MaxTokens       *int64  `json:"max_tokens,omitempty"`
	Summary         *string `json:"summary,omitempty"`
	GenerateSummary *string `json:"generate_summary,omitempty"`
}

// ResponsesResponse represents the OpenAI Responses API response format.
type ResponsesResponse struct {
	Object    string          `json:"object"`
	ID        string          `json:"id"`
	Model     string          `json:"model"`
	CreatedAt int64           `json:"created_at"`
	Output    []ResponsesItem `json:"output"`
	Status    *string         `json:"status,omitempty"`
	Usage     *ResponsesUsage `json:"usage,omitempty"`
	Error     *ResponsesError `json:"error,omitempty"`
}

type ResponsesUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	InputTokenDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokens       int64 `json:"output_tokens"`
	OutputTokenDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
	TotalTokens int64 `json:"total_tokens"`
}

type ResponsesError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ResponsesStreamEvent struct {
	Type           string             `json:"type"`
	SequenceNumber int                `json:"sequence_number"`
	Response       *ResponsesResponse `json:"response,omitempty"`
	OutputIndex    int                `json:"output_index"`
	Item           *ResponsesItem     `json:"item,omitempty"`
	ItemID         *string            `json:"item_id,omitempty"`
	ContentIndex   *int               `json:"content_index,omitempty"`
	Delta          string             `json:"delta,omitempty"`
	Text           string             `json:"text,omitempty"`
	Name           string             `json:"name,omitempty"`
	CallID         string             `json:"call_id,omitempty"`
	Arguments      string             `json:"arguments,omitempty"`
	SummaryIndex   *int               `json:"summary_index,omitempty"`
	Code           string             `json:"code,omitempty"`
	Message        string             `json:"message,omitempty"`
}

const anthropicPromptCacheRetention24h = "24h"

func anthropicCacheMetadataForResponses(req *model.InternalLLMRequest) (*string, *string) {
	if req == nil {
		return nil, nil
	}
	if req.ResponsesPromptCacheKey != nil || req.PromptCacheRetention != nil {
		return req.ResponsesPromptCacheKey, req.PromptCacheRetention
	}

	return derivedAnthropicCacheMetadata(req)
}

func derivedAnthropicCacheMetadata(req *model.InternalLLMRequest) (*string, *string) {
	projection := deriveAnthropicCacheProjection(req)
	if !projection.ok {
		return nil, nil
	}

	sum := sha256.Sum256(projection.payload)
	key := "anthropic-cache-" + hex.EncodeToString(sum[:])
	return &key, projection.retention
}

func buildAnthropicCacheKeyPayload(req *model.InternalLLMRequest) []byte {
	projection := buildAnthropicCacheProjection(req)
	if !projection.ok {
		return nil
	}
	return projection.payload
}

type anthropicCacheProjection struct {
	payload     []byte
	retention   *string
	anchor      string
	ok          bool
	cacheSignal bool
}

type anthropicCacheTool struct {
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
	TTL         string          `json:"ttl,omitempty"`
}

type anthropicCachePart struct {
	Type    string          `json:"type,omitempty"`
	Text    string          `json:"text,omitempty"`
	Name    string          `json:"name,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
	TTL     string          `json:"ttl,omitempty"`
}

type anthropicCacheMessage struct {
	Role  string               `json:"role,omitempty"`
	Parts []anthropicCachePart `json:"parts,omitempty"`
}

type anthropicCachePayload struct {
	Instructions string                  `json:"instructions,omitempty"`
	Tools        []anthropicCacheTool    `json:"tools,omitempty"`
	Messages     []anthropicCacheMessage `json:"messages,omitempty"`
}

func deriveAnthropicCacheProjection(req *model.InternalLLMRequest) anthropicCacheProjection {
	return buildAnthropicCacheProjection(req)
}

func buildAnthropicCacheProjection(req *model.InternalLLMRequest) anthropicCacheProjection {
	if req == nil {
		return anthropicCacheProjection{}
	}

	projection := anthropicCacheProjection{cacheSignal: requestHasCacheControl(req)}
	if !projection.cacheSignal && req.RawAPIFormat != model.APIFormatAnthropicMessage {
		return projection
	}

	payload := anthropicCachePayload{
		Instructions: strings.TrimSpace(convertInstructionsFromMessages(req.Messages)),
	}
	selectedTTL := ""

	for _, tool := range req.Tools {
		if tool.Type != "function" {
			continue
		}
		payload.Tools = append(payload.Tools, anthropicCacheTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  cloneRawJSON(tool.Function.Parameters),
			Strict:      tool.Function.Strict,
			TTL:         cacheControlTTL(tool.CacheControl),
		})
		if selectedTTL == "" {
			selectedTTL = cacheControlTTL(tool.CacheControl)
		}
	}

	stableMessageLimit, messageAnchor, messageTTL := stableAnthropicMessageLimit(req.Messages)
	if payload.Instructions != "" {
		projection.anchor = "system"
		if selectedTTL == "" {
			selectedTTL = stableSystemCacheTTL(req.Messages)
		}
	}
	if len(payload.Tools) > 0 {
		projection.anchor = "tools"
	}
	if stableMessageLimit >= 0 {
		if projection.anchor == "" || projection.anchor == "system" {
			projection.anchor = messageAnchor
		}
		if selectedTTL == "" {
			selectedTTL = messageTTL
		}
		for i := 0; i <= stableMessageLimit && i < len(req.Messages); i++ {
			msg := req.Messages[i]
			if msg.Role == "system" || msg.Role == "developer" {
				continue
			}
			if cacheMsg, ok := cacheMessageFromMessage(msg); ok {
				payload.Messages = append(payload.Messages, cacheMsg)
			}
		}
	}

	if payload.Instructions == "" && len(payload.Tools) == 0 && len(payload.Messages) == 0 {
		projection.anchor = "none"
		return projection
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return projection
	}
	projection.payload = data
	projection.ok = true
	if selectedTTL == model.CacheTTL1h {
		value := anthropicPromptCacheRetention24h
		projection.retention = &value
	}
	if projection.anchor == "" {
		projection.anchor = "unknown"
	}
	return projection
}

func stableAnthropicMessageLimit(messages []model.Message) (int, string, string) {
	userIndexes := make([]int, 0, len(messages))
	for i, msg := range messages {
		if msg.Role == "user" {
			userIndexes = append(userIndexes, i)
		}
	}
	if len(userIndexes) >= 2 {
		idx := userIndexes[len(userIndexes)-2]
		return idx, "previous_user", messageCacheTTL(messages[idx])
	}
	if len(userIndexes) == 1 {
		idx := userIndexes[0]
		return idx, "final_user_fallback", messageCacheTTL(messages[idx])
	}
	return -1, "", ""
}

func cacheMessageFromMessage(msg model.Message) (anthropicCacheMessage, bool) {
	cacheMessage := anthropicCacheMessage{Role: msg.Role}
	if msg.Content.Content != nil {
		cacheMessage.Parts = append(cacheMessage.Parts, anthropicCachePart{Type: "text", Text: strings.TrimSpace(*msg.Content.Content), TTL: cacheControlTTL(msg.CacheControl)})
	}
	for _, part := range msg.Content.MultipleContent {
		cachePart := anthropicCachePart{Type: part.Type, TTL: cacheControlTTL(part.CacheControl)}
		switch part.Type {
		case "text":
			if part.Text != nil {
				cachePart.Text = strings.TrimSpace(*part.Text)
			}
		case "server_tool_use":
			if part.ServerToolUse != nil {
				cachePart.Name = part.ServerToolUse.Name
				cachePart.Input = cloneRawJSON(part.ServerToolUse.Input)
			}
		case "server_tool_result":
			if part.ServerToolResult != nil {
				cachePart.IsError = part.ServerToolResult.IsError != nil && *part.ServerToolResult.IsError
				cachePart.Content = cloneRawJSON(part.ServerToolResult.Content)
			}
		default:
			continue
		}
		cacheMessage.Parts = append(cacheMessage.Parts, cachePart)
	}
	if msg.ToolCallID != nil && msg.Content.Content != nil {
		cacheMessage.Parts = append(cacheMessage.Parts, anthropicCachePart{Type: "tool_result", Text: strings.TrimSpace(*msg.Content.Content), TTL: cacheControlTTL(msg.CacheControl)})
	}
	for _, toolCall := range msg.ToolCalls {
		cacheMessage.Parts = append(cacheMessage.Parts, anthropicCachePart{Type: "tool_use", Name: toolCall.Function.Name, Text: strings.TrimSpace(toolCall.Function.Arguments), TTL: cacheControlTTL(toolCall.CacheControl)})
	}
	return cacheMessage, len(cacheMessage.Parts) > 0
}

func requestHasCacheControl(req *model.InternalLLMRequest) bool {
	if req == nil {
		return false
	}
	for _, msg := range req.Messages {
		if msg.CacheControl != nil {
			return true
		}
		for _, part := range msg.Content.MultipleContent {
			if part.CacheControl != nil {
				return true
			}
		}
		for _, toolCall := range msg.ToolCalls {
			if toolCall.CacheControl != nil {
				return true
			}
		}
	}
	for _, tool := range req.Tools {
		if tool.CacheControl != nil {
			return true
		}
	}
	return false
}

func cacheControlTTL(cacheControl *model.CacheControl) string {
	if cacheControl == nil {
		return ""
	}
	return cacheControl.TTL
}

func stableSystemCacheTTL(messages []model.Message) string {
	for _, msg := range messages {
		if msg.Role != "system" && msg.Role != "developer" {
			continue
		}
		if ttl := cacheControlTTL(msg.CacheControl); ttl != "" {
			return ttl
		}
		for _, part := range msg.Content.MultipleContent {
			if ttl := cacheControlTTL(part.CacheControl); ttl != "" {
				return ttl
			}
		}
	}
	return ""
}

func messageCacheTTL(msg model.Message) string {
	if ttl := cacheControlTTL(msg.CacheControl); ttl != "" {
		return ttl
	}
	for _, part := range msg.Content.MultipleContent {
		if ttl := cacheControlTTL(part.CacheControl); ttl != "" {
			return ttl
		}
	}
	for _, toolCall := range msg.ToolCalls {
		if ttl := cacheControlTTL(toolCall.CacheControl); ttl != "" {
			return ttl
		}
	}
	return ""
}

func anthropicCacheTTLPresent(req *model.InternalLLMRequest, ttl string) bool {
	if req == nil || ttl == "" {
		return false
	}
	for _, msg := range req.Messages {
		if msg.CacheControl != nil && msg.CacheControl.TTL == ttl {
			return true
		}
		for _, part := range msg.Content.MultipleContent {
			if part.CacheControl != nil && part.CacheControl.TTL == ttl {
				return true
			}
		}
		for _, toolCall := range msg.ToolCalls {
			if toolCall.CacheControl != nil && toolCall.CacheControl.TTL == ttl {
				return true
			}
		}
	}
	for _, tool := range req.Tools {
		if tool.CacheControl != nil && tool.CacheControl.TTL == ttl {
			return true
		}
	}
	return false
}

func cloneRawJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func ConvertToResponsesRequest(req *model.InternalLLMRequest) *ResponsesRequest {
	// `user` is deprecated on OpenAI text APIs and is rejected by some
	// OpenAI-compatible upstreams, so keep the modern identifiers
	// (`prompt_cache_key` / `safety_identifier`) and omit the legacy field.
	promptCacheKey, promptCacheRetention := anthropicCacheMetadataForResponses(req)
	result := &ResponsesRequest{
		Model:                req.Model,
		Temperature:          req.Temperature,
		TopP:                 req.TopP,
		Stream:               req.Stream,
		Store:                req.Store,
		ServiceTier:          req.ServiceTier,
		Truncation:           req.Truncation,
		Metadata:             req.Metadata,
		MaxOutputTokens:      req.MaxCompletionTokens,
		ParallelToolCalls:    req.ParallelToolCalls,
		PromptCacheKey:       promptCacheKey,
		PromptCacheRetention: promptCacheRetention,
	}

	// Convert instructions from system messages
	result.Instructions = convertInstructionsFromMessages(req.Messages)

	// Convert input from messages or preserve original array items when available.
	result.Input = buildResponsesInput(req)

	// Convert tools
	if len(req.Tools) > 0 {
		result.Tools = convertToolsToResponses(req.Tools)
	}

	// Convert tool choice
	if req.ToolChoice != nil {
		result.ToolChoice = convertToolChoiceToResponses(req.ToolChoice)
	}

	// Convert text options
	if req.ResponseFormat != nil {
		format := &ResponsesTextFormat{
			Type: req.ResponseFormat.Type,
			Name: req.ResponseFormat.Name,
		}
		// Prefer the parsed Schema so nested fields survive round-trips;
		// fall back to RawSchema (passthrough) then the legacy JSONSchema
		// blob for callers that never populated the new fields.
		if req.ResponseFormat.Schema != nil {
			if b, err := req.ResponseFormat.Schema.ToOpenAIResponseFormat(); err == nil {
				format.Schema = b
			}
		}
		if len(format.Schema) == 0 && len(req.ResponseFormat.RawSchema) > 0 {
			format.Schema = req.ResponseFormat.RawSchema
		}
		if len(format.Schema) == 0 && len(req.ResponseFormat.JSONSchema) > 0 {
			format.Schema = req.ResponseFormat.JSONSchema
		}
		result.Text = &ResponsesTextOptions{Format: format}
	}

	// Verbosity (O-M8) is a sibling of format on Responses text. Attach it
	// even when ResponseFormat is nil — the gpt-5 verbosity knob can be
	// used with plain-text output too.
	if req.Verbosity != nil && strings.TrimSpace(*req.Verbosity) != "" {
		if result.Text == nil {
			result.Text = &ResponsesTextOptions{}
		}
		result.Text.Verbosity = req.Verbosity
	}

	// Convert reasoning
	if req.ReasoningEffort != "" || req.ReasoningBudget != nil || req.ReasoningSummary != nil || req.ReasoningGenerateSummary != nil {
		result.Reasoning = &ResponsesReasoning{
			Effort:          req.ReasoningEffort,
			MaxTokens:       req.ReasoningBudget,
			Summary:         req.ReasoningSummary,
			GenerateSummary: req.ReasoningGenerateSummary,
		}
	}

	// Pass-through fields
	result.PreviousResponseID = req.PreviousResponseID
	result.Background = req.Background
	result.Prompt = req.Prompt
	if result.PromptCacheKey == nil {
		result.PromptCacheKey = req.ResponsesPromptCacheKey
	}
	if result.PromptCacheRetention == nil {
		result.PromptCacheRetention = req.PromptCacheRetention
	}
	result.SafetyIdentifier = req.SafetyIdentifier
	result.MaxToolCalls = req.MaxToolCalls
	result.Conversation = req.Conversation
	result.ContextManagement = req.ContextManagement
	result.StreamOptions = req.ResponsesStreamOptions
	result.Include = req.Include
	result.TopLogprobs = req.TopLogprobs

	return result
}

func buildResponsesInput(req *model.InternalLLMRequest) ResponsesInput {
	if req == nil {
		return ResponsesInput{}
	}
	openaiExt := req.GetOpenAIExtensions()
	if len(openaiExt.RawResponseItems) > 0 {
		return ResponsesInput{Raw: sanitizeResponsesRawItems(append(json.RawMessage(nil), openaiExt.RawResponseItems...))}
	}
	return sanitizeResponsesInput(convertInputFromMessages(req.Messages, req.TransformOptions))
}

func MarshalResponsesInputItems(msgs []model.Message) (json.RawMessage, error) {
	forceArray := true
	input := convertInputFromMessages(msgs, model.TransformOptions{ArrayInputs: &forceArray})
	if len(input.Items) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(input.Items)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func convertInstructionsFromMessages(msgs []model.Message) string {
	var instructions []string
	for _, msg := range msgs {
		if msg.Role != "system" && msg.Role != "developer" {
			continue
		}
		if msg.Content.Content != nil {
			instructions = append(instructions, *msg.Content.Content)
		}
		if len(msg.Content.MultipleContent) > 0 {
			var sb strings.Builder
			for _, p := range msg.Content.MultipleContent {
				if p.Type == "text" && p.Text != nil {
					if sb.Len() > 0 {
						sb.WriteString("\n")
					}
					sb.WriteString(*p.Text)
				}
			}
			if sb.Len() > 0 {
				instructions = append(instructions, sb.String())
			}
		}
	}
	return strings.Join(instructions, "\n")
}

func convertInputFromMessages(msgs []model.Message, transformOptions model.TransformOptions) ResponsesInput {
	if len(msgs) == 0 {
		return ResponsesInput{}
	}

	wasArrayFormat := transformOptions.ArrayInputs != nil && *transformOptions.ArrayInputs

	// Check for simple single user message
	nonSystemMsgs := make([]model.Message, 0)
	for _, msg := range msgs {
		if msg.Role != "system" && msg.Role != "developer" {
			nonSystemMsgs = append(nonSystemMsgs, msg)
		}
	}

	if !wasArrayFormat && len(nonSystemMsgs) == 1 && nonSystemMsgs[0].Content.Content != nil && nonSystemMsgs[0].Role == "user" {
		return ResponsesInput{Text: nonSystemMsgs[0].Content.Content}
	}

	var items []ResponsesItem
	for _, msg := range msgs {
		switch msg.Role {
		case "system", "developer":
			continue
		case "user":
			items = append(items, convertUserMessageToResponses(msg))
		case "assistant":
			items = append(items, convertAssistantMessageToResponses(msg)...)
		case "tool":
			items = append(items, convertToolMessageToResponses(msg))
		}
	}

	return ResponsesInput{Items: sanitizeResponsesItems(items)}
}

func convertUserMessageToResponses(msg model.Message) ResponsesItem {
	var contentItems []ResponsesItem

	if msg.Content.Content != nil {
		contentItems = append(contentItems, ResponsesItem{
			Type: "input_text",
			Text: msg.Content.Content,
		})
	} else {
		for _, p := range msg.Content.MultipleContent {
			switch p.Type {
			case "text":
				if p.Text != nil {
					contentItems = append(contentItems, ResponsesItem{
						Type: "input_text",
						Text: p.Text,
					})
				}
			case "image_url":
				if p.ImageURL != nil {
					contentItems = append(contentItems, ResponsesItem{
						Type:     "input_image",
						ImageURL: &p.ImageURL.URL,
						Detail:   p.ImageURL.Detail,
					})
				}
			case "file":
				// O-H6: reproduce whichever file representation the
				// caller used originally.
				if p.File == nil {
					continue
				}
				item := ResponsesItem{Type: "input_file"}
				if p.File.FileID != "" {
					item.FileID = lo.ToPtr(p.File.FileID)
				}
				if p.File.FileURL != "" {
					item.FileURL = lo.ToPtr(p.File.FileURL)
				}
				if p.File.Filename != "" {
					item.Filename = lo.ToPtr(p.File.Filename)
				}
				if p.File.FileData != "" {
					item.FileData = lo.ToPtr(p.File.FileData)
				}
				if item.FileID == nil && item.FileURL == nil && item.FileData == nil {
					continue
				}
				contentItems = append(contentItems, item)
			case "input_audio":
				// O-H6: audio on Responses rides in a nested object
				// rather than a flat field.
				if p.Audio == nil {
					continue
				}
				contentItems = append(contentItems, ResponsesItem{
					Type: "input_audio",
					InputAudio: &ResponsesInputAudio{
						Data:   p.Audio.Data,
						Format: p.Audio.Format,
					},
				})
			}
		}
	}

	return ResponsesItem{
		Role:    msg.Role,
		Content: &ResponsesInput{Items: contentItems},
	}
}

func convertAssistantMessageToResponses(msg model.Message) []ResponsesItem {
	var items []ResponsesItem

	// Handle reasoning content
	if msg.ReasoningContent != nil && *msg.ReasoningContent != "" {
		reasoningItem := ResponsesItem{
			Type: "reasoning",
			Summary: []ResponsesReasoningSummary{{
				Type: "summary_text",
				Text: *msg.ReasoningContent,
			}},
		}
		if msg.ReasoningSignature != nil && *msg.ReasoningSignature != "" {
			reasoningItem.EncryptedContent = msg.ReasoningSignature
		}
		items = append(items, reasoningItem)
	}

	// Handle tool calls
	for _, tc := range msg.ToolCalls {
		items = append(items, ResponsesItem{
			Type:      "function_call",
			CallID:    tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}

	// Handle content
	var contentItems []ResponsesItem
	if msg.Content.Content != nil {
		contentItems = append(contentItems, ResponsesItem{
			Type: "output_text",
			Text: msg.Content.Content,
		})
	} else {
		for _, p := range msg.Content.MultipleContent {
			if p.Type == "text" && p.Text != nil {
				contentItems = append(contentItems, ResponsesItem{
					Type: "output_text",
					Text: p.Text,
				})
			}
		}
	}

	if len(contentItems) > 0 {
		items = append(items, ResponsesItem{
			Type:    "message",
			Role:    msg.Role,
			Status:  lo.ToPtr("completed"),
			Content: &ResponsesInput{Items: contentItems},
		})
	}

	return sanitizeResponsesItems(items)
}

func convertToolMessageToResponses(msg model.Message) ResponsesItem {
	var output ResponsesInput

	if msg.Content.Content != nil {
		output.Text = msg.Content.Content
	} else if len(msg.Content.MultipleContent) > 0 {
		for _, p := range msg.Content.MultipleContent {
			if p.Type == "text" && p.Text != nil {
				output.Items = append(output.Items, ResponsesItem{
					Type: "input_text",
					Text: p.Text,
				})
			}
		}
	}

	if output.Text == nil && len(output.Items) == 0 {
		output.Text = lo.ToPtr("")
	}

	return ResponsesItem{
		Type:   "function_call_output",
		CallID: lo.FromPtr(msg.ToolCallID),
		Output: &output,
	}
}

func convertToolsToResponses(tools []model.Tool) []ResponsesTool {
	result := make([]ResponsesTool, 0, len(tools))
	for _, tool := range tools {
		switch tool.Type {
		case "function":
			rt := ResponsesTool{
				Type:        "function",
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Strict:      tool.Function.Strict,
			}
			if len(tool.Function.Parameters) > 0 {
				var params map[string]any
				if err := json.Unmarshal(tool.Function.Parameters, &params); err == nil {
					rt.Parameters = params
				}
			}
			result = append(result, rt)
		case "image_generation":
			rt := ResponsesTool{
				Type: "image_generation",
			}
			if tool.ImageGeneration != nil {
				rt.Background = tool.ImageGeneration.Background
				rt.OutputFormat = tool.ImageGeneration.OutputFormat
				rt.Quality = tool.ImageGeneration.Quality
				rt.Size = tool.ImageGeneration.Size
				rt.OutputCompression = tool.ImageGeneration.OutputCompression
			}
			result = append(result, rt)
		}
	}
	return result
}

func convertToolChoiceToResponses(tc *model.ToolChoice) *ResponsesToolChoice {
	if tc == nil {
		return nil
	}

	result := &ResponsesToolChoice{}
	if tc.ToolChoice != nil {
		result.Mode = tc.ToolChoice
	} else if tc.NamedToolChoice != nil {
		result.Type = &tc.NamedToolChoice.Type
		if name := tc.NamedToolChoice.ResolvedFunctionName(); name != "" {
			n := name
			result.Name = &n
		}
	}
	return result
}

func convertToLLMResponseFromResponses(resp *ResponsesResponse) *model.InternalLLMResponse {
	if resp == nil {
		return &model.InternalLLMResponse{
			Object: "chat.completion",
		}
	}

	result := &model.InternalLLMResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Model:   resp.Model,
		Created: resp.CreatedAt,
	}
	if len(resp.Output) > 0 {
		if rawOutput, err := json.Marshal(sanitizeResponsesItems(resp.Output)); err == nil {
			result.RawResponsesOutputItems = rawOutput
		}
	}

	var (
		contentParts     []model.MessageContentPart
		textContent      strings.Builder
		refusalContent   strings.Builder
		reasoningContent strings.Builder
		toolCalls        []model.ToolCall
	)

	for _, outputItem := range resp.Output {
		switch outputItem.Type {
		case "message":
			if outputItem.Content != nil {
				for _, item := range outputItem.Content.Items {
					switch item.Type {
					case "output_text":
						if item.Text != nil {
							textContent.WriteString(*item.Text)
						}
					case "refusal":
						if item.Refusal != nil {
							refusalContent.WriteString(*item.Refusal)
						} else if item.Text != nil {
							refusalContent.WriteString(*item.Text)
						}
					}
				}
			}
		case "output_text":
			if outputItem.Text != nil {
				textContent.WriteString(*outputItem.Text)
			}
		case "function_call":
			toolCalls = append(toolCalls, model.ToolCall{
				ID:   outputItem.CallID,
				Type: "function",
				Function: model.FunctionCall{
					Name:      outputItem.Name,
					Arguments: outputItem.Arguments,
				},
			})
		case "reasoning":
			for _, summary := range outputItem.Summary {
				reasoningContent.WriteString(summary.Text)
			}
		case "image_generation_call":
			if outputItem.Result != nil && *outputItem.Result != "" {
				outputFormat := "png"
				if outputItem.OutputFormat != nil {
					outputFormat = *outputItem.OutputFormat
				}
				contentParts = append(contentParts, model.MessageContentPart{
					Type: "image_url",
					ImageURL: &model.ImageURL{
						URL: "data:image/" + outputFormat + ";base64," + *outputItem.Result,
					},
				})
			}
		}
	}

	choice := model.Choice{
		Index: 0,
		Message: &model.Message{
			Role:      "assistant",
			ToolCalls: toolCalls,
		},
	}

	// Set reasoning content if present
	if reasoningContent.Len() > 0 {
		choice.Message.ReasoningContent = lo.ToPtr(reasoningContent.String())
	}
	if refusalContent.Len() > 0 {
		choice.Message.Refusal = refusalContent.String()
	}

	// Set message content
	if textContent.Len() > 0 {
		if len(contentParts) > 0 {
			textPart := model.MessageContentPart{
				Type: "text",
				Text: lo.ToPtr(textContent.String()),
			}
			contentParts = append([]model.MessageContentPart{textPart}, contentParts...)
			choice.Message.Content = model.MessageContent{
				MultipleContent: contentParts,
			}
		} else {
			choice.Message.Content = model.MessageContent{
				Content: lo.ToPtr(textContent.String()),
			}
		}
	} else if len(contentParts) > 0 {
		choice.Message.Content = model.MessageContent{
			MultipleContent: contentParts,
		}
	}

	// Set finish reason based on status
	if len(toolCalls) > 0 {
		choice.FinishReason = lo.ToPtr("tool_calls")
	} else {
		finishReason, respErr := normalizeResponsesFinishReason(resp.Status, resp.Error)
		choice.FinishReason = finishReason
		if respErr != nil {
			result.Error = respErr
		}
	}

	result.Choices = []model.Choice{choice}
	result.Usage = convertResponsesUsage(resp.Usage)

	return result
}

func (o *ResponseOutbound) ensureOutputItem(outputIndex int, itemType string) ResponsesItem {
	if o.outputItems == nil {
		o.outputItems = make(map[int]ResponsesItem)
	}
	item := o.outputItems[outputIndex]
	if item.Type == "" && itemType != "" {
		item.Type = itemType
	}
	if item.Type == "message" && item.Content == nil {
		item.Content = &ResponsesInput{}
	}
	if item.Type == "reasoning" {
		ensureResponsesReasoningSummary(&item)
	}
	o.outputItems[outputIndex] = item
	return item
}

func cloneResponsesInput(input *ResponsesInput) *ResponsesInput {
	if input == nil {
		return nil
	}
	cloned := *input
	cloned.Items = append([]ResponsesItem(nil), input.Items...)
	if len(input.Raw) > 0 {
		cloned.Raw = append(json.RawMessage(nil), input.Raw...)
	}
	return &cloned
}

func cloneResponsesItem(item ResponsesItem) ResponsesItem {
	cloned := item
	cloned.Content = cloneResponsesInput(item.Content)
	cloned.Output = cloneResponsesInput(item.Output)
	cloned.Summary = append([]ResponsesReasoningSummary(nil), item.Summary...)
	return cloned
}

func mergeResponsesInputPreservingText(dst, src *ResponsesInput) *ResponsesInput {
	if dst == nil && src == nil {
		return nil
	}
	if dst == nil {
		return cloneResponsesInput(src)
	}
	if src == nil {
		return dst
	}
	if dst.Text == nil && src.Text != nil {
		text := *src.Text
		dst.Text = &text
	}
	if len(dst.Items) == 0 && len(src.Items) > 0 {
		dst.Items = append([]ResponsesItem(nil), src.Items...)
	} else {
		for i := range src.Items {
			if i >= len(dst.Items) {
				dst.Items = append(dst.Items, src.Items[i:]...)
				break
			}
			if dst.Items[i].Type == "" {
				dst.Items[i].Type = src.Items[i].Type
			}
			if dst.Items[i].Text == nil && src.Items[i].Text != nil {
				text := *src.Items[i].Text
				dst.Items[i].Text = &text
			}
		}
	}
	if len(dst.Raw) == 0 && len(src.Raw) > 0 {
		dst.Raw = append(json.RawMessage(nil), src.Raw...)
	}
	return dst
}

func (o *ResponseOutbound) mergeOutputItemAdded(event ResponsesStreamEvent) {
	if o == nil || event.Item == nil {
		return
	}
	if o.outputItems == nil {
		o.outputItems = make(map[int]ResponsesItem)
	}
	cloned := cloneResponsesItem(*event.Item)
	if existing, ok := o.outputItems[event.OutputIndex]; ok {
		if cloned.Type == "" {
			cloned.Type = existing.Type
		}
		cloned.Content = mergeResponsesInputPreservingText(cloned.Content, existing.Content)
		cloned.Output = mergeResponsesInputPreservingText(cloned.Output, existing.Output)
		if len(cloned.Summary) == 0 && len(existing.Summary) > 0 {
			cloned.Summary = append([]ResponsesReasoningSummary(nil), existing.Summary...)
		}
		cloned.CallID = firstNonEmpty(cloned.CallID, existing.CallID)
		cloned.Name = firstNonEmpty(cloned.Name, existing.Name)
		if cloned.Arguments == "" {
			cloned.Arguments = existing.Arguments
		} else if existing.Arguments != "" && !strings.Contains(cloned.Arguments, existing.Arguments) {
			cloned.Arguments = existing.Arguments + cloned.Arguments
		}
	}
	o.outputItems[event.OutputIndex] = cloned
}

func (o *ResponseOutbound) mergeOutputTextDelta(event ResponsesStreamEvent) {
	if o == nil {
		return
	}
	item := o.ensureOutputItem(event.OutputIndex, "message")
	if item.Type != "message" {
		return
	}
	if item.Content == nil {
		item.Content = &ResponsesInput{}
	}
	contentIndex := 0
	if event.ContentIndex != nil && *event.ContentIndex >= 0 {
		contentIndex = *event.ContentIndex
	}
	for len(item.Content.Items) <= contentIndex {
		item.Content.Items = append(item.Content.Items, ResponsesItem{})
	}
	if item.Content.Items[contentIndex].Type == "" {
		item.Content.Items[contentIndex].Type = "output_text"
	}
	if item.Content.Items[contentIndex].Text == nil {
		item.Content.Items[contentIndex].Text = lo.ToPtr("")
	}
	*item.Content.Items[contentIndex].Text += event.Delta
	o.outputItems[event.OutputIndex] = item
}

func (o *ResponseOutbound) mergeFunctionCallDelta(event ResponsesStreamEvent) {
	if o == nil {
		return
	}
	item := o.ensureOutputItem(event.OutputIndex, "function_call")
	if item.Type != "function_call" {
		return
	}
	item.CallID = firstNonEmpty(item.CallID, event.CallID)
	item.Name = firstNonEmpty(item.Name, event.Name)
	item.Arguments += event.Delta
	o.outputItems[event.OutputIndex] = item
}

func (o *ResponseOutbound) mergeReasoningDelta(event ResponsesStreamEvent) {
	if o == nil {
		return
	}
	item := o.ensureOutputItem(event.OutputIndex, "reasoning")
	if item.Type != "reasoning" {
		return
	}
	summaryIndex := 0
	if event.SummaryIndex != nil && *event.SummaryIndex >= 0 {
		summaryIndex = *event.SummaryIndex
	}
	for len(item.Summary) <= summaryIndex {
		item.Summary = append(item.Summary, ResponsesReasoningSummary{})
	}
	if item.Summary[summaryIndex].Type == "" {
		item.Summary[summaryIndex].Type = "summary_text"
	}
	item.Summary[summaryIndex].Text += event.Delta
	o.outputItems[event.OutputIndex] = item
}

func (o *ResponseOutbound) marshalTrackedOutputItems() (json.RawMessage, bool) {
	if o == nil || len(o.outputItems) == 0 {
		return nil, false
	}
	maxIdx := -1
	for idx := range o.outputItems {
		if idx > maxIdx {
			maxIdx = idx
		}
	}
	items := make([]ResponsesItem, 0, len(o.outputItems))
	for idx := 0; idx <= maxIdx; idx++ {
		item, ok := o.outputItems[idx]
		if !ok {
			continue
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, false
	}
	data, err := json.Marshal(sanitizeResponsesItems(items))
	if err != nil {
		return nil, false
	}
	return data, true
}

func sanitizeResponsesInput(input ResponsesInput) ResponsesInput {
	if len(input.Raw) > 0 {
		input.Raw = sanitizeResponsesRawItems(input.Raw)
	}
	if len(input.Items) > 0 {
		input.Items = sanitizeResponsesItems(input.Items)
	}
	return input
}

func sanitizeResponsesItems(items []ResponsesItem) []ResponsesItem {
	if len(items) == 0 {
		return items
	}

	sanitized := make([]ResponsesItem, len(items))
	for i, item := range items {
		sanitized[i] = item
		ensureResponsesReasoningSummary(&sanitized[i])
		ensureResponsesRefusalShape(&sanitized[i])
	}
	return sanitized
}

func ensureResponsesRefusalShape(item *ResponsesItem) {
	if item == nil || item.Content == nil {
		return
	}
	for i := range item.Content.Items {
		contentItem := &item.Content.Items[i]
		if contentItem.Type != "refusal" || contentItem.Refusal != nil || contentItem.Text == nil {
			continue
		}
		contentItem.Refusal = contentItem.Text
		contentItem.Text = nil
	}
}

func ensureResponsesReasoningSummary(item *ResponsesItem) {
	if item == nil || item.Type != "reasoning" {
		return
	}
	if len(item.Summary) == 0 {
		item.Summary = []ResponsesReasoningSummary{{
			Type: "summary_text",
			Text: "",
		}}
		return
	}
	for i := range item.Summary {
		if item.Summary[i].Type == "" {
			item.Summary[i].Type = "summary_text"
		}
		if item.Summary[i].Text == "" {
			item.Summary[i].Text = ""
		}
	}
}

func sanitizeResponsesRawItems(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}

	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return raw
	}

	changed := false
	for _, item := range items {
		if decodeRawString(item["type"]) != "reasoning" {
			continue
		}

		summaryRaw, ok := item["summary"]
		if !ok || len(bytes.TrimSpace(summaryRaw)) == 0 || bytes.Equal(bytes.TrimSpace(summaryRaw), []byte("null")) {
			item["summary"] = defaultRawResponsesReasoningSummary()
			changed = true
			continue
		}

		sanitizedSummary, summaryChanged, ok := sanitizeResponsesRawSummary(summaryRaw)
		if ok && summaryChanged {
			item["summary"] = sanitizedSummary
			changed = true
		}
	}

	if !changed {
		return raw
	}

	data, err := json.Marshal(items)
	if err != nil {
		return raw
	}
	return data
}

func sanitizeResponsesRawSummary(raw json.RawMessage) (json.RawMessage, bool, bool) {
	var summaryItems []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &summaryItems); err != nil {
		return nil, false, false
	}
	if len(summaryItems) == 0 {
		return defaultRawResponsesReasoningSummary(), true, true
	}

	changed := false
	for _, summary := range summaryItems {
		typeRaw, hasType := summary["type"]
		if !hasType || len(bytes.TrimSpace(typeRaw)) == 0 || bytes.Equal(bytes.TrimSpace(typeRaw), []byte("null")) {
			summary["type"] = []byte(`"summary_text"`)
			changed = true
		}
		textRaw, hasText := summary["text"]
		if !hasText || len(bytes.TrimSpace(textRaw)) == 0 || bytes.Equal(bytes.TrimSpace(textRaw), []byte("null")) {
			summary["text"] = []byte(`""`)
			changed = true
		}
	}

	if !changed {
		return raw, false, true
	}

	data, err := json.Marshal(summaryItems)
	if err != nil {
		return nil, false, false
	}
	return data, true, true
}

func defaultRawResponsesReasoningSummary() json.RawMessage {
	return json.RawMessage(`[{"type":"summary_text","text":""}]`)
}

func decodeRawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// normalizeResponsesFinishReason maps an OpenAI Responses API `status` value
// to a legal OpenAI Chat Completions finish_reason. The Chat schema enum is
// stop | length | tool_calls | content_filter | function_call; emitting
// anything else (the historical "error" value we used) is rejected outright
// by strict downstream SDKs such as OpenAI Python / Pydantic AI.
//
// When the upstream carries a ResponsesError on failure / incomplete turns
// we synthesise a ResponseError so the inbound layer can surface the
// original cause to the client via the final chunk payload. Tool-call
// driven completions are handled by the caller before this helper runs;
// content_filter / nuanced mappings (incomplete_details.reason) will land
// with O-M1.
func normalizeResponsesFinishReason(status *string, errDetail *ResponsesError) (*string, *model.ResponseError) {
	var respErr *model.ResponseError
	if errDetail != nil && (errDetail.Message != "" || errDetail.Code != 0) {
		respErr = &model.ResponseError{
			Detail: model.ErrorDetail{
				Code:    fmt.Sprintf("%d", errDetail.Code),
				Message: errDetail.Message,
			},
		}
	}

	if status == nil {
		return nil, respErr
	}
	switch *status {
	case "completed":
		return lo.ToPtr("stop"), nil
	case "incomplete":
		return lo.ToPtr("length"), respErr
	case "failed":
		return lo.ToPtr("stop"), respErr
	default:
		return nil, respErr
	}
}

// responseCarriesFunctionCall reports whether a terminal Responses event
// contains at least one function_call output item. Used by the streaming
// response.completed branch to override a "stop" finish_reason to
// "tool_calls" when the upstream chose to invoke a client-defined tool.
//
// The lookup prefers the fully-populated `response.output` Array attached
// to the completed event; when the upstream omits it (some OpenAI-compat
// upstreams do), we fall back to the items we have been tracking during
// the stream via mergeOutputItemAdded / mergeFunctionCallDelta.
func (o *ResponseOutbound) responseCarriesFunctionCall(resp *ResponsesResponse) bool {
	if resp != nil {
		for _, item := range resp.Output {
			if item.Type == "function_call" {
				return true
			}
		}
		if len(resp.Output) > 0 {
			return false
		}
	}
	if o == nil || len(o.outputItems) == 0 {
		return false
	}
	for _, item := range o.outputItems {
		if item.Type == "function_call" {
			return true
		}
	}
	return false
}

func convertResponsesUsage(usage *ResponsesUsage) *model.Usage {
	if usage == nil {
		return nil
	}

	result := &model.Usage{
		PromptTokens:     usage.InputTokens,
		CompletionTokens: usage.OutputTokens,
		TotalTokens:      usage.TotalTokens,
	}

	if usage.InputTokenDetails.CachedTokens > 0 {
		result.PromptTokensDetails = &model.PromptTokensDetails{
			CachedTokens: usage.InputTokenDetails.CachedTokens,
		}
	}

	if usage.OutputTokenDetails.ReasoningTokens > 0 {
		result.CompletionTokensDetails = &model.CompletionTokensDetails{
			ReasoningTokens: usage.OutputTokenDetails.ReasoningTokens,
		}
	}

	return result
}
