package model

import "strings"

// FinishReason is the canonical, typed representation of the reason a model
// stopped generating a response. It is the union of stop reasons reported by
// OpenAI, Anthropic and Gemini, so callers can distinguish safety blocks,
// refusals and pause-turn events from ordinary completions even when the
// request traverses multiple providers.
//
// Values are intentionally usable as wire strings: the zero value serialises
// as the empty string, and known values map 1:1 to lower-case snake_case so
// the type can be stored in Choice.FinishReason (which remains *string for
// JSON compatibility).
type FinishReason string

const (
	FinishReasonUnknown       FinishReason = ""
	FinishReasonStop          FinishReason = "stop"
	FinishReasonLength        FinishReason = "length"
	FinishReasonToolCalls     FinishReason = "tool_calls"
	FinishReasonFunctionCall  FinishReason = "function_call"
	FinishReasonStopSequence  FinishReason = "stop_sequence"
	FinishReasonContentFilter FinishReason = "content_filter"
	FinishReasonRefusal       FinishReason = "refusal"
	FinishReasonPauseTurn     FinishReason = "pause_turn"
	FinishReasonSafety        FinishReason = "safety"
	FinishReasonRecitation    FinishReason = "recitation"
	FinishReasonBlocklist     FinishReason = "blocklist"
	FinishReasonProhibited    FinishReason = "prohibited_content"
	FinishReasonSPII          FinishReason = "spii"
	FinishReasonLanguage      FinishReason = "language"
	FinishReasonImageSafety   FinishReason = "image_safety"
	FinishReasonMalformedCall FinishReason = "malformed_function_call"
	FinishReasonError         FinishReason = "error"
	FinishReasonOther         FinishReason = "other"
)

// ParseFinishReason interprets a canonical FinishReason string (as produced
// by String or by the FromXxx helpers). Unknown values are returned verbatim
// so lossless round-trips through the typed layer remain possible.
func ParseFinishReason(s string) FinishReason {
	return FinishReason(s)
}

// FinishReasonFromAnthropic maps an Anthropic stop_reason wire value to a
// canonical FinishReason. Unknown values are returned verbatim.
func FinishReasonFromAnthropic(raw string) FinishReason {
	switch raw {
	case "":
		return FinishReasonUnknown
	case "end_turn":
		return FinishReasonStop
	case "max_tokens":
		return FinishReasonLength
	case "stop_sequence":
		return FinishReasonStopSequence
	case "tool_use":
		return FinishReasonToolCalls
	case "pause_turn":
		return FinishReasonPauseTurn
	case "refusal":
		return FinishReasonRefusal
	default:
		return FinishReason(raw)
	}
}

// FinishReasonFromOpenAI maps an OpenAI finish_reason wire value to a
// canonical FinishReason.
func FinishReasonFromOpenAI(raw string) FinishReason {
	switch raw {
	case "":
		return FinishReasonUnknown
	case "stop":
		return FinishReasonStop
	case "length":
		return FinishReasonLength
	case "tool_calls":
		return FinishReasonToolCalls
	case "function_call":
		return FinishReasonFunctionCall
	case "content_filter":
		return FinishReasonContentFilter
	case "refusal":
		return FinishReasonRefusal
	case "error":
		return FinishReasonError
	default:
		return FinishReason(raw)
	}
}

// FinishReasonFromGemini maps a Gemini finishReason wire value to a
// canonical FinishReason. Values are case-insensitive upstream, so we accept
// either form.
func FinishReasonFromGemini(raw string) FinishReason {
	switch strings.ToUpper(raw) {
	case "":
		return FinishReasonUnknown
	case "STOP":
		return FinishReasonStop
	case "MAX_TOKENS":
		return FinishReasonLength
	case "SAFETY":
		return FinishReasonSafety
	case "RECITATION":
		return FinishReasonRecitation
	case "LANGUAGE":
		return FinishReasonLanguage
	case "BLOCKLIST":
		return FinishReasonBlocklist
	case "PROHIBITED_CONTENT":
		return FinishReasonProhibited
	case "SPII":
		return FinishReasonSPII
	case "MALFORMED_FUNCTION_CALL":
		return FinishReasonMalformedCall
	case "IMAGE_SAFETY":
		return FinishReasonImageSafety
	case "OTHER":
		return FinishReasonOther
	default:
		return FinishReason(raw)
	}
}

// String returns the canonical wire string for the reason. Useful when the
// caller needs a *string pointer for Choice.FinishReason.
func (r FinishReason) String() string {
	return string(r)
}

// IsZero reports whether the reason is the zero/unknown value.
func (r FinishReason) IsZero() bool {
	return r == FinishReasonUnknown
}

// IsSafetyBlock reports whether the reason indicates the model output was
// blocked by a safety / moderation / compliance filter rather than finishing
// normally.
func (r FinishReason) IsSafetyBlock() bool {
	switch r {
	case FinishReasonContentFilter, FinishReasonRefusal, FinishReasonSafety,
		FinishReasonRecitation, FinishReasonBlocklist, FinishReasonProhibited,
		FinishReasonSPII, FinishReasonImageSafety:
		return true
	default:
		return false
	}
}

// ToAnthropic returns the Anthropic stop_reason wire string. Values that do
// not have a direct Anthropic counterpart collapse to the closest semantic
// neighbour (safety-like → "refusal", tool-like → "tool_use", etc.) so the
// emitted string always satisfies the Anthropic schema.
func (r FinishReason) ToAnthropic() string {
	switch r {
	case FinishReasonUnknown:
		return ""
	case FinishReasonStop:
		return "end_turn"
	case FinishReasonLength:
		return "max_tokens"
	case FinishReasonStopSequence:
		return "stop_sequence"
	case FinishReasonToolCalls, FinishReasonFunctionCall, FinishReasonMalformedCall:
		return "tool_use"
	case FinishReasonPauseTurn:
		return "pause_turn"
	case FinishReasonRefusal, FinishReasonContentFilter, FinishReasonSafety,
		FinishReasonRecitation, FinishReasonBlocklist, FinishReasonProhibited,
		FinishReasonSPII, FinishReasonImageSafety:
		return "refusal"
	case FinishReasonError, FinishReasonLanguage, FinishReasonOther:
		return "end_turn"
	default:
		return string(r)
	}
}

// ToOpenAI returns the OpenAI finish_reason wire string. Safety-like values
// collapse to "content_filter"; internal/transport errors collapse to
// "error" which, while not part of the public OpenAI contract, is already
// emitted by this codebase for failed Responses-API conversions and is
// tolerated by all major OpenAI SDKs we target.
func (r FinishReason) ToOpenAI() string {
	switch r {
	case FinishReasonUnknown:
		return ""
	case FinishReasonStop, FinishReasonStopSequence, FinishReasonPauseTurn,
		FinishReasonLanguage, FinishReasonOther:
		return "stop"
	case FinishReasonLength:
		return "length"
	case FinishReasonToolCalls:
		return "tool_calls"
	case FinishReasonFunctionCall:
		return "function_call"
	case FinishReasonContentFilter, FinishReasonRefusal, FinishReasonSafety,
		FinishReasonRecitation, FinishReasonBlocklist, FinishReasonProhibited,
		FinishReasonSPII, FinishReasonImageSafety:
		return "content_filter"
	case FinishReasonError, FinishReasonMalformedCall:
		return "error"
	default:
		return string(r)
	}
}

// ToGemini returns the Gemini finishReason wire string (UPPER_SNAKE_CASE).
// Values without a Gemini counterpart fall back to "STOP" (natural completion)
// or "OTHER" (error-like) rather than leaking lower-case canonical strings.
func (r FinishReason) ToGemini() string {
	switch r {
	case FinishReasonUnknown:
		return ""
	case FinishReasonStop, FinishReasonStopSequence, FinishReasonToolCalls,
		FinishReasonFunctionCall, FinishReasonPauseTurn:
		return "STOP"
	case FinishReasonLength:
		return "MAX_TOKENS"
	case FinishReasonSafety, FinishReasonContentFilter, FinishReasonRefusal:
		return "SAFETY"
	case FinishReasonRecitation:
		return "RECITATION"
	case FinishReasonLanguage:
		return "LANGUAGE"
	case FinishReasonBlocklist:
		return "BLOCKLIST"
	case FinishReasonProhibited:
		return "PROHIBITED_CONTENT"
	case FinishReasonSPII:
		return "SPII"
	case FinishReasonMalformedCall:
		return "MALFORMED_FUNCTION_CALL"
	case FinishReasonImageSafety:
		return "IMAGE_SAFETY"
	case FinishReasonError, FinishReasonOther:
		return "OTHER"
	default:
		return strings.ToUpper(string(r))
	}
}
