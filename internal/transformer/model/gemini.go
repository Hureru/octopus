package model

import (
	"encoding/json"
	"strings"
)

// GeminiGenerateContentRequest represents a Gemini API request
// Shared by both inbound and outbound transformers.
type GeminiGenerateContentRequest struct {
	Contents          []*GeminiContent        `json:"contents"`
	SystemInstruction *GeminiContent          `json:"systemInstruction,omitempty"`
	Tools             []*GeminiTool           `json:"tools,omitempty"`
	ToolConfig        *GeminiToolConfig       `json:"toolConfig,omitempty"`
	GenerationConfig  *GeminiGenerationConfig `json:"generationConfig,omitempty"`
	SafetySettings    []*GeminiSafetySetting  `json:"safetySettings,omitempty"`

	// CachedContent references a Google-managed cached content resource
	// (e.g. "cachedContents/xxxxxxxx"). When set, the upstream reuses the
	// cached prefix tokens instead of re-reading the bytes, cutting latency
	// and per-request cost. G-H8.
	// Ref: https://ai.google.dev/gemini-api/docs/caching
	CachedContent string `json:"cachedContent,omitempty"`

	// Labels are arbitrary key/value tags carried through to Gemini for
	// billing attribution and analytics. Max 64 entries, keys up to 63
	// chars, values up to 63 chars. G-H8.
	// Ref: https://ai.google.dev/api/generate-content#GenerateContentRequest
	Labels map[string]string `json:"labels,omitempty"`
}

// GeminiToolConfig configures tool/function calling behavior.
// See Gemini "toolConfig.functionCallingConfig".
type GeminiToolConfig struct {
	FunctionCallingConfig *GeminiFunctionCallingConfig `json:"functionCallingConfig,omitempty"`
}

// GeminiFunctionCallingConfig controls function calling mode and allowed functions.
type GeminiFunctionCallingConfig struct {
	// Mode is typically one of: AUTO, ANY, NONE.
	Mode string `json:"mode,omitempty"`
	// AllowedFunctionNames restricts which functions can be called when mode is ANY.
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

// GeminiContent represents a message content in Gemini format.
// Role is "user" / "model" for turns inside `contents`; for
// `systemInstruction` Gemini requires the role to be absent, which
// `omitempty` handles automatically so long as callers leave the field
// blank there.
type GeminiContent struct {
	Role  string        `json:"role,omitempty"`
	Parts []*GeminiPart `json:"parts"`
}

// GeminiPart represents a part of content (text, function call, etc.)
type GeminiPart struct {
	Text             string                  `json:"text,omitempty"`
	InlineData       *GeminiBlob             `json:"inlineData,omitempty"`
	FunctionCall     *GeminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *GeminiFunctionResponse `json:"functionResponse,omitempty"`
	FileData         *GeminiFileData         `json:"fileData,omitempty"`
	VideoMetadata    *GeminiVideoMetadata    `json:"videoMetadata,omitempty"`

	// ExecutableCode carries a codeExecution tool invocation emitted by
	// Gemini when the sandboxed code_execution tool is enabled. Mirrors the
	// upstream {language, code} shape. G-H9.
	ExecutableCode *GeminiExecutableCode `json:"executableCode,omitempty"`

	// CodeExecutionResult carries the outcome of a prior executableCode
	// part. Mirrors the upstream {outcome, output} shape. G-H9.
	CodeExecutionResult *GeminiCodeExecutionResult `json:"codeExecutionResult,omitempty"`

	// Thought indicates if the part is thought from the model
	Thought bool `json:"thought,omitempty"`

	// ThoughtSignature is an opaque signature for the thought
	ThoughtSignature string `json:"thoughtSignature,omitempty"`
}

// GeminiExecutableCode mirrors the code_execution tool's code-emitting
// payload. Language is an enum ("LANGUAGE_UNSPECIFIED" / "PYTHON" at time
// of writing). Code is the literal source Gemini wants to run.
// Ref: https://ai.google.dev/api/caching#ExecutableCode
type GeminiExecutableCode struct {
	Language string `json:"language,omitempty"`
	Code     string `json:"code,omitempty"`
}

// GeminiCodeExecutionResult mirrors the code_execution tool's result
// payload. Outcome is an enum ("OUTCOME_UNSPECIFIED" / "OUTCOME_OK" /
// "OUTCOME_FAILED" / "OUTCOME_DEADLINE_EXCEEDED"). Output is the literal
// stdout / stderr text Gemini produced.
// Ref: https://ai.google.dev/api/caching#CodeExecutionResult
type GeminiCodeExecutionResult struct {
	Outcome string `json:"outcome,omitempty"`
	Output  string `json:"output,omitempty"`
}

// GeminiBlob represents inline binary data
type GeminiBlob struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // base64 encoded
}

// GeminiFileData represents a reference to a file
type GeminiFileData struct {
	MimeType string `json:"mimeType"`
	FileURI  string `json:"fileUri"`
}

// GeminiVideoMetadata contains video-specific metadata
type GeminiVideoMetadata struct {
	StartOffset string `json:"startOffset,omitempty"`
	EndOffset   string `json:"endOffset,omitempty"`
}

// GeminiFunctionCall represents a function call from the model
type GeminiFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args,omitempty"`
}

// GeminiFunctionResponse represents a function call result
type GeminiFunctionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

// GeminiTool represents a tool/function definition. Exactly one of the
// *Tool flavours should be set per entry — Gemini's API treats the fields
// as a discriminated union, and some combinations (e.g. googleSearch +
// functionDeclarations) are rejected at request time.
type GeminiTool struct {
	// FunctionDeclarations holds client-defined functions the model may
	// call via functionCall parts.
	FunctionDeclarations []*GeminiFunctionDeclaration `json:"functionDeclarations,omitempty"`

	// CodeExecution enables Gemini's sandboxed code_execution tool.
	CodeExecution *GeminiCodeExecution `json:"codeExecution,omitempty"`

	// GoogleSearch enables Gemini's web search tool (Gemini 2.5+). The
	// payload is an empty object per the API; we keep the nil-vs-empty
	// distinction via pointer.
	GoogleSearch *GeminiGoogleSearch `json:"googleSearch,omitempty"`

	// UrlContext enables Gemini's URL fetch tool, which lets the model
	// read public web pages by URL.
	UrlContext *GeminiUrlContext `json:"urlContext,omitempty"`
}

// GeminiGoogleSearch toggles Gemini's managed web-search tool. The wire
// payload is `{}`; the struct is empty on purpose.
type GeminiGoogleSearch struct{}

// GeminiUrlContext toggles Gemini's URL fetch tool. Empty payload like
// GoogleSearch.
type GeminiUrlContext struct{}

// GeminiFunctionDeclaration describes a function that can be called
type GeminiFunctionDeclaration struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// GeminiCodeExecution represents code execution capability
type GeminiCodeExecution struct{}

// GeminiGenerationConfig controls generation parameters
type GeminiGenerationConfig struct {
	Temperature        *float64      `json:"temperature,omitempty"`
	TopP               *float64      `json:"topP,omitempty"`
	TopK               *int          `json:"topK,omitempty"`
	CandidateCount     int           `json:"candidateCount,omitempty"`
	MaxOutputTokens    int           `json:"maxOutputTokens,omitempty"`
	StopSequences      []string      `json:"stopSequences,omitempty"`
	ResponseMimeType   string        `json:"responseMimeType,omitempty"`
	ResponseSchema     *GeminiSchema `json:"responseSchema,omitempty"`
	ResponseModalities []string      `json:"responseModalities,omitempty"`

	// PresencePenalty / FrequencyPenalty mirror the OpenAI knobs. Gemini
	// accepts them since 1.5 on a [-2.0, 2.0] range; left unset the upstream
	// default applies. G-H1.
	PresencePenalty  *float64 `json:"presencePenalty,omitempty"`
	FrequencyPenalty *float64 `json:"frequencyPenalty,omitempty"`

	// ResponseLogprobs toggles emission of per-token logprobs on candidates.
	// When nil the upstream default (disabled) applies. G-H1.
	ResponseLogprobs *bool `json:"responseLogprobs,omitempty"`

	// Logprobs sets how many of the top candidates to return logprobs for.
	// Gemini caps this at 5 per token; callers that exceed get clamped by
	// the outbound transformer. G-H1.
	Logprobs *int `json:"logprobs,omitempty"`

	// Seed pins the generation RNG for reproducible sampling. G-H1.
	Seed *int64 `json:"seed,omitempty"`

	// MediaResolution controls media-understanding fidelity
	// ("MEDIA_RESOLUTION_LOW|MEDIUM|HIGH"). Forwarded as a passthrough from
	// TransformerMetadata["gemini_media_resolution"]. G-H1.
	MediaResolution string `json:"mediaResolution,omitempty"`

	// SpeechConfig carries Gemini's audio-output configuration (voice
	// selection, language code, multi-speaker setup). The schema is
	// deeply nested and shared with the Live API, so we treat it as an
	// opaque passthrough — callers either supply the raw JSON via
	// InternalLLMRequest.GeminiSpeechConfig or synthesize it from the
	// generic request.Audio {format, voice} pair. G-H11.
	// Ref: https://ai.google.dev/api/generate-content#SpeechConfig
	SpeechConfig json.RawMessage `json:"speechConfig,omitempty"`

	// AudioTimestamp toggles per-part audio timestamp emission for
	// transcription-style workloads. Left nil to inherit the upstream
	// default. G-H11.
	AudioTimestamp *bool `json:"audioTimestamp,omitempty"`

	// ThinkingConfig is the thinking features configuration
	ThinkingConfig *GeminiThinkingConfig `json:"thinkingConfig,omitempty"`
}

// GeminiSchema for structured output. Mirrors the OpenAPI 3.0 / JSON Schema
// Draft-07 subset that Gemini's responseSchema and function-calling
// parameters accept. Fields not explicitly listed here (e.g. $ref,
// additionalProperties, if/then/else) are rejected by the API.
type GeminiSchema struct {
	// Type is the primitive JSON Schema type. In-memory we store the
	// Draft-07 lowercase form ("string", "number", "integer", "boolean",
	// "array", "object"); MarshalJSON normalises to Gemini's required
	// UPPER_SNAKE_CASE at serialization time. Missing or unknown types are
	// rejected by Gemini at the API boundary.
	Type string `json:"type"`

	// Description is the free-form natural-language hint shown to the model.
	Description string `json:"description,omitempty"`

	// Format narrows the Type — e.g. "int32"/"int64" for integer,
	// "float"/"double" for number, "date-time"/"enum" for string.
	Format string `json:"format,omitempty"`

	// Nullable flags the value as legally null. Gemini's wire form is a
	// boolean (not the JSON Schema {"type":["string","null"]} sugar).
	Nullable bool `json:"nullable,omitempty"`

	// Enum holds the allowed string values when Type=="string". For enum
	// fields Format is typically "enum" on Gemini.
	Enum []string `json:"enum,omitempty"`

	// Required is the list of property names that must appear for
	// Type=="object". Gemini enforces required even when property schemas
	// are otherwise permissive.
	Required []string `json:"required,omitempty"`

	// PropertyOrdering dictates the emission order of object properties.
	// Gemini honours this at generation time to stabilise output shape.
	PropertyOrdering []string `json:"propertyOrdering,omitempty"`

	// Properties maps field name to sub-schema for Type=="object".
	Properties map[string]*GeminiSchema `json:"properties,omitempty"`

	// Items is the element schema for Type=="array".
	Items *GeminiSchema `json:"items,omitempty"`

	// MinItems / MaxItems constrain array cardinality.
	MinItems *int64 `json:"minItems,omitempty"`
	MaxItems *int64 `json:"maxItems,omitempty"`

	// Minimum / Maximum constrain numeric range. Pointers so zero is
	// distinguishable from absent.
	Minimum *float64 `json:"minimum,omitempty"`
	Maximum *float64 `json:"maximum,omitempty"`

	// AnyOf expresses a union of allowed schemas. Gemini supports anyOf but
	// not oneOf / allOf — callers converting from Draft-07 should prefer
	// anyOf or fall back to ErrSchemaLossy.
	AnyOf []*GeminiSchema `json:"anyOf,omitempty"`
}

// MarshalJSON renders the schema in Gemini's wire shape. Gemini rejects
// Draft-07 lowercase `type` values ("string" / "object" / …) and requires
// UPPER_SNAKE_CASE enum values ("STRING" / "OBJECT" / "INTEGER" / …). We
// keep GeminiSchema.Type lowercase in-memory (matching the Draft-07 source)
// and normalise to Gemini's expected casing only at serialization time.
// Format is intentionally left untouched — Gemini accepts lowercase format
// tokens like "int32" / "date-time" / "enum".
func (g GeminiSchema) MarshalJSON() ([]byte, error) {
	type alias GeminiSchema
	a := alias(g)
	a.Type = normalizeGeminiSchemaType(a.Type)
	return json.Marshal(a)
}

// normalizeGeminiSchemaType maps JSON Schema Draft-07 `type` values to the
// UPPER_SNAKE_CASE enum Gemini's API expects. Unknown values are upper-cased
// as a best-effort so caller-provided custom types still reach the upstream.
// Empty input is preserved so omitempty on the field keeps working.
func normalizeGeminiSchemaType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "":
		return ""
	case "string":
		return "STRING"
	case "number":
		return "NUMBER"
	case "integer":
		return "INTEGER"
	case "boolean":
		return "BOOLEAN"
	case "array":
		return "ARRAY"
	case "object":
		return "OBJECT"
	default:
		return strings.ToUpper(t)
	}
}

// GeminiThinkingConfig is the thinking features configuration
type GeminiThinkingConfig struct {
	// IncludeThoughts indicates whether to include thoughts in the response
	IncludeThoughts bool `json:"includeThoughts,omitempty"`

	// ThinkingBudget is the thinking budget in tokens
	ThinkingBudget *int32 `json:"thinkingBudget,omitempty"`

	// ThinkingLevel is the level of thoughts tokens that the model should generate
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
}

// GeminiSafetySetting configures content safety filtering
type GeminiSafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

// GeminiGenerateContentResponse represents a Gemini API response
type GeminiGenerateContentResponse struct {
	Candidates     []*GeminiCandidate    `json:"candidates,omitempty"`
	PromptFeedback *GeminiPromptFeedback `json:"promptFeedback,omitempty"`
	UsageMetadata  *GeminiUsageMetadata  `json:"usageMetadata,omitempty"`
	ModelVersion   string                `json:"modelVersion,omitempty"`
	// ResponseId is a unique identifier that Gemini assigns to every response
	// (non-streaming and each streaming chunk). Round-trip it through
	// InternalLLMResponse.ID so downstream logs / dashboards stay consistent.
	ResponseId string `json:"responseId,omitempty"`
	// CreateTime is an RFC3339 timestamp for when the response was produced.
	// Mapped onto InternalLLMResponse.Created (unix seconds).
	CreateTime string `json:"createTime,omitempty"`
}

// GeminiCandidate represents a generated response candidate
type GeminiCandidate struct {
	Content       *GeminiContent        `json:"content,omitempty"`
	FinishReason  *string               `json:"finishReason,omitempty"`
	Index         int                   `json:"index"`
	SafetyRatings []*GeminiSafetyRating `json:"safetyRatings,omitempty"`

	// GroundingMetadata is emitted when the googleSearch (or broader
	// retrieval) tool is active on the request. Carries the search
	// queries Gemini issued, the chunks of text it grounded on, and
	// per-span support indices. G-H10.
	// Ref: https://ai.google.dev/api/generate-content#GroundingMetadata
	GroundingMetadata *GeminiGroundingMetadata `json:"groundingMetadata,omitempty"`

	// CitationMetadata is emitted when the model produces attributed
	// output. Each citationSource is a span in the generated text tied
	// to a source URI. G-H10.
	// Ref: https://ai.google.dev/api/generate-content#CitationMetadata
	CitationMetadata *GeminiCitationMetadata `json:"citationMetadata,omitempty"`

	// UrlContextMetadata is emitted when the urlContext tool was enabled.
	// Carries the per-URL fetch status so callers can see which URLs were
	// successfully retrieved. G-H10.
	// Ref: https://ai.google.dev/api/generate-content#UrlContextMetadata
	UrlContextMetadata *GeminiUrlContextMetadata `json:"urlContextMetadata,omitempty"`
}

// GeminiGroundingMetadata mirrors Gemini's groundingMetadata response field.
// Fields are populated best-effort — older models and simpler grounded
// responses may omit the support / entry-point sub-objects.
type GeminiGroundingMetadata struct {
	// SearchEntryPoint carries the HTML snippet Google requires grounded
	// UIs to display (the Search suggestion chip).
	SearchEntryPoint *GeminiSearchEntryPoint `json:"searchEntryPoint,omitempty"`

	// GroundingChunks are the source documents / web pages this response
	// drew on. Each chunk is a discriminated union; for now only the
	// "web" variant is modelled explicitly.
	GroundingChunks []*GeminiGroundingChunk `json:"groundingChunks,omitempty"`

	// GroundingSupports tie spans of the generated text to indices into
	// GroundingChunks. One support entry per span.
	GroundingSupports []*GeminiGroundingSupport `json:"groundingSupports,omitempty"`

	// WebSearchQueries lists the queries Gemini actually issued when
	// grounding the response.
	WebSearchQueries []string `json:"webSearchQueries,omitempty"`

	// RetrievalMetadata is an opaque JSON blob some variants emit; we
	// preserve it for passthrough without interpreting.
	RetrievalMetadata json.RawMessage `json:"retrievalMetadata,omitempty"`
}

// GeminiSearchEntryPoint carries the required Google Search suggestion UI
// chip so grounded apps can display it verbatim.
type GeminiSearchEntryPoint struct {
	RenderedContent string `json:"renderedContent,omitempty"`
	SDKBlob         string `json:"sdkBlob,omitempty"`
}

// GeminiGroundingChunk is a single source document / web page reference.
// Currently only the Web variant is modelled; other shapes are preserved
// via the raw field.
type GeminiGroundingChunk struct {
	Web *GeminiGroundingChunkWeb `json:"web,omitempty"`
}

// GeminiGroundingChunkWeb is the URL/title pair for a web-sourced chunk.
type GeminiGroundingChunkWeb struct {
	URI     string `json:"uri,omitempty"`
	Title   string `json:"title,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

// GeminiGroundingSupport ties a span of the generated text to the source
// chunks that supported it. GroundingChunkIndices are indices into the
// sibling GroundingChunks slice.
type GeminiGroundingSupport struct {
	Segment               *GeminiGroundingSegment `json:"segment,omitempty"`
	GroundingChunkIndices []int                   `json:"groundingChunkIndices,omitempty"`
	ConfidenceScores      []float64               `json:"confidenceScores,omitempty"`
}

// GeminiGroundingSegment is a byte-offset span into the generated text,
// with the literal text included for convenience.
type GeminiGroundingSegment struct {
	StartIndex int    `json:"startIndex,omitempty"`
	EndIndex   int    `json:"endIndex,omitempty"`
	Text       string `json:"text,omitempty"`
	PartIndex  int    `json:"partIndex,omitempty"`
}

// GeminiCitationMetadata mirrors Gemini's citationMetadata response field.
type GeminiCitationMetadata struct {
	CitationSources []*GeminiCitationSource `json:"citationSources,omitempty"`
}

// GeminiCitationSource is a single inline citation: a span of the generated
// text tied to a source URI and optional license.
type GeminiCitationSource struct {
	StartIndex int    `json:"startIndex,omitempty"`
	EndIndex   int    `json:"endIndex,omitempty"`
	URI        string `json:"uri,omitempty"`
	Title      string `json:"title,omitempty"`
	License    string `json:"license,omitempty"`
}

// GeminiUrlContextMetadata mirrors the urlContext tool's per-URL retrieval
// status payload.
type GeminiUrlContextMetadata struct {
	URLMetadata []*GeminiURLMetadata `json:"urlMetadata,omitempty"`
}

// GeminiURLMetadata is a single URL fetch status entry.
type GeminiURLMetadata struct {
	RetrievedURL        string `json:"retrievedUrl,omitempty"`
	URLRetrievalStatus  string `json:"urlRetrievalStatus,omitempty"`
	URL                 string `json:"url,omitempty"` // some variants emit "url" instead
}

// GeminiSafetyRating represents content safety evaluation
type GeminiSafetyRating struct {
	Category    string `json:"category"`
	Probability string `json:"probability"`
	Blocked     bool   `json:"blocked,omitempty"`
}

// GeminiPromptFeedback provides feedback on the prompt
type GeminiPromptFeedback struct {
	BlockReason   string                `json:"blockReason,omitempty"`
	SafetyRatings []*GeminiSafetyRating `json:"safetyRatings,omitempty"`
}

// GeminiUsageMetadata provides token usage information
type GeminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount,omitempty"`
	TotalTokenCount      int `json:"totalTokenCount"`

	// CachedContentTokenCount is the number of tokens in the cached content
	CachedContentTokenCount int `json:"cachedContentTokenCount,omitempty"`

	// ThoughtsTokenCount is the number of tokens in the model's thoughts
	ThoughtsTokenCount int `json:"thoughtsTokenCount,omitempty"`

	// ToolUsePromptTokenCount is the subset of PromptTokenCount consumed by
	// tool use prompts during multi-turn function calling.
	ToolUsePromptTokenCount int `json:"toolUsePromptTokenCount,omitempty"`

	// Per-modality breakdowns. Each entry carries {modality, tokenCount}.
	// See https://ai.google.dev/api/generate-content#UsageMetadata
	PromptTokensDetails        []GeminiModalityTokenCount `json:"promptTokensDetails,omitempty"`
	CandidatesTokensDetails    []GeminiModalityTokenCount `json:"candidatesTokensDetails,omitempty"`
	CacheTokensDetails         []GeminiModalityTokenCount `json:"cacheTokensDetails,omitempty"`
	ToolUsePromptTokensDetails []GeminiModalityTokenCount `json:"toolUsePromptTokensDetails,omitempty"`
}

// GeminiModalityTokenCount carries a single modality's contribution to a
// token count (e.g. TEXT=120, IMAGE=34).
type GeminiModalityTokenCount struct {
	Modality   string `json:"modality"`
	TokenCount int    `json:"tokenCount"`
}
