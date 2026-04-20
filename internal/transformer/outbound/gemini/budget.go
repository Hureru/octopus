package gemini

import "strings"

// Gemini thinking configuration reference:
//   https://ai.google.dev/gemini-api/docs/thinking
//
// The API exposes two distinct levers:
//   - thinkingBudget: an int token cap (0 disables thinking, -1 lets the model
//     decide dynamically, any positive value is a hard cap).
//   - thinkingLevel: a string on Gemini 3.x that selects the reasoning tier
//     (low / medium / high / dynamic) without pinning a token budget.
//
// resolveThinkingConfig picks the right lever for a given model family and
// falls back through three priority tiers:
//   1. request.ReasoningBudget pointer (honors an explicit 0 or -1)
//   2. request.ReasoningEffort string ("low" / "medium" / "high" / "minimal")
//   3. model-family default (off for classic flash-lite, dynamic otherwise)
//
// If the client set AdaptiveThinking the whole result reduces to the dynamic
// sentinel (-1 for budget, "dynamic" for Gemini 3 level) regardless of the
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
	geminiFamily25                             // 2.5 family: budget-based thinking
	geminiFamily3                              // 3.x family: level-based thinking
)

// classifyGeminiFamily inspects the model ID to decide which thinking scheme
// applies. Matching is conservative: unknown models default to the 2.5
// budget-based scheme because it is the safest superset (both 2.5 and 3.x
// accept an integer budget, but only 3.x accepts thinkingLevel).
func classifyGeminiFamily(modelID string) geminiFamily {
	id := strings.ToLower(modelID)
	if id == "" {
		return geminiFamily25
	}
	if strings.Contains(id, "flash-lite") {
		return geminiFamilyNoThinking
	}
	if strings.Contains(id, "gemini-3") || strings.Contains(id, "gemini-3.") {
		return geminiFamily3
	}
	return geminiFamily25
}

func dynamicDecision(fam geminiFamily) thinkingDecision {
	if fam == geminiFamily3 {
		return thinkingDecision{Supported: true, UseLevel: true, Level: "dynamic", IncludeThoughts: true}
	}
	return thinkingDecision{Supported: true, Budget: -1, IncludeThoughts: true}
}

func defaultFamilyDecision(fam geminiFamily) thinkingDecision {
	// Without an explicit signal we mirror adaptive behaviour — let Gemini
	// pick its own budget rather than pin an arbitrary number.
	return dynamicDecision(fam)
}

// decisionFromBudget honours caller-supplied budget values. 0 disables
// thinking; -1 means dynamic; positive values pin a hard cap.
func decisionFromBudget(fam geminiFamily, budget int64) thinkingDecision {
	switch {
	case budget == 0:
		// Explicit disable. Level-based models accept thinkingBudget=0 too.
		return thinkingDecision{Supported: true, Budget: 0, IncludeThoughts: false}
	case budget < 0:
		return dynamicDecision(fam)
	default:
		b := clampGeminiBudget(fam, int32(budget))
		return thinkingDecision{Supported: true, Budget: b, IncludeThoughts: true}
	}
}

func decisionFromEffort(fam geminiFamily, effort string) thinkingDecision {
	if fam == geminiFamily3 {
		level := map3EffortToLevel(effort)
		return thinkingDecision{Supported: true, UseLevel: true, Level: level, IncludeThoughts: level != "none"}
	}
	b := map25EffortToBudget(effort)
	if b == 0 {
		return thinkingDecision{Supported: true, Budget: 0, IncludeThoughts: false}
	}
	return thinkingDecision{Supported: true, Budget: b, IncludeThoughts: true}
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
// special "dynamic" tier; "minimal" is coerced to "low" (Gemini has no
// "minimal" tier).
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

// clampGeminiBudget caps positive budgets to values the API accepts. 2.5 Pro
// goes up to 32768; 2.5 Flash is capped at 24576. We conservatively clamp at
// 32768 across the board because the API returns 400 if the value exceeds
// the model's cap.
func clampGeminiBudget(_ geminiFamily, b int32) int32 {
	const maxBudget int32 = 32768
	if b > maxBudget {
		return maxBudget
	}
	return b
}
