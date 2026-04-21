package gemini

import "testing"

// TestResolveThinkingConfigAdaptive verifies the adaptive-thinking sentinel
// collapses to the family-appropriate dynamic representation: budget=-1 for
// 2.5 (int budget) and an empty level for 3.x (so the wire payload doesn't
// include the unsupported "dynamic" string).
func TestResolveThinkingConfigAdaptive(t *testing.T) {
	d := resolveThinkingConfig("gemini-2.5-flash", nil, "", true)
	if !d.Supported || d.UseLevel || d.Budget != -1 || !d.IncludeThoughts {
		t.Fatalf("adaptive 2.5: got %+v", d)
	}
	d = resolveThinkingConfig("gemini-3.0-pro", nil, "", true)
	if !d.Supported || !d.UseLevel || d.Level != "" || !d.IncludeThoughts {
		t.Fatalf("adaptive 3.0-pro: got %+v", d)
	}
	d = resolveThinkingConfig("gemini-3.1-pro", nil, "", true)
	if !d.Supported || !d.UseLevel || d.Level != "" || !d.IncludeThoughts {
		t.Fatalf("adaptive 3.1-pro: got %+v", d)
	}
}

func TestResolveThinkingConfigBudgetRespectsZeroAndDynamic(t *testing.T) {
	zero := int64(0)
	d := resolveThinkingConfig("gemini-2.5-flash", &zero, "", false)
	if !d.Supported || d.UseLevel || d.Budget != 0 || d.IncludeThoughts {
		t.Fatalf("budget=0: got %+v", d)
	}

	neg := int64(-1)
	d = resolveThinkingConfig("gemini-2.5-pro", &neg, "", false)
	if d.Budget != -1 || !d.IncludeThoughts {
		t.Fatalf("budget=-1: got %+v", d)
	}

	big := int64(100000)
	d = resolveThinkingConfig("gemini-2.5-pro", &big, "", false)
	if d.Budget > 32768 {
		t.Fatalf("clamp: got %+v", d)
	}
}

func TestResolveThinkingConfigEffortFallback(t *testing.T) {
	cases := map[string]int32{
		"low":     1024,
		"medium":  4096,
		"high":    24576,
		"minimal": 0,
	}
	for eff, want := range cases {
		d := resolveThinkingConfig("gemini-2.5-flash", nil, eff, false)
		if d.UseLevel || d.Budget != want {
			t.Errorf("effort=%s: got budget=%d want=%d", eff, d.Budget, want)
		}
	}

	// Gemini 3 Flash preserves every effort keyword verbatim as a level.
	d := resolveThinkingConfig("gemini-3.0-flash", nil, "medium", false)
	if !d.UseLevel || d.Level != "medium" {
		t.Errorf("gemini-3-flash medium: got %+v", d)
	}
}

func TestResolveThinkingConfigFlashLiteDisabled(t *testing.T) {
	d := resolveThinkingConfig("gemini-2.5-flash-lite", nil, "high", false)
	if d.Supported {
		t.Fatalf("flash-lite should not support thinking: %+v", d)
	}
}

// TestResolveThinkingConfigBudgetFamilyClamp verifies family-specific bounds:
// - Pro 2.5 rejects budget=0 (promoted to 128) and caps at 32768.
// - Flash 2.5 accepts 0 (disabled) and caps at 24576.
// Regression guard for G-C5. Ref: https://ai.google.dev/gemini-api/docs/thinking
func TestResolveThinkingConfigBudgetFamilyClamp(t *testing.T) {
	zero := int64(0)
	d := resolveThinkingConfig("gemini-2.5-pro", &zero, "", false)
	if !d.Supported || d.UseLevel || d.Budget != 128 || !d.IncludeThoughts {
		t.Fatalf("pro budget=0 should clamp to min=128: %+v", d)
	}

	tooBig := int64(50000)
	d = resolveThinkingConfig("gemini-2.5-pro", &tooBig, "", false)
	if d.Budget != 32768 {
		t.Fatalf("pro clamp max=32768, got %+v", d)
	}

	overFlash := int64(30000)
	d = resolveThinkingConfig("gemini-2.5-flash", &overFlash, "", false)
	if d.Budget != 24576 {
		t.Fatalf("flash clamp max=24576, got %+v", d)
	}

	flashZero := int64(0)
	d = resolveThinkingConfig("gemini-2.5-flash", &flashZero, "", false)
	if d.Budget != 0 || d.IncludeThoughts {
		t.Fatalf("flash budget=0 preserved: %+v", d)
	}
}

// TestResolveThinkingConfigGemini3TranslatesBudgetToLevel covers the
// level-only constraint: Gemini 3 rejects thinkingBudget entirely, so a
// client-supplied integer budget is mapped to the closest thinkingLevel
// tier. budget=0 coerces to the lowest supported tier per sub-family (since
// Gemini 3 cannot be fully disabled) and -1 collapses to an empty level so
// the wire payload doesn't include the unsupported "dynamic" string.
func TestResolveThinkingConfigGemini3TranslatesBudgetToLevel(t *testing.T) {
	small := int64(1000)
	// On Gemini 3 Pro (no "minimal"), small budget ~ "low" (budgetToLevel returns "low").
	d := resolveThinkingConfig("gemini-3.0-pro", &small, "", false)
	if !d.UseLevel || d.Level != "low" {
		t.Fatalf("gemini-3-pro small budget -> level=low, got %+v", d)
	}

	medium := int64(4096)
	// 3.0 Pro only supports {low, high} — medium coerces up to "high".
	d = resolveThinkingConfig("gemini-3.0-pro", &medium, "", false)
	if !d.UseLevel || d.Level != "high" {
		t.Fatalf("gemini-3-pro medium budget -> level=high, got %+v", d)
	}

	// 3.1 Pro supports medium natively.
	d = resolveThinkingConfig("gemini-3.1-pro", &medium, "", false)
	if !d.UseLevel || d.Level != "medium" {
		t.Fatalf("gemini-3.1-pro medium budget -> level=medium, got %+v", d)
	}

	large := int64(20000)
	d = resolveThinkingConfig("gemini-3.0-pro", &large, "", false)
	if !d.UseLevel || d.Level != "high" {
		t.Fatalf("gemini-3-pro large budget -> level=high, got %+v", d)
	}

	// budget=0 on Gemini 3 Flash → "minimal" (lowest supported, closest to disabled).
	zero := int64(0)
	d = resolveThinkingConfig("gemini-3.0-flash", &zero, "", false)
	if !d.UseLevel || d.Level != "minimal" || d.IncludeThoughts {
		t.Fatalf("gemini-3-flash budget=0 -> level=minimal, got %+v", d)
	}
	// budget=0 on Gemini 3 Pro → "low" (no "minimal" tier).
	d = resolveThinkingConfig("gemini-3.0-pro", &zero, "", false)
	if !d.UseLevel || d.Level != "low" || d.IncludeThoughts {
		t.Fatalf("gemini-3-pro budget=0 -> level=low, got %+v", d)
	}
	// budget=0 on Gemini 3.1 Pro → "low" (cannot disable at all).
	d = resolveThinkingConfig("gemini-3.1-pro", &zero, "", false)
	if !d.UseLevel || d.Level != "low" || d.IncludeThoughts {
		t.Fatalf("gemini-3.1-pro budget=0 -> level=low, got %+v", d)
	}

	// budget=-1 on any Gemini 3 family → empty level (server-side dynamic default).
	dynamic := int64(-1)
	d = resolveThinkingConfig("gemini-3.0-pro", &dynamic, "", false)
	if !d.UseLevel || d.Level != "" || !d.IncludeThoughts {
		t.Fatalf("gemini-3-pro budget=-1 -> empty level, got %+v", d)
	}
}

// TestResolveThinkingConfigProEffortMinimal guards a regression where
// effort="minimal" returned budget=0 on pro, which pro rejects. It should
// promote to the family minimum instead.
func TestResolveThinkingConfigProEffortMinimal(t *testing.T) {
	d := resolveThinkingConfig("gemini-2.5-pro", nil, "minimal", false)
	if !d.Supported || d.UseLevel || d.Budget != 128 || !d.IncludeThoughts {
		t.Fatalf("pro effort=minimal should land at budget=128: %+v", d)
	}
}

// TestClassifyGeminiFamilyVariants pins the model-id → family table.
// Order-sensitive matches (flash-lite, 3.1 vs 3.x, flash vs pro) are the
// main failure modes, so this list covers each one explicitly.
func TestClassifyGeminiFamilyVariants(t *testing.T) {
	cases := map[string]geminiFamily{
		"":                       geminiFamily25Flash,
		"gemini-2.5-flash":       geminiFamily25Flash,
		"gemini-2.5-flash-lite":  geminiFamilyNoThinking,
		"gemini-2.5-pro":         geminiFamily25Pro,
		"gemini-3.0-flash":       geminiFamily3Flash,
		"gemini-3.0-pro":         geminiFamily3Pro,
		"gemini-3-pro":           geminiFamily3Pro,
		"gemini-3-flash":         geminiFamily3Flash,
		"gemini-3.1-pro":         geminiFamily31Pro,
		"gemini-3.1-pro-preview": geminiFamily31Pro,
		"GEMINI-3.1-PRO":         geminiFamily31Pro,
		"gemini-3":               geminiFamily3Flash, // unknown 3.x variant → permissive default
	}
	for id, want := range cases {
		if got := classifyGeminiFamily(id); got != want {
			t.Errorf("classifyGeminiFamily(%q) = %d, want %d", id, got, want)
		}
	}
}

// TestClampLevelToFamily verifies each sub-family's supported level set.
// Gemini 3 Flash: no coercion. Gemini 3 Pro: minimal/low→low, medium/high→high.
// Gemini 3.1 Pro: minimal/low→low, medium→medium, high→high.
func TestClampLevelToFamily(t *testing.T) {
	type tc struct {
		fam   geminiFamily
		level string
		want  string
	}
	cases := []tc{
		{geminiFamily3Flash, "minimal", "minimal"},
		{geminiFamily3Flash, "low", "low"},
		{geminiFamily3Flash, "medium", "medium"},
		{geminiFamily3Flash, "high", "high"},
		{geminiFamily3Pro, "minimal", "low"},
		{geminiFamily3Pro, "low", "low"},
		{geminiFamily3Pro, "medium", "high"},
		{geminiFamily3Pro, "high", "high"},
		{geminiFamily31Pro, "minimal", "low"},
		{geminiFamily31Pro, "low", "low"},
		{geminiFamily31Pro, "medium", "medium"},
		{geminiFamily31Pro, "high", "high"},
	}
	for _, c := range cases {
		if got := clampLevelToFamily(c.fam, c.level); got != c.want {
			t.Errorf("clampLevelToFamily(fam=%d, level=%q) = %q, want %q", c.fam, c.level, got, c.want)
		}
	}
}

// TestResolveThinkingConfigGemini3EffortCoercion walks effort across every
// sub-family to confirm the clamp table flows through decisionFromEffort.
func TestResolveThinkingConfigGemini3EffortCoercion(t *testing.T) {
	// 3 Flash supports minimal natively.
	d := resolveThinkingConfig("gemini-3.0-flash", nil, "minimal", false)
	if !d.UseLevel || d.Level != "minimal" {
		t.Errorf("flash effort=minimal: %+v", d)
	}

	// 3 Pro lacks minimal/medium — clamp up.
	d = resolveThinkingConfig("gemini-3.0-pro", nil, "minimal", false)
	if !d.UseLevel || d.Level != "low" {
		t.Errorf("3-pro effort=minimal → low, got %+v", d)
	}
	d = resolveThinkingConfig("gemini-3.0-pro", nil, "medium", false)
	if !d.UseLevel || d.Level != "high" {
		t.Errorf("3-pro effort=medium → high, got %+v", d)
	}

	// 3.1 Pro lacks minimal but supports medium.
	d = resolveThinkingConfig("gemini-3.1-pro", nil, "minimal", false)
	if !d.UseLevel || d.Level != "low" {
		t.Errorf("3.1-pro effort=minimal → low, got %+v", d)
	}
	d = resolveThinkingConfig("gemini-3.1-pro", nil, "medium", false)
	if !d.UseLevel || d.Level != "medium" {
		t.Errorf("3.1-pro effort=medium: %+v", d)
	}

	// effort=off/none collapses to the family disable semantics.
	d = resolveThinkingConfig("gemini-3.0-flash", nil, "off", false)
	if !d.UseLevel || d.Level != "minimal" || d.IncludeThoughts {
		t.Errorf("flash effort=off → minimal no-thoughts, got %+v", d)
	}
	d = resolveThinkingConfig("gemini-3.1-pro", nil, "off", false)
	if !d.UseLevel || d.Level != "low" || d.IncludeThoughts {
		t.Errorf("3.1-pro effort=off → low no-thoughts, got %+v", d)
	}

	// Unknown effort on any Gemini 3 family → dynamic (empty level).
	d = resolveThinkingConfig("gemini-3.0-pro", nil, "bogus", false)
	if !d.UseLevel || d.Level != "" || !d.IncludeThoughts {
		t.Errorf("3-pro effort=bogus → dynamic, got %+v", d)
	}
}

func TestCanonicalGeminiModality(t *testing.T) {
	cases := map[string]string{
		"text":    "TEXT",
		"TEXT":    "TEXT",
		"Image":   "IMAGE",
		"audio":   "AUDIO",
		" Audio ": "AUDIO",
		"video":   "",
		"":        "",
	}
	for in, want := range cases {
		if got := canonicalGeminiModality(in); got != want {
			t.Errorf("canonicalGeminiModality(%q) = %q, want %q", in, got, want)
		}
	}
}
