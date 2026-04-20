package model

import (
	"encoding/json"
	"testing"
)

func TestNamedToolChoiceResolvedFunctionName(t *testing.T) {
	name := "lookup"
	openAI := &NamedToolChoice{
		Type:     "function",
		Function: &ToolFunction{Name: name},
	}
	if got := openAI.ResolvedFunctionName(); got != name {
		t.Errorf("openai style: got %q want %q", got, name)
	}

	anthName := "search"
	anth := &NamedToolChoice{
		Type: "tool",
		Name: &anthName,
	}
	if got := anth.ResolvedFunctionName(); got != anthName {
		t.Errorf("anthropic style: got %q want %q", got, anthName)
	}

	both := &NamedToolChoice{
		Type:     "tool",
		Name:     &anthName,
		Function: &ToolFunction{Name: name},
	}
	if got := both.ResolvedFunctionName(); got != anthName {
		t.Errorf("both set: expected Name to win, got %q", got)
	}

	none := &NamedToolChoice{Type: "any"}
	if got := none.ResolvedFunctionName(); got != "" {
		t.Errorf("no name: got %q", got)
	}
}

func TestToolChoiceMarshalRoundTrip(t *testing.T) {
	// OpenAI-style
	strMode := "required"
	tc := ToolChoice{ToolChoice: &strMode}
	b, err := json.Marshal(tc)
	if err != nil || string(b) != `"required"` {
		t.Fatalf("string form marshal: %s err=%v", b, err)
	}

	// Rich form with Name + DisableParallelToolUse
	disable := true
	name := "finder"
	named := ToolChoice{
		NamedToolChoice: &NamedToolChoice{
			Type:                   "tool",
			Name:                   &name,
			DisableParallelToolUse: &disable,
		},
	}
	b, err = json.Marshal(named)
	if err != nil {
		t.Fatalf("named marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if parsed["type"] != "tool" {
		t.Errorf("type not round-tripped: %v", parsed)
	}
	if parsed["name"] != "finder" {
		t.Errorf("name not round-tripped: %v", parsed)
	}
	if parsed["disable_parallel_tool_use"] != true {
		t.Errorf("disable_parallel_tool_use not round-tripped: %v", parsed)
	}
}
