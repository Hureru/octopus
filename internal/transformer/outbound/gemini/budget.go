package gemini

import (
	"strings"

	"github.com/bestruirui/octopus/internal/utils/log"
)

// Gemini thinking configuration reference:
//   https://ai.google.dev/gemini-api/docs/thinking
//   https://ai.google.dev/gemini-api/docs/gemini-3
//
// The API exposes two distinct levers:
//   - thinkingBudget: an int token cap accepted by Gemini 2.5 (0 disables
//     thinking, -1 lets the model decide dynamically, any positive value is a
//     hard cap). Gemini 3.x REJECTS thinkingBudget.
//   - thinkingLevel: a string ("minimal" / "low" / "medium" / "high")
//     accepted only by Gemini 3.x. Neither "none" nor "dynamic" are valid
//     wire values — emitting them triggers a 400. To express "dynamic" on
//     Gemini 3 we omit thinkingLevel and rely on the server default (which
//     the docs describe as dynamic "high"). To express "disable" on Gemini 3
//     we emit the lowest supported level (see per-family notes below)
//     because Gemini 3 cannot be fully disabled per the public docs.
//
// Family-specific ranges / levels (as of 2026-04):
//   - Gemini 2.5 Flash:      0      .. 24576    (integer budget)
//   - Gemini 2.5 Flash-Lite: classified as NoThinking — ThinkingConfig omitted.
//   - Gemini 2.5 Pro:        128    .. 32768    (0 rejected by the API)
//   - Gemini 3 Flash:        thinkingLevel ∈ {minimal, low, medium, high}
//   - Gemini 3 Pro:          thinkingLevel ∈ {low, high}  (no minimal / medium)
//   - Gemini 3.1 Pro:        thinkingLevel ∈ {low, medium, high}  (no
//     minimal; cannot be disabled — disable requests are promoted to "low")
//
// resolveThinkingConfig picks the right lever for a given model family and
// falls back through three priority tiers:
//   1. request.ReasoningBudget pointer (honors an explicit 0 or -1)
//   2. request.ReasoningEffort string ("low" / "medium" / "high" / "minimal")
//   3. model-family default (dynamic — Gemini decides)
//
// If the client set AdaptiveThinking the whole result reduces to the dynamic
// sentinel regardless of explicit budget/effort.

// thinkingDecision carries the resolved thinking configuration so callers can
// populate a GeminiThinkingConfig without re-deriving everything themselves.
//
// Interpretation for Gemini 3 family:
//   - UseLevel=true, Level=""      → omit thinkingLevel (server-side dynamic default)
//   - UseLevel=true, Level="..."   → emit the exact string (must be one of
//     {minimal, low, medium, high} — callers never receive "none" / "dynamic")
//
// For Gemini 2.5 family:
//   - UseLevel=false, Budget=0     → disable thinking
//   - UseLevel=false, Budget=-1    → dynamic allocation
//   - UseLevel=false, Budget>0     → hard cap
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
	// when UseLevel is true). An empty string means "let the server decide".
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
	geminiFamily3Flash                         // 3.x flash: thinkingLevel ∈ {minimal, low, medium, high}
	geminiFamily3Pro                           // 3.0 pro: thinkingLevel ∈ {low, high}
	geminiFamily31Pro                          // 3.1 pro: thinkingLevel ∈ {low, medium, high}; cannot disable
)

// classifyGeminiFamily inspects the model ID to decide which thinking scheme
// applies. Matching is conservative: unknown models default to the 2.5 Flash
// tier because its [0, 24576] range is the safest superset for the integer
// budget lever. Unknown Gemini 3.x variants default to 3 Flash (the most
// permissive level-based tier) so that caller-supplied levels are preserved
// whenever possible.
//
// Ordering is important:
//   - "flash-lite" first so Gemini 2.5 Flash-Lite doesn't match the generic
//     "flash" branch below.
//   - "3.1" ahead of "gemini-3" because "gemini-3.1-pro" contains both and
//     we need the more specific classification to win.
func classifyGeminiFamily(modelID string) geminiFamily {
	id := strings.ToLower(modelID)
	if id == "" {
		return geminiFamily25Flash
	}
	if strings.Contains(id, "flash-lite") {
		return geminiFamilyNoThinking
	}
	// Gemini 3.1 (currently only released as Pro).
	if strings.Contains(id, "3.1") {
		return geminiFamily31Pro
	}
	// Gemini 3.x general.
	if strings.Contains(id, "gemini-3") || strings.Contains(id, "-3-") || strings.HasSuffix(id, "-3") {
		switch {
		case strings.Contains(id, "flash"):
			return geminiFamily3Flash
		case strings.Contains(id, "pro"):
			return geminiFamily3Pro
		default:
			// Unknown 3.x variant — default to the most permissive tier so a
			// caller-supplied level is preserved verbatim.
			return geminiFamily3Flash
		}
	}
	if strings.Contains(id, "pro") {
		return geminiFamily25Pro
	}
	return geminiFamily25Flash
}

// isGemini3Family reports whether the family uses thinkingLevel rather than
// the integer thinkingBudget.
func isGemini3Family(fam geminiFamily) bool {
	switch fam {
	case geminiFamily3Flash, geminiFamily3Pro, geminiFamily31Pro:
		return true
	}
	return false
}

// dynamicDecision represents "let Gemini decide" — budget=-1 on 2.5 and an
// empty level (with includeThoughts) on 3.x so that the wire payload does not
// include the unsupported "dynamic" string.
func dynamicDecision(fam geminiFamily) thinkingDecision {
	if isGemini3Family(fam) {
		return thinkingDecision{Supported: true, UseLevel: true, Level: "", IncludeThoughts: true}
	}
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
// translate a positive budget into the closest thinkingLevel tier. Disable
// (budget=0) becomes the lowest supported level because Gemini 3 cannot be
// fully disabled. -1 (dynamic) collapses to an empty level (server default).
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
		case geminiFamily3Flash:
			// Gemini 3 Flash supports "minimal" — the closest to disabled.
			return thinkingDecision{Supported: true, UseLevel: true, Level: "minimal", IncludeThoughts: false}
		case geminiFamily3Pro:
			// Gemini 3 Pro only has low/high; approximate disabled with "low".
			log.Warnf("gemini: disable-thinking is not supported by %s; clamping up to 'low'", familyDisplayName(fam))
			return thinkingDecision{Supported: true, UseLevel: true, Level: "low", IncludeThoughts: false}
		case geminiFamily31Pro:
			// Gemini 3.1 Pro cannot be disabled at all per the docs.
			log.Warnf("gemini: %s cannot disable thinking; clamping up to 'low'", familyDisplayName(fam))
			return thinkingDecision{Supported: true, UseLevel: true, Level: "low", IncludeThoughts: false}
		default:
			return thinkingDecision{Supported: true, Budget: 0, IncludeThoughts: false}
		}
	default:
		if isGemini3Family(fam) {
			// Gemini 3 rejects thinkingBudget; approximate the caller's
			// intent with a thinkingLevel tier, then coerce to the
			// sub-family's supported set.
			level := budgetToLevel(int32(budget))
			level = clampLevelToFamily(fam, level)
			return thinkingDecision{Supported: true, UseLevel: true, Level: level, IncludeThoughts: true}
		}
		b := clampGeminiBudget(fam, int32(budget))
		return thinkingDecision{Supported: true, Budget: b, IncludeThoughts: true}
	}
}

func decisionFromEffort(fam geminiFamily, effort string) thinkingDecision {
	if isGemini3Family(fam) {
		level := map3EffortToLevel(effort)
		switch level {
		case "off":
			// Treat effort=off/none as budget=0 so the family-specific
			// disable-semantics in decisionFromBudget apply.
			return decisionFromBudget(fam, 0)
		case "":
			// Unknown / unspecified effort → dynamic.
			return dynamicDecision(fam)
		default:
			clamped := clampLevelToFamily(fam, level)
			return thinkingDecision{Supported: true, UseLevel: true, Level: clamped, IncludeThoughts: true}
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

// map3EffortToLevel maps the OpenAI reasoning_effort keyword to a Gemini 3.x
// thinkingLevel tier. The returned string is pre-clamp — callers should run
// it through clampLevelToFamily for sub-family restrictions. Special return
// values:
//
//	"off" → caller should invoke the family-specific disable path
//	""    → unknown / unspecified; caller should emit dynamic (no level)
func map3EffortToLevel(effort string) string {
	switch effort {
	case "none", "off":
		return "off"
	case "minimal":
		return "minimal"
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	default:
		return ""
	}
}

// clampLevelToFamily coerces a thinkingLevel string to one supported by the
// given Gemini 3 sub-family. Unknown levels are returned unchanged so that
// caller-supplied values reach the upstream (Gemini will surface the 400
// rather than this project silently rewriting).
//
//	geminiFamily3Flash : {minimal, low, medium, high}  (no coercion)
//	geminiFamily3Pro   : {low, high}                   (minimal→low, medium→high)
//	geminiFamily31Pro  : {low, medium, high}           (minimal→low)
func clampLevelToFamily(fam geminiFamily, level string) string {
	switch fam {
	case geminiFamily3Flash:
		return level
	case geminiFamily3Pro:
		switch level {
		case "minimal", "low":
			return "low"
		case "medium", "high":
			return "high"
		}
	case geminiFamily31Pro:
		switch level {
		case "minimal", "low":
			return "low"
		case "medium":
			return "medium"
		case "high":
			return "high"
		}
	}
	return level
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
	case geminiFamily3Flash:
		return "gemini-3-flash"
	case geminiFamily3Pro:
		return "gemini-3-pro"
	case geminiFamily31Pro:
		return "gemini-3.1-pro"
	default:
		return "gemini-unknown"
	}
}

// budgetToLevel approximates an integer thinkingBudget as a Gemini 3
// thinkingLevel tier. Used when a client carries a budget across the
// 2.5 → 3 boundary. The returned level is pre-clamp — sub-family
// restrictions are applied by clampLevelToFamily afterwards.
func budgetToLevel(b int32) string {
	switch {
	case b <= 0:
		return "minimal"
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
