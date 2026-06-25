package openai

import (
	"context"
	"encoding/json"
	"testing"

	inboundOpenai "github.com/bestruirui/octopus/internal/transformer/inbound/openai"
)

// TestIssue88_ThinkingParameterPassthrough tests that the thinking parameter
// from Cherry Studio is correctly passed through to DeepSeek API.
// This is a regression test for GitHub issue #88 where the thinking parameter
// was being silently dropped, causing DeepSeek to use its default (enabled).
func TestIssue88_ThinkingParameterPassthrough(t *testing.T) {
	// Simulate the request Cherry Studio sends when "思维链长度" is set to "关闭"
	clientRequest := `{
		"model": "deepseek-v4-flash",
		"messages": [{"role": "user", "content": "你好"}],
		"thinking": {"type": "disabled"}
	}`

	// Step 1: Inbound transformation (Cherry Studio -> Octopus internal format)
	inbound := &inboundOpenai.ChatInbound{}
	internalReq, err := inbound.TransformRequest(context.Background(), []byte(clientRequest))
	if err != nil {
		t.Fatalf("inbound transformation failed: %v", err)
	}

	// Verify thinking parameter was parsed
	if internalReq.Thinking == nil {
		t.Fatal("thinking parameter was lost during inbound transformation")
	}
	if internalReq.Thinking.Type != "disabled" {
		t.Fatalf("expected thinking.type='disabled', got %q", internalReq.Thinking.Type)
	}

	// Step 2: Outbound transformation (Octopus internal format -> DeepSeek API)
	outbound := &ChatOutbound{}
	httpReq, err := outbound.TransformRequest(context.Background(), internalReq, "https://api.deepseek.com/v1", "test-key")
	if err != nil {
		t.Fatalf("outbound transformation failed: %v", err)
	}

	// Read the request body that would be sent to DeepSeek
	body := make([]byte, 0)
	buf := make([]byte, 1024)
	for {
		n, err := httpReq.Body.Read(buf)
		if n > 0 {
			body = append(body, buf[:n]...)
		}
		if err != nil {
			break
		}
	}

	// Parse the outgoing request
	var deepseekRequest map[string]interface{}
	if err := json.Unmarshal(body, &deepseekRequest); err != nil {
		t.Fatalf("failed to parse outgoing request: %v", err)
	}

	// Verify thinking parameter is present in the request to DeepSeek
	thinkingField, exists := deepseekRequest["thinking"]
	if !exists {
		t.Fatal("thinking parameter was lost during outbound transformation - this causes DeepSeek to default to enabled!")
	}

	thinkingMap, ok := thinkingField.(map[string]interface{})
	if !ok {
		t.Fatalf("thinking field has wrong type: %T", thinkingField)
	}

	typeValue, ok := thinkingMap["type"].(string)
	if !ok {
		t.Fatalf("thinking.type has wrong type: %T", thinkingMap["type"])
	}

	if typeValue != "disabled" {
		t.Fatalf("thinking.type should be 'disabled' but got %q", typeValue)
	}

	t.Log("✓ thinking parameter correctly passed through from Cherry Studio to DeepSeek API")
}
