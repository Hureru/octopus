package helper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ApplyParamOverride merges a JSON-object override into an outbound JSON request body.
// Empty overrides, nil bodies, and non-object request bodies are ignored.
func ApplyParamOverride(request *http.Request, paramOverride *string) error {
	if request == nil || request.Body == nil || paramOverride == nil || strings.TrimSpace(*paramOverride) == "" {
		return nil
	}

	body, err := io.ReadAll(request.Body)
	if err != nil {
		return fmt.Errorf("failed to read request body: %w", err)
	}
	// 合并失败时也要把读走的请求体放回去
	merged, mergeErr := MergeParamOverride(body, paramOverride)
	if mergeErr != nil {
		merged = body
	}
	request.Body = io.NopCloser(bytes.NewReader(merged))
	request.ContentLength = int64(len(merged))
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(merged)), nil
	}
	return mergeErr
}

// MergeParamOverride merges a JSON-object override into a JSON request body: an override key replaces
// the body's key. The other values are not decoded, so they keep their exact value (big integers too).
// Empty overrides, and bodies or overrides that are not JSON objects, leave the body unchanged.
func MergeParamOverride(body []byte, paramOverride *string) ([]byte, error) {
	if paramOverride == nil || strings.TrimSpace(*paramOverride) == "" {
		return body, nil
	}
	var bodyMap, override map[string]json.RawMessage
	if json.Unmarshal(body, &bodyMap) != nil || json.Unmarshal([]byte(*paramOverride), &override) != nil || bodyMap == nil {
		return body, nil
	}
	for key, value := range override {
		bodyMap[key] = value
	}
	merged, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body with param override: %w", err)
	}
	return merged, nil
}
