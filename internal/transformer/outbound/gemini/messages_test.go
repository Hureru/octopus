package gemini

import "testing"

func TestCleanGeminiSchemaRemovesPropertyNamesRecursively(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"propertyNames": map[string]any{
			"type": "string",
		},
		"properties": map[string]any{
			"payload": map[string]any{
				"type": "object",
				"propertyNames": map[string]any{
					"pattern": "^[a-z]+$",
				},
			},
		},
	}

	cleanGeminiSchema(schema)

	if _, ok := schema["propertyNames"]; ok {
		t.Fatalf("expected top-level propertyNames to be removed")
	}
	props := schema["properties"].(map[string]any)
	payload := props["payload"].(map[string]any)
	if _, ok := payload["propertyNames"]; ok {
		t.Fatalf("expected nested propertyNames to be removed")
	}
}
