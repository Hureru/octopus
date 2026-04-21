package gemini

import "testing"

func TestResolveThinkingConfigAdaptive(t *testing.T) {
	// Gemini 2.5 adaptive -> dynamic budget (-1)
	d := resolveThinkingConfig("gemini-2.5-flash", nil, "", true)
	if !d.Supported || d.UseLevel || d.Budget != -1 || !d.IncludeThoughts {
		t.Fatalf("adaptive 2.5: got %+v", d)
	}
	// Gemini 3 adaptive -> dynamic budget (-1), avoid emitting unsupported dynamic level
	d = resolveThinkingConfig("gemini-3.0-pro", nil, "", true)
	if !d.Supported || d.UseLevel || d.Budget != -1 || !d.IncludeThoughts {
		t.Fatalf("adaptive 3: got %+v", d)
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

	// Gemini 3 uses thinkingLevel instead of budget.
	d := resolveThinkingConfig("gemini-3.0-flash", nil, "medium", false)
	if !d.UseLevel || d.Level != "medium" {
		t.Errorf("gemini-3 medium: got %+v", d)
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
// tier and adaptive (-1) / explicit zero are preserved as dynamic / none.
func TestResolveThinkingConfigGemini3TranslatesBudgetToLevel(t *testing.T) {
	small := int64(1000)
	d := resolveThinkingConfig("gemini-3.0-pro", &small, "", false)
	if !d.UseLevel || d.Level != "low" {
		t.Fatalf("gemini-3 small budget -> level=low, got %+v", d)
	}

	medium := int64(4096)
	d = resolveThinkingConfig("gemini-3.0-pro", &medium, "", false)
	if !d.UseLevel || d.Level != "medium" {
		t.Fatalf("gemini-3 medium budget -> level=medium, got %+v", d)
	}

	large := int64(20000)
	d = resolveThinkingConfig("gemini-3.0-pro", &large, "", false)
	if !d.UseLevel || d.Level != "high" {
		t.Fatalf("gemini-3 large budget -> level=high, got %+v", d)
	}

	zero := int64(0)
	d = resolveThinkingConfig("gemini-3.0-pro", &zero, "", false)
	if !d.UseLevel || d.Level != "none" || d.IncludeThoughts {
		t.Fatalf("gemini-3 budget=0 -> level=none, got %+v", d)
	}

	dynamic := int64(-1)
	d = resolveThinkingConfig("gemini-3.0-pro", &dynamic, "", false)
	if d.UseLevel || d.Budget != -1 {
		t.Fatalf("gemini-3 budget=-1 -> dynamic, got %+v", d)
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
