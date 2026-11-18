package helper

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/dlclark/regexp2"
)

const modelFetchUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36"

// A "/v<digit>" path segment; the leading [^/] keeps a "//v1.example.com" host from matching.
var apiVersionSegment = regexp.MustCompile(`(?i)[^/]/v\d`)

// modelFetcher lists the models served at one endpoint, speaking one upstream protocol.
type modelFetcher func(client *http.Client, ctx context.Context, request model.Channel, endpoint string) ([]string, error)

func FetchModels(ctx context.Context, request model.Channel) ([]string, error) {
	client, err := ChannelHTTPClientWithContext(ctx, &request)
	if err != nil {
		return nil, err
	}
	// Relay stations often list models only in the OpenAI format, so native protocols fall back to it.
	fetchers := []modelFetcher{fetchOpenAIModels}
	switch request.Type {
	case outbound.OutboundTypeAnthropic:
		fetchers = []modelFetcher{fetchAnthropicModels, fetchOpenAIModels}
	case outbound.OutboundTypeGemini:
		fetchers = []modelFetcher{fetchGeminiModels, fetchOpenAIModels}
	}
	fetchModel, err := probeModelEndpoints(client, ctx, request, fetchers)
	if err != nil {
		return nil, err
	}
	if request.MatchRegex != nil && *request.MatchRegex != "" {
		matchModel := make([]string, 0)
		re, err := regexp2.Compile(*request.MatchRegex, regexp2.ECMAScript)
		if err != nil {
			return nil, err
		}
		for _, model := range fetchModel {
			matched, err := re.MatchString(model)
			if err != nil {
				return nil, err
			}
			if matched {
				matchModel = append(matchModel, model)
			}
		}
		return matchModel, nil
	}
	return fetchModel, nil
}

// probeModelEndpoints returns the first non-empty model list. It asks the configured base URL
// first and, when that carries no API version (e.g. a site root), guesses the common prefixes.
func probeModelEndpoints(client *http.Client, ctx context.Context, request model.Channel, fetchers []modelFetcher) ([]string, error) {
	baseURL := strings.TrimRight(request.GetBaseUrl(), "/")
	endpoints := []string{baseURL + "/models"}
	if !apiVersionSegment.MatchString(baseURL) {
		endpoints = append(endpoints, baseURL+"/v1/models", baseURL+"/api/v1/models", baseURL+"/v1beta/models")
	}
	var firstErr error
	for _, fetch := range fetchers {
		for i, endpoint := range endpoints {
			models, err := fetch(client, ctx, request, endpoint)
			if len(models) > 0 {
				return models, nil
			}
			if errors.As(err, new(*url.Error)) {
				// No HTTP response at all: other paths on the same host will not fare better.
				return nil, cmp.Or(firstErr, err)
			}
			if i == 0 && firstErr == nil {
				// Only the configured endpoint reports errors: a guessed path failing says nothing about the channel.
				firstErr = err
			}
		}
	}
	// A guess or fallback counts only when it returns models. Turning the configured endpoint's
	// error into an empty success would make auto-sync delete every model of the channel.
	return []string{}, firstErr
}

// refer: https://platform.openai.com/docs/api-reference/models/list
func fetchOpenAIModels(client *http.Client, ctx context.Context, request model.Channel, endpoint string) ([]string, error) {
	req, _ := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		endpoint,
		nil,
	)
	applyDefaultModelRequestHeaders(req, request)
	req.Header.Set("Authorization", "Bearer "+request.GetChannelKey().ChannelKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result model.OpenAIModelList
	if err := decodeModelJSONResponse(resp, &result); err != nil {
		return nil, err
	}

	models := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, m.ID)
	}
	return models, nil
}

// refer: https://ai.google.dev/api/models
func fetchGeminiModels(client *http.Client, ctx context.Context, request model.Channel, endpoint string) ([]string, error) {
	var allModels []string
	pageToken := ""

	for {
		req, _ := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			endpoint,
			nil,
		)
		applyDefaultModelRequestHeaders(req, request)
		req.Header.Set("X-Goog-Api-Key", request.GetChannelKey().ChannelKey)
		if pageToken != "" {
			q := req.URL.Query()
			q.Add("pageToken", pageToken)
			req.URL.RawQuery = q.Encode()
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		var result model.GeminiModelList
		if err := decodeModelJSONResponse(resp, &result); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		for _, m := range result.Models {
			name := strings.TrimPrefix(m.Name, "models/")
			allModels = append(allModels, name)
		}

		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}
	return allModels, nil
}

// refer: https://platform.claude.com/docs
func fetchAnthropicModels(client *http.Client, ctx context.Context, request model.Channel, endpoint string) ([]string, error) {
	var allModels []string
	var afterID string
	for {
		req, _ := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			endpoint,
			nil,
		)
		applyDefaultModelRequestHeaders(req, request)
		req.Header.Set("X-Api-Key", request.GetChannelKey().ChannelKey)
		req.Header.Set("Anthropic-Version", "2023-06-01")
		q := req.URL.Query()
		if afterID != "" {
			q.Set("after_id", afterID)
		}
		req.URL.RawQuery = q.Encode()

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		var result model.AnthropicModelList
		if err := decodeModelJSONResponse(resp, &result); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		for _, m := range result.Data {
			allModels = append(allModels, m.ID)
		}

		if !result.HasMore {
			break
		}

		afterID = result.LastID
	}
	return allModels, nil
}

func applyDefaultModelRequestHeaders(req *http.Request, request model.Channel) {
	if req == nil {
		return
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", modelFetchUserAgent)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json, text/plain, */*")
	}
	if req.Header.Get("Accept-Language") == "" {
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	}
	for _, header := range request.CustomHeader {
		// 这里没有客户端请求，引用 {client_header:x} 的头直接跳过，不把占位符原文发给上游。
		if value, ok := header.ResolveValue(nil); ok && header.HeaderKey != "" {
			req.Header.Set(header.HeaderKey, value)
		}
	}
}

func decodeModelJSONResponse(resp *http.Response, result any) error {
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return formatModelHTTPError(resp.StatusCode, resp.Header.Get("Content-Type"), bodyBytes)
	}
	if len(bodyBytes) == 0 {
		return nil
	}
	if err := json.Unmarshal(bodyBytes, result); err != nil {
		if summary := extractModelHTMLResponseSummary(resp.Header.Get("Content-Type"), bodyBytes); summary != "" {
			return fmt.Errorf("decode response failed: %s", summary)
		}
		return fmt.Errorf("decode response failed: %w", err)
	}
	return nil
}

func formatModelHTTPError(statusCode int, contentType string, bodyBytes []byte) error {
	if payload, ok := parseModelErrorPayload(bodyBytes); ok {
		if message := extractModelErrorMessage(payload); message != "" {
			return fmt.Errorf("http %d: %s", statusCode, message)
		}
	}
	if summary := extractModelHTMLResponseSummary(contentType, bodyBytes); summary != "" {
		return fmt.Errorf("http %d: %s", statusCode, summary)
	}
	return fmt.Errorf("http %d: %s", statusCode, strings.TrimSpace(string(bodyBytes)))
}

func parseModelErrorPayload(bodyBytes []byte) (map[string]any, bool) {
	if len(bodyBytes) == 0 {
		return map[string]any{}, true
	}
	var payload map[string]any
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func extractModelErrorMessage(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if message, ok := payload["message"].(string); ok && strings.TrimSpace(message) != "" {
		return strings.TrimSpace(message)
	}
	if message, ok := payload["msg"].(string); ok && strings.TrimSpace(message) != "" {
		return strings.TrimSpace(message)
	}
	if errorPayload, ok := payload["error"].(map[string]any); ok {
		if message, ok := errorPayload["message"].(string); ok && strings.TrimSpace(message) != "" {
			return strings.TrimSpace(message)
		}
	}
	return ""
}

func extractModelHTMLResponseSummary(contentType string, bodyBytes []byte) string {
	body := strings.TrimSpace(string(bodyBytes))
	if body == "" {
		return ""
	}
	lowered := strings.ToLower(body)
	loweredContentType := strings.ToLower(contentType)
	if !strings.Contains(loweredContentType, "text/html") && !strings.Contains(lowered, "<html") && !strings.Contains(lowered, "<!doctype") {
		if strings.Contains(lowered, "just a moment") {
			return "Just a moment..."
		}
		return ""
	}
	if start := strings.Index(lowered, "<title>"); start >= 0 {
		start += len("<title>")
		if end := strings.Index(lowered[start:], "</title>"); end >= 0 {
			title := strings.TrimSpace(body[start : start+end])
			if pipe := strings.Index(title, "|"); pipe >= 0 {
				title = strings.TrimSpace(title[:pipe])
			}
			if title != "" {
				return title
			}
		}
	}
	if strings.Contains(lowered, "just a moment") {
		return "Just a moment..."
	}
	if strings.Contains(lowered, "cloudflare tunnel error") {
		return "Cloudflare Tunnel error"
	}
	if strings.Contains(lowered, "cloudflare") {
		return "Cloudflare challenge"
	}
	return ""
}
