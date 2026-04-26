package model

import (
	"encoding/json"
	"net/url"
)

type RequestCoreView struct {
	Model          string
	Messages       []Message
	Tools          []Tool
	ToolChoice     *ToolChoice
	ResponseFormat *ResponseFormat
	Stop           *Stop
	Stream         *bool
	StreamOptions  *StreamOptions
}

type RequestEmbeddingView struct {
	Input          *EmbeddingInput
	Dimensions     *int64
	EncodingFormat *string
}

type RequestSamplingView struct {
	FrequencyPenalty    *float64
	Logprobs            *bool
	MaxCompletionTokens *int64
	MaxTokens           *int64
	PresencePenalty     *float64
	Seed                *int64
	Temperature         *float64
	TopLogprobs         *int64
	TopP                *float64
	TopK                *int64
	ParallelToolCalls   *bool
}

type RequestCapabilityView struct {
	Modalities []string
	Audio      *struct {
		Format string `json:"format,omitempty"`
		Voice  string `json:"voice,omitempty"`
	}
	ReasoningEffort  string
	ReasoningBudget  *int64
	AdaptiveThinking bool
	ThinkingDisplay  string
	EnableThinking   *bool
	Verbosity        *string
	Prediction       json.RawMessage
	WebSearchOptions json.RawMessage
	Include          []string
	ServiceTier      *string
	Truncation       *string
	Store            *bool
	PromptCacheKey   *string
	SafetyIdentifier *string
	User             *string
	LogitBias        map[string]int64
	Metadata         map[string]string
}

type RequestProviderView struct {
	RawRequest          []byte
	RawAPIFormat        APIFormat
	TransformerMetadata map[string]string
	TransformOptions    TransformOptions
	ExtraBody           json.RawMessage
	Query               url.Values
	Extensions          *ProviderExtensions
	OpenAIResponses     OpenAIResponsesRequestView
	Gemini              GeminiRequestView
	Anthropic           AnthropicRequestView
}

type OpenAIResponsesRequestView struct {
	PreviousResponseID       *string
	Background               *bool
	Prompt                   json.RawMessage
	PromptCacheKey           *string
	PromptCacheRetention     *string
	MaxToolCalls             *int64
	Conversation             json.RawMessage
	ContextManagement        json.RawMessage
	StreamOptions            json.RawMessage
	ReasoningSummary         *string
	ReasoningGenerateSummary *string
	RawInputItems            json.RawMessage
}

type GeminiRequestView struct {
	CachedContentRef *string
	SpeechConfig     json.RawMessage
}

type AnthropicRequestView struct {
	MCPServers json.RawMessage
	Container  json.RawMessage
}

type RequestIRView struct {
	Core         RequestCoreView
	Embedding    RequestEmbeddingView
	Sampling     RequestSamplingView
	Capabilities RequestCapabilityView
	Provider     RequestProviderView
}

func (r *InternalLLMRequest) CoreView() RequestCoreView {
	if r == nil {
		return RequestCoreView{}
	}
	return RequestCoreView{
		Model:          r.Model,
		Messages:       r.Messages,
		Tools:          r.Tools,
		ToolChoice:     r.ToolChoice,
		ResponseFormat: r.ResponseFormat,
		Stop:           r.Stop,
		Stream:         r.Stream,
		StreamOptions:  r.StreamOptions,
	}
}

func (r *InternalLLMRequest) EmbeddingView() RequestEmbeddingView {
	if r == nil {
		return RequestEmbeddingView{}
	}
	return RequestEmbeddingView{
		Input:          r.EmbeddingInput,
		Dimensions:     r.EmbeddingDimensions,
		EncodingFormat: r.EmbeddingEncodingFormat,
	}
}

func (r *InternalLLMRequest) SamplingView() RequestSamplingView {
	if r == nil {
		return RequestSamplingView{}
	}
	return RequestSamplingView{
		FrequencyPenalty:    r.FrequencyPenalty,
		Logprobs:            r.Logprobs,
		MaxCompletionTokens: r.MaxCompletionTokens,
		MaxTokens:           r.MaxTokens,
		PresencePenalty:     r.PresencePenalty,
		Seed:                r.Seed,
		Temperature:         r.Temperature,
		TopLogprobs:         r.TopLogprobs,
		TopP:                r.TopP,
		TopK:                r.TopK,
		ParallelToolCalls:   r.ParallelToolCalls,
	}
}

func (r *InternalLLMRequest) CapabilityView() RequestCapabilityView {
	if r == nil {
		return RequestCapabilityView{}
	}
	return RequestCapabilityView{
		Modalities:       r.Modalities,
		Audio:            r.Audio,
		ReasoningEffort:  r.ReasoningEffort,
		ReasoningBudget:  r.ReasoningBudget,
		AdaptiveThinking: r.AdaptiveThinking,
		ThinkingDisplay:  r.ThinkingDisplay,
		EnableThinking:   r.EnableThinking,
		Verbosity:        r.Verbosity,
		Prediction:       r.Prediction,
		WebSearchOptions: r.WebSearchOptions,
		Include:          r.Include,
		ServiceTier:      r.ServiceTier,
		Truncation:       r.Truncation,
		Store:            r.Store,
		PromptCacheKey:   r.PromptCacheKey,
		SafetyIdentifier: r.SafetyIdentifier,
		User:             r.User,
		LogitBias:        r.LogitBias,
		Metadata:         r.Metadata,
	}
}

func (r *InternalLLMRequest) ProviderView() RequestProviderView {
	if r == nil {
		return RequestProviderView{}
	}
	return RequestProviderView{
		RawRequest:          r.RawRequest,
		RawAPIFormat:        r.RawAPIFormat,
		TransformerMetadata: r.TransformerMetadata,
		TransformOptions:    r.TransformOptions,
		ExtraBody:           r.ExtraBody,
		Query:               r.Query,
		Extensions:          r.ProviderExtensions,
		OpenAIResponses: OpenAIResponsesRequestView{
			PreviousResponseID:       r.PreviousResponseID,
			Background:               r.Background,
			Prompt:                   r.Prompt,
			PromptCacheKey:           r.ResponsesPromptCacheKey,
			PromptCacheRetention:     r.PromptCacheRetention,
			MaxToolCalls:             r.MaxToolCalls,
			Conversation:             r.Conversation,
			ContextManagement:        r.ContextManagement,
			StreamOptions:            r.ResponsesStreamOptions,
			ReasoningSummary:         r.ReasoningSummary,
			ReasoningGenerateSummary: r.ReasoningGenerateSummary,
			RawInputItems:            r.RawInputItems,
		},
		Gemini: GeminiRequestView{
			CachedContentRef: r.GeminiCachedContentRef,
			SpeechConfig:     r.GeminiSpeechConfig,
		},
		Anthropic: AnthropicRequestView{
			MCPServers: r.AnthropicMCPServers,
			Container:  r.AnthropicContainer,
		},
	}
}

func (r *InternalLLMRequest) IRView() RequestIRView {
	if r == nil {
		return RequestIRView{}
	}
	return RequestIRView{
		Core:         r.CoreView(),
		Embedding:    r.EmbeddingView(),
		Sampling:     r.SamplingView(),
		Capabilities: r.CapabilityView(),
		Provider:     r.ProviderView(),
	}
}
