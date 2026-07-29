package openai

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestEmbeddingTransformRequestRawPreservesUnknownFieldsAndRewritesModel(t *testing.T) {
	outbound := &EmbeddingOutbound{}
	request, err := outbound.TransformRequestRaw(
		context.Background(),
		[]byte(`{"model":"client-model","input":"hello","encoding_format":"float","vendor_extension":{"enabled":true}}`),
		"upstream-model",
		"https://api.example.test/v1",
		"secret",
		nil,
	)
	if err != nil {
		t.Fatalf("TransformRequestRaw failed: %v", err)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if payload["model"] != "upstream-model" {
		t.Fatalf("model = %#v", payload["model"])
	}
	if _, ok := payload["vendor_extension"].(map[string]any); !ok {
		t.Fatalf("unknown extension was not preserved: %#v", payload)
	}
	if !outbound.CanPassthrough(model.APIFormatOpenAIEmbedding) {
		t.Fatal("embedding outbound should support same-format passthrough")
	}
}
