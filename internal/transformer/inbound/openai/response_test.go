package openai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
)

func TestConvertToInternalRequestPreservesRawInputItems(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-4o",
		Input: ResponsesInput{Items: []ResponsesItem{
			{Type: "input_text", Text: stringPtr("hello")},
		}},
	}

	internalReq, err := convertToInternalRequest(req)
	if err != nil {
		t.Fatalf("convertToInternalRequest failed: %v", err)
	}
	if len(internalReq.RawInputItems) == 0 {
		t.Fatalf("expected raw input items to be preserved")
	}

	var items []map[string]any
	if err := json.Unmarshal(internalReq.RawInputItems, &items); err != nil {
		t.Fatalf("unmarshal raw input items failed: %v", err)
	}
	if len(items) != 1 || items[0]["type"] != "input_text" {
		t.Fatalf("expected original raw input items to be kept, got %#v", items)
	}
	if internalReq.TransformOptions.ArrayInputs == nil || !*internalReq.TransformOptions.ArrayInputs {
		t.Fatalf("expected array input flag to stay true")
	}
}

func stringPtr(value string) *string {
	return &value
}

func TestResponseInboundEmitsCompletedBeforeDoneWhenUsageMissing(t *testing.T) {
	inbound := &ResponseInbound{}
	ctx := context.Background()

	if _, err := inbound.TransformStream(ctx, &model.InternalLLMResponse{
		ID:      "chatcmpl_1",
		Object:  "chat.completion.chunk",
		Created: 1,
		Model:   "gpt-4o",
		Choices: []model.Choice{{
			Index: 0,
			Delta: &model.Message{
				Role: "assistant",
				Content: model.MessageContent{
					Content: stringPtr("hello"),
				},
			},
		}},
	}); err != nil {
		t.Fatalf("TransformStream content chunk failed: %v", err)
	}

	if _, err := inbound.TransformStream(ctx, &model.InternalLLMResponse{
		ID:     "chatcmpl_1",
		Object: "chat.completion.chunk",
		Model:  "gpt-4o",
		Choices: []model.Choice{{
			Index:        0,
			FinishReason: stringPtr("stop"),
		}},
	}); err != nil {
		t.Fatalf("TransformStream finish chunk failed: %v", err)
	}

	doneChunk, err := inbound.TransformStream(ctx, &model.InternalLLMResponse{Object: "[DONE]"})
	if err != nil {
		t.Fatalf("TransformStream done chunk failed: %v", err)
	}
	if !strings.Contains(string(doneChunk), `"type":"response.completed"`) {
		t.Fatalf("expected done chunk to emit response.completed, got %s", string(doneChunk))
	}
	if !strings.Contains(string(doneChunk), "data: [DONE]") {
		t.Fatalf("expected done chunk to keep [DONE], got %s", string(doneChunk))
	}
}

func TestResponseInboundEmitsIncompleteTerminalEventWithoutDone(t *testing.T) {
	inbound := &ResponseInbound{}

	chunk, err := inbound.TransformStream(context.Background(), &model.InternalLLMResponse{
		ID:     "chatcmpl_2",
		Object: "chat.completion.chunk",
		Model:  "gpt-4o",
		Choices: []model.Choice{{
			Index:        0,
			FinishReason: stringPtr("length"),
		}},
	})
	if err != nil {
		t.Fatalf("TransformStream incomplete chunk failed: %v", err)
	}
	if !strings.Contains(string(chunk), `"type":"response.incomplete"`) {
		t.Fatalf("expected incomplete chunk to emit response.incomplete, got %s", string(chunk))
	}
}

func TestResponseOutboundPreservesIncompleteTerminalStatus(t *testing.T) {
	outbound := &openaiOutbound.ResponseOutbound{}
	stream, err := outbound.TransformStream(context.Background(), []byte(`{"type":"response.incomplete","response":{"id":"resp_1","model":"gpt-4o","status":"incomplete"}}`))
	if err != nil {
		t.Fatalf("TransformStream failed: %v", err)
	}
	if stream == nil || len(stream.Choices) == 0 || stream.Choices[0].FinishReason == nil {
		t.Fatalf("expected finish reason to be set, got %#v", stream)
	}
	if got := *stream.Choices[0].FinishReason; got != "length" {
		t.Fatalf("expected response.incomplete to map to length, got %q", got)
	}
}
