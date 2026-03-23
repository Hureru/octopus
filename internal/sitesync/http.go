package sitesync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/bestruirui/octopus/internal/client"
	"github.com/bestruirui/octopus/internal/model"
)

func siteHTTPClient(siteRecord *model.Site) (*http.Client, error) {
	if siteRecord == nil {
		return nil, fmt.Errorf("site is nil")
	}
	if !siteRecord.Proxy {
		return client.GetHTTPClientSystemProxy(false)
	}
	if siteRecord.SiteProxy == nil || strings.TrimSpace(*siteRecord.SiteProxy) == "" {
		return client.GetHTTPClientSystemProxy(true)
	}
	return client.GetHTTPClientCustomProxy(strings.TrimSpace(*siteRecord.SiteProxy))
}

func requestJSON(ctx context.Context, siteRecord *model.Site, method string, requestURL string, body any, headers map[string]string) (map[string]any, error) {
	httpClient, err := siteHTTPClient(siteRecord)
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if body != nil {
		payload, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return nil, marshalErr
		}
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for _, item := range siteRecord.CustomHeader {
		if strings.TrimSpace(item.HeaderKey) != "" {
			req.Header.Set(strings.TrimSpace(item.HeaderKey), item.HeaderValue)
		}
	}
	for key, value := range headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}
	if len(bodyBytes) == 0 {
		return map[string]any{}, nil
	}

	var payload map[string]any
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return nil, fmt.Errorf("decode response failed: %w", err)
	}
	return payload, nil
}

func buildSiteURL(baseURL string, path string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return baseURL + path
}

func parseTokenItems(payload map[string]any) []map[string]any {
	for _, candidate := range []any{payload["data"], nestedValue(payload, "data", "items"), nestedValue(payload, "data", "data"), payload["items"], payload["list"], nestedValue(payload, "data", "list")} {
		if items := normalizeItemSlice(candidate); len(items) > 0 {
			return items
		}
	}
	return nil
}

func parseGroupItems(payload map[string]any) []model.SiteUserGroup {
	items := make([]model.SiteUserGroup, 0)
	for _, candidate := range []any{payload["data"], nestedValue(payload, "data", "groups"), payload["groups"], payload} {
		switch value := candidate.(type) {
		case map[string]any:
			for key := range value {
				lowered := strings.ToLower(strings.TrimSpace(key))
				if lowered == "" || lowered == "success" || lowered == "message" || lowered == "data" || lowered == "code" || lowered == "error" {
					continue
				}
				items = append(items, model.SiteUserGroup{GroupKey: key, Name: key})
			}
		case []any:
			for _, raw := range value {
				switch item := raw.(type) {
				case string:
					if strings.TrimSpace(item) != "" {
						items = append(items, model.SiteUserGroup{GroupKey: strings.TrimSpace(item), Name: strings.TrimSpace(item)})
					}
				case map[string]any:
					groupKey := firstNonEmptyString(jsonString(item["group_id"]), jsonString(item["groupId"]), jsonString(item["id"]), jsonString(item["value"]), jsonString(item["name"]), jsonString(item["group_name"]), jsonString(item["groupName"]), jsonString(item["title"]))
					groupName := firstNonEmptyString(jsonString(item["name"]), jsonString(item["group_name"]), jsonString(item["groupName"]), jsonString(item["title"]), groupKey)
					if strings.TrimSpace(groupKey) != "" {
						items = append(items, model.SiteUserGroup{GroupKey: strings.TrimSpace(groupKey), Name: strings.TrimSpace(groupName)})
					}
				}
			}
		}
		if len(items) > 0 {
			break
		}
	}
	deduped := make(map[string]model.SiteUserGroup)
	for _, item := range items {
		key := model.NormalizeSiteGroupKey(item.GroupKey)
		item.GroupKey = key
		item.Name = model.NormalizeSiteGroupName(key, item.Name)
		deduped[key] = item
	}
	keys := make([]string, 0, len(deduped))
	for key := range deduped {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]model.SiteUserGroup, 0, len(keys))
	for _, key := range keys {
		result = append(result, deduped[key])
	}
	return result
}

func normalizeItemSlice(value any) []map[string]any {
	rawItems, ok := value.([]any)
	if !ok {
		return nil
	}
	items := make([]map[string]any, 0, len(rawItems))
	for _, raw := range rawItems {
		if item, ok := raw.(map[string]any); ok {
			items = append(items, item)
		}
	}
	return items
}

func normalizeModelNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	result := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	slices.Sort(result)
	return result
}

func parseEnabledFlag(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return int(typed) != 0
	case int:
		return typed != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "", "enabled", "active", "1", "true", "on":
			return true
		case "disabled", "inactive", "0", "false", "off":
			return false
		default:
			return true
		}
	default:
		return true
	}
}

func ensureBearer(token string) string {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return token
	}
	return "Bearer " + token
}

func jsonString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strings.TrimSpace(fmt.Sprintf("%.0f", typed))
	case int:
		return strings.TrimSpace(fmt.Sprintf("%d", typed))
	default:
		return ""
	}
}

func jsonBool(value any) bool {
	typed, ok := value.(bool)
	if ok {
		return typed
	}
	return false
}

func nestedValue(payload map[string]any, keys ...string) any {
	var current any = payload
	for _, key := range keys {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = obj[key]
	}
	return current
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func marshalRawPayload(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(payload)
}
