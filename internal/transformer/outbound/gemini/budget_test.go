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
