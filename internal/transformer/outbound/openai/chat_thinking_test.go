package openai

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestChatOutbound_ThinkingParameter(t *testing.T) {
	tests := []struct {
		name            string
		thinking        *model.ThinkingConfig
		expectInPayload bool
		expectedType    string
	}{
		{
			name:            "thinking disabled",
			thinking:        &model.ThinkingConfig{Type: "disabled"},
			expectInPayload: true,
			expectedType:    "disabled",
		},
		{
			name:            "thinking enabled",
			thinking:        &model.ThinkingConfig{Type: "enabled"},
			expectInPayload: true,
			expectedType:    "enabled",
		},
		{
			name:            "thinking not set",
			thinking:        nil,
			expectInPayload: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := "Hello"
			req := &model.InternalLLMRequest{
				Model: "deepseek-v4-flash",
				Messages: []model.Message{
					{
						Role: "user",
						Content: model.MessageContent{
							Content: &content,
						},
					},
				},
				Thinking: tt.thinking,
			}

			outbound := &ChatOutbound{}
			httpReq, err := outbound.TransformRequest(context.Background(), req, "https://api.deepseek.com/v1", "test-key")
			if err != nil {
				t.Fatalf("TransformRequest failed: %v", err)
			}

			body, err := io.ReadAll(httpReq.Body)
			if err != nil {
				t.Fatalf("failed to read request body: %v", err)
			}

			var payload map[string]interface{}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("failed to unmarshal payload: %v", err)
			}

			thinkingField, exists := payload["thinking"]
			if tt.expectInPayload {
				if !exists {
					t.Fatalf("expected 'thinking' field in payload, but it was missing")
				}
				thinkingMap, ok := thinkingField.(map[string]interface{})
				if !ok {
					t.Fatalf("expected 'thinking' to be an object, got %T", thinkingField)
				}
				typeValue, ok := thinkingMap["type"].(string)
				if !ok {
					t.Fatalf("expected 'thinking.type' to be a string, got %T", thinkingMap["type"])
				}
				if typeValue != tt.expectedType {
					t.Fatalf("expected thinking.type=%q, got %q", tt.expectedType, typeValue)
				}
			} else {
				if exists {
					t.Fatalf("expected 'thinking' field to be omitted, but got: %v", thinkingField)
				}
			}
		})
	}
}

func TestChatOutbound_ThinkingWithReasoningEffort(t *testing.T) {
	// Test that both thinking and reasoning_effort can coexist
	content := "Hello"
	req := &model.InternalLLMRequest{
		Model: "deepseek-v4-flash",
		Messages: []model.Message{
			{
				Role: "user",
				Content: model.MessageContent{
					Content: &content,
				},
			},
		},
		Thinking:        &model.ThinkingConfig{Type: "disabled"},
		ReasoningEffort: "high",
	}

	outbound := &ChatOutbound{}
	httpReq, err := outbound.TransformRequest(context.Background(), req, "https://api.deepseek.com/v1", "test-key")
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	body, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatalf("failed to read request body: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	// Both fields should be present
	if _, exists := payload["thinking"]; !exists {
		t.Fatalf("expected 'thinking' field in payload")
	}
	if _, exists := payload["reasoning_effort"]; !exists {
		t.Fatalf("expected 'reasoning_effort' field in payload")
	}
}
