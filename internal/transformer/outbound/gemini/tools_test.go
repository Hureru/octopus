package gemini

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestConvertToolsGoogleSearch(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "gemini-2.5-pro",
		Tools: []model.Tool{
			{Type: "server_search"},
		},
	}
	out := convertLLMToGeminiRequest(req)
	if len(out.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d: %+v", len(out.Tools), out.Tools)
	}
	if out.Tools[0].GoogleSearch == nil {
		t.Fatalf("expected GoogleSearch populated, got %+v", out.Tools[0])
	}
}

func TestConvertToolsUrlContext(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "gemini-2.5-pro",
		Tools: []model.Tool{
			{Type: "url_context"},
		},
	}
	out := convertLLMToGeminiRequest(req)
	if len(out.Tools) != 1 || out.Tools[0].UrlContext == nil {
		t.Fatalf("expected UrlContext populated, got %+v", out.Tools)
	}
}

func TestConvertToolsCodeExecution(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "gemini-2.5-pro",
		Tools: []model.Tool{
			{Type: "code_execution"},
		},
	}
	out := convertLLMToGeminiRequest(req)
	if len(out.Tools) != 1 || out.Tools[0].CodeExecution == nil {
		t.Fatalf("expected CodeExecution populated, got %+v", out.Tools)
	}
}

func TestConvertToolsMixedFunctionAndServerTool(t *testing.T) {
	parameters := json.RawMessage(`{"type":"object"}`)
	req := &model.InternalLLMRequest{
		Model: "gemini-2.5-pro",
		Tools: []model.Tool{
			{Type: "function", Function: model.Function{Name: "lookup", Parameters: parameters}},
			{Type: "server_search"},
		},
	}
	out := convertLLMToGeminiRequest(req)
	if len(out.Tools) != 2 {
		t.Fatalf("expected 2 tool entries (functionDeclarations + googleSearch), got %d: %+v", len(out.Tools), out.Tools)
	}
	hasFunc := false
	hasSearch := false
	for _, g := range out.Tools {
		if len(g.FunctionDeclarations) > 0 {
			hasFunc = true
		}
		if g.GoogleSearch != nil {
			hasSearch = true
		}
	}
	if !hasFunc || !hasSearch {
		t.Errorf("expected both function and googleSearch tools, got %+v", out.Tools)
	}
}

func TestConvertToolsGoogleSearchWireShape(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "gemini-2.5-pro",
		Tools: []model.Tool{{Type: "server_search"}},
	}
	out := convertLLMToGeminiRequest(req)
	b, err := json.Marshal(out.Tools[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"googleSearch":{}`) {
		t.Errorf("googleSearch not emitted as empty object: %s", b)
	}
}

func TestConvertToolsUnknownDropped(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "gemini-2.5-pro",
		Tools: []model.Tool{{Type: "nonsense_tool"}},
	}
	out := convertLLMToGeminiRequest(req)
	if len(out.Tools) != 0 {
		t.Fatalf("expected unknown tool to be dropped, got %+v", out.Tools)
	}
}
