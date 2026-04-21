package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

type ChatOutbound struct{}

// ChatCompletionsRequest is the explicit OpenAI chat/completions wire payload.
// Keeping this as a whitelist prevents internal/provider-specific fields on the
// shared InternalLLMRequest from leaking to OpenAI-compatible upstreams.
type ChatCompletionsRequest struct {
	Messages []model.Message `json:"messages"`
	Model    string          `json:"model"`

	FrequencyPenalty    *float64              `json:"frequency_penalty,omitempty"`
	Logprobs            *bool                 `json:"logprobs,omitempty"`
	MaxCompletionTokens *int64                `json:"max_completion_tokens,omitempty"`
	MaxTokens           *int64                `json:"max_tokens,omitempty"`
	PresencePenalty     *float64              `json:"presence_penalty,omitempty"`
	Seed                *int64                `json:"seed,omitempty"`
	Store               *bool                 `json:"store,omitempty"`
	Temperature         *float64              `json:"temperature,omitempty"`
	TopLogprobs         *int64                `json:"top_logprobs,omitempty"`
	TopP                *float64              `json:"top_p,omitempty"`
	LogitBias           map[string]int64      `json:"logit_bias,omitempty"`
	Metadata            map[string]string     `json:"metadata,omitempty"`
	Modalities          []string              `json:"modalities,omitempty"`
	Audio               *ChatCompletionsAudio `json:"audio,omitempty"`
	ReasoningEffort     string                `json:"reasoning_effort,omitempty"`
	ServiceTier         *string               `json:"service_tier,omitempty"`
	Stop                *model.Stop           `json:"stop,omitempty"`
	Stream              *bool                 `json:"stream,omitempty"`
	StreamOptions       *model.StreamOptions  `json:"stream_options,omitempty"`
	ParallelToolCalls   *bool                 `json:"parallel_tool_calls,omitempty"`
	Tools               []model.Tool          `json:"tools,omitempty"`
	ToolChoice          *model.ToolChoice     `json:"tool_choice,omitempty"`
	ResponseFormat      *model.ResponseFormat `json:"response_format,omitempty"`
	SafetyIdentifier    *string               `json:"safety_identifier,omitempty"`
	// PromptCacheKey mirrors the top-level model field. Only forwarded when
	// the client populated the field on the Chat entrypoint — Responses
	// inbound carries its own ResponsesPromptCacheKey pass-through that
	// stays isolated from this builder.
	PromptCacheKey *string `json:"prompt_cache_key,omitempty"`
	// User is OpenAI's legacy caller-supplied end-user identifier. OpenAI now
	// prefers `safety_identifier` + `prompt_cache_key`, but the field is still
	// accepted for backward compatibility; we forward it when the client sets
	// it so downstreams that key on `user` keep working.
	User *string `json:"user,omitempty"`
	// Verbosity is the gpt-5 detail-level knob ("low" | "medium" | "high").
	Verbosity *string `json:"verbosity,omitempty"`
	// Prediction forwards the OpenAI "predicted outputs" payload verbatim.
	Prediction json.RawMessage `json:"prediction,omitempty"`
	// WebSearchOptions configures the Chat Completions built-in web search
	// tool; kept as raw JSON for schema stability.
	WebSearchOptions json.RawMessage `json:"web_search_options,omitempty"`
}

type ChatCompletionsAudio struct {
	Format string `json:"format,omitempty"`
	Voice  string `json:"voice,omitempty"`
}

func (o *ChatOutbound) TransformRequest(ctx context.Context, request *model.InternalLLMRequest, baseUrl, key string) (*http.Request, error) {
	request.ClearHelpFields()
	request.NormalizeMessages()
	request.FlattenUnsupportedBlocks(model.AlternationProviderOpenAI)

	// developer role is preserved as-is on OpenAI outbound (O-L5). OpenAI
	// 2025+ model spec treats "developer" as the canonical instruction
	// role for reasoning models; the latest Chat Completions API accepts
	// it natively and silently maps "system" to "developer" on reasoning
	// models for backward compatibility. Previously we forced
	// developer → system which worked on gpt-4 / gpt-4o (where the two
	// are interchangeable) but lost the semantic distinction on gpt-5
	// reasoning models. Keep the original role so upstreams that depend
	// on it (and downstreams that replay it) see the caller's intent.
	// Ref: https://platform.openai.com/docs/api-reference/chat

	if request.Stream != nil && *request.Stream {
		if request.StreamOptions == nil {
			request.StreamOptions = &model.StreamOptions{IncludeUsage: true}
		} else if !request.StreamOptions.IncludeUsage {
			request.StreamOptions.IncludeUsage = true
		}
	}

	body, err := json.Marshal(buildChatCompletionsRequest(request))
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	applyOpenAIOrgProjectHeaders(req, request)

	parsedUrl, err := url.Parse(strings.TrimSuffix(baseUrl, "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}
	parsedUrl.Path = parsedUrl.Path + "/chat/completions"
	req.URL = parsedUrl
	req.Method = http.MethodPost
	return req, nil
}

// applyOpenAIOrgProjectHeaders forwards the optional OpenAI-Organization and
// OpenAI-Project headers from TransformerMetadata. Both headers are scoped
// to multi-org / multi-project deployments where a single API key can hit
// several billing scopes; callers set the metadata keys upstream (in relay
// configuration or per-request overrides) and the outbound transformer
// blindly forwards the values. Empty / whitespace-only values are dropped
// so we don't emit header keys with blank values. O-M7.
// Ref: https://platform.openai.com/docs/api-reference/debugging-requests
func applyOpenAIOrgProjectHeaders(req *http.Request, request *model.InternalLLMRequest) {
	if req == nil || request == nil || request.TransformerMetadata == nil {
		return
	}
	if org := strings.TrimSpace(request.TransformerMetadata["openai_organization"]); org != "" {
		req.Header.Set("OpenAI-Organization", org)
	}
	if project := strings.TrimSpace(request.TransformerMetadata["openai_project"]); project != "" {
		req.Header.Set("OpenAI-Project", project)
	}
}

func buildChatCompletionsRequest(request *model.InternalLLMRequest) *ChatCompletionsRequest {
	if request == nil {
		return &ChatCompletionsRequest{}
	}

	result := &ChatCompletionsRequest{
		Messages:            request.Messages,
		Model:               request.Model,
		FrequencyPenalty:    request.FrequencyPenalty,
		Logprobs:            request.Logprobs,
		MaxCompletionTokens: request.MaxCompletionTokens,
		MaxTokens:           request.MaxTokens,
		PresencePenalty:     request.PresencePenalty,
		Seed:                request.Seed,
		Store:               request.Store,
		Temperature:         request.Temperature,
		TopLogprobs:         request.TopLogprobs,
		TopP:                request.TopP,
		LogitBias:           request.LogitBias,
		Metadata:            request.Metadata,
		Modalities:          request.Modalities,
		ReasoningEffort:     request.ReasoningEffort,
		ServiceTier:         request.ServiceTier,
		Stop:                request.Stop,
		Stream:              request.Stream,
		StreamOptions:       request.StreamOptions,
		ParallelToolCalls:   request.ParallelToolCalls,
		Tools:               request.Tools,
		ToolChoice:          request.ToolChoice,
		ResponseFormat:      request.ResponseFormat,
		SafetyIdentifier:    request.SafetyIdentifier,
		PromptCacheKey:      request.PromptCacheKey,
		User:                request.User,
		Verbosity:           request.Verbosity,
		Prediction:          request.Prediction,
		WebSearchOptions:    request.WebSearchOptions,
	}

	if request.Audio != nil {
		result.Audio = &ChatCompletionsAudio{
			Format: request.Audio.Format,
			Voice:  request.Audio.Voice,
		}
	}

	return result
}

func (o *ChatOutbound) TransformResponse(ctx context.Context, response *http.Response) (*model.InternalLLMResponse, error) {
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if len(body) == 0 {
		return nil, fmt.Errorf("response body is empty")
	}

	var resp model.InternalLLMResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return &resp, nil
}

func (o *ChatOutbound) TransformStream(ctx context.Context, eventData []byte) (*model.InternalLLMResponse, error) {
	if bytes.HasPrefix(eventData, []byte("[DONE]")) {
		return &model.InternalLLMResponse{
			Object: "[DONE]",
		}, nil
	}

	var errCheck struct {
		Error *model.ErrorDetail `json:"error"`
	}
	if err := json.Unmarshal(eventData, &errCheck); err == nil && errCheck.Error != nil {
		return nil, &model.ResponseError{
			Detail: *errCheck.Error,
		}
	}

	var resp model.InternalLLMResponse
	if err := json.Unmarshal(eventData, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stream chunk: %w", err)
	}
	return &resp, nil
}
