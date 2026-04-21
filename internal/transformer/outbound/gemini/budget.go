package gemini

import (
	"strings"

	"github.com/bestruirui/octopus/internal/utils/log"
)

// Gemini thinking configuration reference:
//   https://ai.google.dev/gemini-api/docs/thinking
//
// The API exposes two distinct levers:
//   - thinkingBudget: an int token cap (0 disables thinking, -1 lets the model
//     decide dynamically, any positive value is a hard cap).
//   - thinkingLevel: a string on Gemini 3.x that selects the reasoning tier
//     (low / medium / high) without pinning a token budget.
//
// Family-specific budget ranges (as of 2026-04):
//   - Gemini 2.5 Flash:      0      .. 24576
//   - Gemini 2.5 Flash-Lite: 512    .. 24576  (currently classified as
//     NoThinking in this project — see classifyGeminiFamily — so these
//     bounds are never consulted; kept here for documentation.)
//   - Gemini 2.5 Pro:        128    .. 32768  (0 is rejected by the API)
//   - Gemini 3 Pro:          thinkingLevel only; thinkingBudget is rejected.
//
// resolveThinkingConfig picks the right lever for a given model family and
// falls back through three priority tiers:
//   1. request.ReasoningBudget pointer (honors an explicit 0 or -1)
//   2. request.ReasoningEffort string ("low" / "medium" / "high" / "minimal")
//   3. model-family default (off for classic flash-lite, dynamic otherwise)
//
// If the client set AdaptiveThinking the whole result reduces to the dynamic
// sentinel (-1 budget) regardless of the
// explicit budget/effort — adaptive thinking is precisely the opt-in to let
// the model pick per-turn.

// thinkingDecision carries the resolved thinking configuration so callers can
// populate a GeminiThinkingConfig without re-deriving everything themselves.
type thinkingDecision struct {
	// Supported reports whether the target model family supports thinking at
	// all. When false the caller should omit ThinkingConfig entirely.
	Supported bool
	// UseLevel indicates that thinkingLevel (Gemini 3.x) should be used in
	// lieu of the integer thinkingBudget.
	UseLevel bool
	// Budget is the integer token budget (only meaningful when UseLevel is
	// false). 0 disables thinking, -1 requests dynamic allocation.
	Budget int32
	// Level is the string reasoning tier for Gemini 3.x (only meaningful
	// when UseLevel is true).
	Level string
	// IncludeThoughts mirrors the Gemini includeThoughts flag — surface
	// thoughts in the response when thinking is enabled.
	IncludeThoughts bool
}

// resolveThinkingConfig computes the thinking decision for a given model plus
// the request's reasoning intent. modelID is matched case-insensitively.
func resolveThinkingConfig(modelID string, reasoningBudget *int64, reasoningEffort string, adaptive bool) thinkingDecision {
	fam := classifyGeminiFamily(modelID)
	if fam == geminiFamilyNoThinking {
		return thinkingDecision{Supported: false}
	}

	if adaptive {
		return dynamicDecision(fam)
	}

	// Tier 1: explicit budget pointer. 0 and -1 are both meaningful signals
	// so a pointer-nil check is the only way to tell "unset" from "0".
	if reasoningBudget != nil {
		return decisionFromBudget(fam, *reasoningBudget)
	}

	// Tier 2: effort keyword.
	if effort := strings.ToLower(strings.TrimSpace(reasoningEffort)); effort != "" {
		return decisionFromEffort(fam, effort)
	}

	// Tier 3: family default.
	return defaultFamilyDecision(fam)
}

// geminiFamily is an internal classification that drives the thinking
// configuration. Values only need to be distinguishable — they are not
// serialised anywhere.
type geminiFamily int

const (
	geminiFamilyNoThinking geminiFamily = iota // flash-lite or other non-thinking models
	geminiFamily25Flash                        // 2.5 flash (budget 0..24576)
	geminiFamily25Pro                          // 2.5 pro (budget 128..32768, 0 rejected)
	geminiFamily3                              // 3.x family: level-based thinking
)

// classifyGeminiFamily inspects the model ID to decide which thinking scheme
// applies. Matching is conservative: unknown models default to the 2.5 Flash
// tier because its [0, 24576] range is the safest superset for the integer
// budget lever.
func classifyGeminiFamily(modelID string) geminiFamily {
	id := strings.ToLower(modelID)
	if id == "" {
		return geminiFamily25Flash
	}
	if strings.Contains(id, "flash-lite") {
		return geminiFamilyNoThinking
	}
	if strings.Contains(id, "gemini-3") {
		return geminiFamily3
	}
	if strings.Contains(id, "pro") {
		return geminiFamily25Pro
	}
	return geminiFamily25Flash
}

func dynamicDecision(fam geminiFamily) thinkingDecision {
	_ = fam
	return thinkingDecision{Supported: true, Budget: -1, IncludeThoughts: true}
}

func defaultFamilyDecision(fam geminiFamily) thinkingDecision {
	// Without an explicit signal we mirror adaptive behaviour — let Gemini
	// pick its own budget rather than pin an arbitrary number.
	return dynamicDecision(fam)
}

// decisionFromBudget honours caller-supplied budget values. 0 disables
// thinking on families that accept it; 2.5 Pro rejects 0 so we clamp it up
// to the family minimum rather than emit a request the API would 400. -1
// means dynamic; positive values are clamped to family-specific ranges.
//
// For Gemini 3.x the integer budget lever is not accepted by the API; we
// translate a positive budget into the closest thinkingLevel tier and keep
// the dynamic sentinel (-1) unchanged.
func decisionFromBudget(fam geminiFamily, budget int64) thinkingDecision {
	switch {
	case budget < 0:
		return dynamicDecision(fam)
	case budget == 0:
		switch fam {
		case geminiFamily25Pro:
			// Pro rejects thinkingBudget=0; pin to family minimum and
			// surface the override so operators can spot the silent bump.
			min := geminiBudgetBounds(fam).min
			log.Warnf("gemini: thinkingBudget=0 is not accepted by %s; clamping up to family minimum %d", familyDisplayName(fam), min)
			return thinkingDecision{Supported: true, Budget: min, IncludeThoughts: true}
		case geminiFamily3:
			// "disable thinking" on Gemini 3 is expressed via thinkingLevel
			// rather than a zero budget.
			return thinkingDecision{Supported: true, UseLevel: true, Level: "none", IncludeThoughts: false}
		default:
			return thinkingDecision{Supported: true, Budget: 0, IncludeThoughts: false}
		}
	default:
		if fam == geminiFamily3 {
			// Gemini 3 rejects thinkingBudget; approximate the caller's
			// intent with a thinkingLevel tier.
			return thinkingDecision{Supported: true, UseLevel: true, Level: budgetToLevel(int32(budget)), IncludeThoughts: true}
		}
		b := clampGeminiBudget(fam, int32(budget))
		return thinkingDecision{Supported: true, Budget: b, IncludeThoughts: true}
	}
}

func decisionFromEffort(fam geminiFamily, effort string) thinkingDecision {
	if fam == geminiFamily3 {
		level := map3EffortToLevel(effort)
		switch level {
		case "none":
			return thinkingDecision{Supported: true, UseLevel: true, Level: "none", IncludeThoughts: false}
		case "dynamic":
			return dynamicDecision(fam)
		default:
			return thinkingDecision{Supported: true, UseLevel: true, Level: level, IncludeThoughts: true}
		}
	}
	b := map25EffortToBudget(effort)
	if b == 0 {
		if fam == geminiFamily25Pro {
			// Pro rejects 0; promote to family minimum.
			min := geminiBudgetBounds(fam).min
			return thinkingDecision{Supported: true, Budget: min, IncludeThoughts: true}
		}
		return thinkingDecision{Supported: true, Budget: 0, IncludeThoughts: false}
	}
	if b < 0 {
		return dynamicDecision(fam)
	}
	return thinkingDecision{Supported: true, Budget: clampGeminiBudget(fam, b), IncludeThoughts: true}
}

// map25EffortToBudget keeps the historical effort-to-budget table used by
// reasoningToThinkingBudget. "minimal" maps to 0 (disabled).
func map25EffortToBudget(effort string) int32 {
	switch effort {
	case "none", "off":
		return 0
	case "minimal":
		return 0
	case "low":
		return 1024
	case "medium":
		return 4096
	case "high":
		return 24576
	default:
		return -1
	}
}

// map3EffortToLevel maps the OpenAI reasoning_effort keyword to Gemini 3.x
// thinkingLevel tier. Gemini 3 exposes "low", "medium", "high" plus the
// "minimal" is coerced to "low" (Gemini has no "minimal" tier). Unknown
// values fall back to the budget-based dynamic path instead of emitting a
// thinkingLevel the upstream may reject.
func map3EffortToLevel(effort string) string {
	switch effort {
	case "none", "off":
		return "none"
	case "minimal", "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	default:
		return "dynamic"
	}
}

// geminiBudgetRange captures the min/max thinkingBudget values a given
// family accepts. Values outside the range cause Gemini to return 400.
type geminiBudgetRange struct {
	min int32
	max int32
}

// geminiBudgetBounds reports the valid [min, max] for a budget-driven
// family. Called from clampGeminiBudget and the zero-budget special case
// in decisionFromBudget.
func geminiBudgetBounds(fam geminiFamily) geminiBudgetRange {
	switch fam {
	case geminiFamily25Pro:
		return geminiBudgetRange{min: 128, max: 32768}
	case geminiFamily25Flash:
		return geminiBudgetRange{min: 0, max: 24576}
	default:
		// NoThinking and Gemini 3 never reach here; guard conservatively.
		return geminiBudgetRange{min: 0, max: 32768}
	}
}

// familyDisplayName is only used for diagnostic logging.
func familyDisplayName(fam geminiFamily) string {
	switch fam {
	case geminiFamily25Flash:
		return "gemini-2.5-flash"
	case geminiFamily25Pro:
		return "gemini-2.5-pro"
	case geminiFamily3:
		return "gemini-3"
	default:
		return "gemini-unknown"
	}
}

// budgetToLevel approximates an integer thinkingBudget as a Gemini 3
// thinkingLevel tier. Used when a client carries a budget across the
// 2.5 → 3 boundary.
func budgetToLevel(b int32) string {
	switch {
	case b <= 0:
		return "none"
	case b <= 2048:
		return "low"
	case b <= 8192:
		return "medium"
	default:
		return "high"
	}
}

// clampGeminiBudget caps positive budgets to the family's accepted range.
// Gemini responds with HTTP 400 when the value is outside [min, max], so
// honouring the bounds here is what keeps the upstream call alive.
func clampGeminiBudget(fam geminiFamily, b int32) int32 {
	r := geminiBudgetBounds(fam)
	if b > r.max {
		return r.max
	}
	if b < r.min {
		return r.min
	}
	return b
}
