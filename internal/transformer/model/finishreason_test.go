package model

import "testing"

func TestFinishReasonFromAnthropic(t *testing.T) {
	cases := map[string]FinishReason{
		"":              FinishReasonUnknown,
		"end_turn":      FinishReasonStop,
		"max_tokens":    FinishReasonLength,
		"stop_sequence": FinishReasonStopSequence,
		"tool_use":      FinishReasonToolCalls,
		"pause_turn":    FinishReasonPauseTurn,
		"refusal":       FinishReasonRefusal,
		"weird_custom":  FinishReason("weird_custom"),
	}
	for raw, want := range cases {
		if got := FinishReasonFromAnthropic(raw); got != want {
			t.Errorf("FinishReasonFromAnthropic(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestFinishReasonFromOpenAI(t *testing.T) {
	cases := map[string]FinishReason{
		"":               FinishReasonUnknown,
		"stop":           FinishReasonStop,
		"length":         FinishReasonLength,
		"tool_calls":     FinishReasonToolCalls,
		"function_call":  FinishReasonFunctionCall,
		"content_filter": FinishReasonContentFilter,
		"refusal":        FinishReasonRefusal,
		"error":          FinishReasonError,
		"brand_new":      FinishReason("brand_new"),
	}
	for raw, want := range cases {
		if got := FinishReasonFromOpenAI(raw); got != want {
			t.Errorf("FinishReasonFromOpenAI(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestFinishReasonFromGemini(t *testing.T) {
	cases := map[string]FinishReason{
		"":                        FinishReasonUnknown,
		"STOP":                    FinishReasonStop,
		"MAX_TOKENS":              FinishReasonLength,
		"SAFETY":                  FinishReasonSafety,
		"RECITATION":              FinishReasonRecitation,
		"LANGUAGE":                FinishReasonLanguage,
		"BLOCKLIST":               FinishReasonBlocklist,
		"PROHIBITED_CONTENT":      FinishReasonProhibited,
		"SPII":                    FinishReasonSPII,
		"MALFORMED_FUNCTION_CALL": FinishReasonMalformedCall,
		"IMAGE_SAFETY":            FinishReasonImageSafety,
		"OTHER":                   FinishReasonOther,
		"stop":                    FinishReasonStop, // case-insensitive
		"NEW_GEMINI_VALUE":        FinishReason("NEW_GEMINI_VALUE"),
	}
	for raw, want := range cases {
		if got := FinishReasonFromGemini(raw); got != want {
			t.Errorf("FinishReasonFromGemini(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestFinishReasonToAnthropic(t *testing.T) {
	cases := map[FinishReason]string{
		FinishReasonUnknown:       "",
		FinishReasonStop:          "end_turn",
		FinishReasonLength:        "max_tokens",
		FinishReasonStopSequence:  "stop_sequence",
		FinishReasonToolCalls:     "tool_use",
		FinishReasonFunctionCall:  "tool_use",
		FinishReasonMalformedCall: "tool_use",
		FinishReasonPauseTurn:     "pause_turn",
		FinishReasonRefusal:       "refusal",
		FinishReasonContentFilter: "refusal",
		FinishReasonSafety:        "refusal",
		FinishReasonRecitation:    "refusal",
		FinishReasonBlocklist:     "refusal",
		FinishReasonProhibited:    "refusal",
		FinishReasonSPII:          "refusal",
		FinishReasonImageSafety:   "refusal",
		FinishReasonError:         "end_turn",
		FinishReasonLanguage:      "end_turn",
		FinishReasonOther:         "end_turn",
		FinishReason("custom_x"):  "custom_x",
	}
	for r, want := range cases {
		if got := r.ToAnthropic(); got != want {
			t.Errorf("%q.ToAnthropic() = %q, want %q", r, got, want)
		}
	}
}

func TestFinishReasonToOpenAI(t *testing.T) {
	cases := map[FinishReason]string{
		FinishReasonUnknown:       "",
		FinishReasonStop:          "stop",
		FinishReasonStopSequence:  "stop",
		FinishReasonPauseTurn:     "stop",
		FinishReasonLanguage:      "stop",
		FinishReasonOther:         "stop",
		FinishReasonLength:        "length",
		FinishReasonToolCalls:     "tool_calls",
		FinishReasonFunctionCall:  "function_call",
		FinishReasonContentFilter: "content_filter",
		FinishReasonRefusal:       "content_filter",
		FinishReasonSafety:        "content_filter",
		FinishReasonRecitation:    "content_filter",
		FinishReasonBlocklist:     "content_filter",
		FinishReasonProhibited:    "content_filter",
		FinishReasonSPII:          "content_filter",
		FinishReasonImageSafety:   "content_filter",
		FinishReasonError:         "error",
		FinishReasonMalformedCall: "error",
		FinishReason("custom_x"):  "custom_x",
	}
	for r, want := range cases {
		if got := r.ToOpenAI(); got != want {
			t.Errorf("%q.ToOpenAI() = %q, want %q", r, got, want)
		}
	}
}

func TestFinishReasonToGemini(t *testing.T) {
	cases := map[FinishReason]string{
		FinishReasonUnknown:       "",
		FinishReasonStop:          "STOP",
		FinishReasonStopSequence:  "STOP",
		FinishReasonToolCalls:     "STOP",
		FinishReasonFunctionCall:  "STOP",
		FinishReasonPauseTurn:     "STOP",
		FinishReasonLength:        "MAX_TOKENS",
		FinishReasonSafety:        "SAFETY",
		FinishReasonContentFilter: "SAFETY",
		FinishReasonRefusal:       "SAFETY",
		FinishReasonRecitation:    "RECITATION",
		FinishReasonLanguage:      "LANGUAGE",
		FinishReasonBlocklist:     "BLOCKLIST",
		FinishReasonProhibited:    "PROHIBITED_CONTENT",
		FinishReasonSPII:          "SPII",
		FinishReasonMalformedCall: "MALFORMED_FUNCTION_CALL",
		FinishReasonImageSafety:   "IMAGE_SAFETY",
		FinishReasonError:         "OTHER",
		FinishReasonOther:         "OTHER",
		FinishReason("custom_x"):  "CUSTOM_X",
	}
	for r, want := range cases {
		if got := r.ToGemini(); got != want {
			t.Errorf("%q.ToGemini() = %q, want %q", r, got, want)
		}
	}
}

// TestFinishReasonRoundTripAnthropicPreservesDetail verifies that the value
// of concern in the plan (`pause_turn`, `refusal`, `stop_sequence`) survives
// outbound/anthropic → canonical → inbound/anthropic without collapsing to
// "end_turn".
func TestFinishReasonRoundTripAnthropicPreservesDetail(t *testing.T) {
	cases := []struct {
		upstream string
		want     string
	}{
		{"end_turn", "end_turn"},
		{"max_tokens", "max_tokens"},
		{"stop_sequence", "stop_sequence"},
		{"tool_use", "tool_use"},
		{"pause_turn", "pause_turn"},
		{"refusal", "refusal"},
	}
	for _, tc := range cases {
		canonical := FinishReasonFromAnthropic(tc.upstream).String()
		got := ParseFinishReason(canonical).ToAnthropic()
		if got != tc.want {
			t.Errorf("round-trip %q: canonical=%q wire=%q want %q", tc.upstream, canonical, got, tc.want)
		}
	}
}

// TestFinishReasonRoundTripGeminiSafetyReachesAnthropic verifies that Gemini
// safety-family reasons collapse to Anthropic "refusal" rather than the
// previous "end_turn" default, so Anthropic clients can see the safety block.
func TestFinishReasonRoundTripGeminiSafetyReachesAnthropic(t *testing.T) {
	cases := []struct {
		gemini         string
		wantAnthropic  string
		wantOpenAI     string
		wantSafetyFlag bool
	}{
		{"SAFETY", "refusal", "content_filter", true},
		{"RECITATION", "refusal", "content_filter", true},
		{"BLOCKLIST", "refusal", "content_filter", true},
		{"PROHIBITED_CONTENT", "refusal", "content_filter", true},
		{"SPII", "refusal", "content_filter", true},
		{"IMAGE_SAFETY", "refusal", "content_filter", true},
		{"STOP", "end_turn", "stop", false},
		{"MAX_TOKENS", "max_tokens", "length", false},
		{"MALFORMED_FUNCTION_CALL", "tool_use", "error", false},
	}
	for _, tc := range cases {
		r := FinishReasonFromGemini(tc.gemini)
		if got := r.ToAnthropic(); got != tc.wantAnthropic {
			t.Errorf("%s.ToAnthropic() = %q, want %q", tc.gemini, got, tc.wantAnthropic)
		}
		if got := r.ToOpenAI(); got != tc.wantOpenAI {
			t.Errorf("%s.ToOpenAI() = %q, want %q", tc.gemini, got, tc.wantOpenAI)
		}
		if got := r.IsSafetyBlock(); got != tc.wantSafetyFlag {
			t.Errorf("%s.IsSafetyBlock() = %v, want %v", tc.gemini, got, tc.wantSafetyFlag)
		}
	}
}

func TestFinishReasonIsZero(t *testing.T) {
	if !FinishReasonUnknown.IsZero() {
		t.Error("FinishReasonUnknown should report IsZero")
	}
	if FinishReasonStop.IsZero() {
		t.Error("FinishReasonStop should not report IsZero")
	}
}
