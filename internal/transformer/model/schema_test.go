package model

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSchemaToGeminiBasic(t *testing.T) {
	src := `{
		"type":"object",
		"description":"user object",
		"properties":{
			"name":{"type":"string","description":"full name"},
			"age":{"type":"integer","minimum":0,"maximum":150}
		},
		"required":["name"],
		"propertyOrdering":["name","age"]
	}`
	s, err := ParseSchema([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	g, err := s.ToGemini()
	if err != nil {
		t.Fatalf("to gemini: %v", err)
	}
	if g.Type != "object" || g.Description != "user object" {
		t.Fatalf("top-level mismatch: %+v", g)
	}
	if len(g.Required) != 1 || g.Required[0] != "name" {
		t.Fatalf("required: %+v", g.Required)
	}
	if g.PropertyOrdering == nil || g.PropertyOrdering[0] != "name" {
		t.Fatalf("propertyOrdering: %+v", g.PropertyOrdering)
	}
	if g.Properties["age"] == nil || g.Properties["age"].Minimum == nil || *g.Properties["age"].Minimum != 0 {
		t.Fatalf("age subschema: %+v", g.Properties["age"])
	}
}

func TestSchemaToGeminiLossyKeywords(t *testing.T) {
	src := `{
		"type":"object",
		"additionalProperties":false,
		"allOf":[{"type":"object"}],
		"pattern":"^a.*",
		"minLength":1,
		"const":"x",
		"$ref":"#/definitions/Foo"
	}`
	s, err := ParseSchema([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = s.ToGemini()
	if !errors.Is(err, ErrSchemaLossy) {
		t.Fatalf("expected ErrSchemaLossy, got %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"$ref", "additionalProperties", "allOf", "const", "pattern", "min/maxLength"} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected lossy report to mention %q: %v", want, msg)
		}
	}
}

func TestSchemaToGeminiEnumStringFormat(t *testing.T) {
	src := `{"type":"string","enum":["a","b","c"]}`
	s, _ := ParseSchema([]byte(src))
	g, err := s.ToGemini()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if g.Format != "enum" {
		t.Fatalf("expected Format=enum, got %q", g.Format)
	}
	if len(g.Enum) != 3 {
		t.Fatalf("enum not round-tripped: %+v", g.Enum)
	}
}

func TestSchemaToOpenAIResponseFormatRoundTrip(t *testing.T) {
	src := `{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`
	s, err := ParseSchema([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := s.ToOpenAIResponseFormat()
	if err != nil {
		t.Fatalf("to openai: %v", err)
	}
	var reparsed map[string]any
	if err := json.Unmarshal(out, &reparsed); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if reparsed["type"] != "object" {
		t.Fatalf("root not preserved: %v", reparsed)
	}
}

func TestResponseFormatUnmarshalWrapper(t *testing.T) {
	wire := `{
		"type":"json_schema",
		"json_schema":{
			"name":"User",
			"strict":true,
			"schema":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}
		}
	}`
	var rf ResponseFormat
	if err := json.Unmarshal([]byte(wire), &rf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rf.Name != "User" || rf.Strict == nil || !*rf.Strict {
		t.Fatalf("wrapper fields lost: %+v", rf)
	}
	if rf.Schema == nil || rf.Schema.Type != "object" {
		t.Fatalf("schema not parsed: %+v", rf.Schema)
	}
	if len(rf.RawSchema) == 0 {
		t.Fatalf("raw schema not preserved")
	}
}

func TestResponseFormatUnmarshalBareSchema(t *testing.T) {
	wire := `{"type":"json_schema","json_schema":{"type":"object","properties":{"a":{"type":"string"}}}}`
	var rf ResponseFormat
	if err := json.Unmarshal([]byte(wire), &rf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rf.Schema == nil || rf.Schema.Type != "object" {
		t.Fatalf("bare schema not parsed: %+v", rf.Schema)
	}
	if rf.Name != "" || rf.Strict != nil {
		t.Fatalf("wrapper fields should be empty: %+v", rf)
	}
}

func TestResponseFormatMarshalReconstructsWrapper(t *testing.T) {
	strict := true
	rf := ResponseFormat{
		Type:   "json_schema",
		Name:   "Foo",
		Strict: &strict,
		Schema: &Schema{Type: "object", Required: []string{"x"}},
	}
	out, err := json.Marshal(rf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"name":"Foo"`) {
		t.Errorf("marshal missing name: %s", out)
	}
	if !strings.Contains(string(out), `"strict":true`) {
		t.Errorf("marshal missing strict: %s", out)
	}
	if !strings.Contains(string(out), `"type":"object"`) {
		t.Errorf("marshal missing schema body: %s", out)
	}
}

// TestSchemaToGeminiUpperCaseTypeOnMarshal guards against a regression where
// GeminiSchema.Type serialised as Draft-07 lowercase and Gemini returned
// `INVALID_ARGUMENT: Invalid value at 'generationConfig.responseSchema.type'`.
// Matches Gemini REST spec: https://ai.google.dev/api/rest/v1beta/Schema
func TestSchemaToGeminiUpperCaseTypeOnMarshal(t *testing.T) {
	src := `{
		"type":"object",
		"properties":{
			"name":{"type":"string"},
			"age":{"type":"integer","minimum":0},
			"hobbies":{"type":"array","items":{"type":"string"}},
			"preferences":{
				"anyOf":[{"type":"string"},{"type":"number"}]
			}
		},
		"required":["name"]
	}`
	s, err := ParseSchema([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	g, err := s.ToGemini()
	if err != nil {
		t.Fatalf("to gemini: %v", err)
	}
	// Struct fields stay lowercase in-memory.
	if g.Type != "object" {
		t.Fatalf("expected in-memory Type lowercase, got %q", g.Type)
	}
	// Wire shape must be upper-case at every nesting level.
	out, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wire := string(out)
	for _, want := range []string{
		`"type":"OBJECT"`,
		`"type":"STRING"`,
		`"type":"INTEGER"`,
		`"type":"ARRAY"`,
		`"type":"NUMBER"`,
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("missing %s in %s", want, wire)
		}
	}
	for _, forbid := range []string{
		`"type":"object"`,
		`"type":"string"`,
		`"type":"integer"`,
		`"type":"array"`,
		`"type":"number"`,
	} {
		if strings.Contains(wire, forbid) {
			t.Errorf("wire still contains Draft-07 lowercase %s: %s", forbid, wire)
		}
	}
}

// TestGeminiSchemaPassthroughUpperCase ensures that passthrough
// (messages.go `var fallback model.GeminiSchema; json.Unmarshal(...)`)
// also lands in the upper-case wire shape, not just the Schema.ToGemini()
// path. Regression guard for the Draft-07 raw-schema path.
func TestGeminiSchemaPassthroughUpperCase(t *testing.T) {
	raw := []byte(`{"type":"object","properties":{"id":{"type":"string"}}}`)
	var g GeminiSchema
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(&g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wire := string(out)
	if !strings.Contains(wire, `"type":"OBJECT"`) || !strings.Contains(wire, `"type":"STRING"`) {
		t.Errorf("passthrough wire missing uppercase type: %s", wire)
	}
}
