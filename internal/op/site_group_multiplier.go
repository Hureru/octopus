package op

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
)

var storedGroupMultiplierFields = []string{
	"rate_multiplier",
	"group_multiplier",
	"multiplier",
	"ratio",
	"rate",
}

var storedGroupIdentityFields = []string{
	"group_key",
	"groupKey",
	"key",
	"group_id",
	"groupId",
	"id",
}

var storedGroupPayloadWrappers = []string{
	"data",
	"items",
	"groups",
	"list",
	"records",
	"result",
}

func storedSiteGroupMultiplier(rawPayload, groupKey string) (float64, bool) {
	target := model.NormalizeSiteGroupKey(groupKey)
	if strings.TrimSpace(rawPayload) == "" || target == "" {
		return 0, false
	}
	decoder := json.NewDecoder(strings.NewReader(rawPayload))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return 0, false
	}
	return findStoredSiteGroupMultiplier(payload, target)
}

func findStoredSiteGroupMultiplier(value any, target string) (float64, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if sameStoredSiteGroupKey(key, target) {
				if multiplier, ok := storedSiteGroupMultiplierValue(item); ok {
					return multiplier, true
				}
			}
		}
		if storedSiteGroupItemMatches(typed, target) {
			if multiplier, ok := storedSiteGroupMultiplierValue(typed); ok {
				return multiplier, true
			}
		}
		for _, key := range storedGroupPayloadWrappers {
			if item, ok := typed[key]; ok {
				if multiplier, found := findStoredSiteGroupMultiplier(item, target); found {
					return multiplier, true
				}
			}
		}
	case []any:
		for _, item := range typed {
			if multiplier, ok := findStoredSiteGroupMultiplier(item, target); ok {
				return multiplier, true
			}
		}
	}
	return 0, false
}

func storedSiteGroupItemMatches(item map[string]any, target string) bool {
	for _, field := range storedGroupIdentityFields {
		if value, ok := item[field]; ok && sameStoredSiteGroupKey(storedSiteGroupScalar(value), target) {
			return true
		}
	}
	return false
}

func storedSiteGroupMultiplierValue(value any) (float64, bool) {
	if item, ok := value.(map[string]any); ok {
		for _, field := range storedGroupMultiplierFields {
			if multiplier, valid := storedSiteGroupNumber(item[field]); valid {
				return multiplier, true
			}
		}
		return 0, false
	}
	return storedSiteGroupNumber(value)
}

func storedSiteGroupNumber(value any) (float64, bool) {
	var parsed float64
	var err error
	switch typed := value.(type) {
	case json.Number:
		parsed, err = typed.Float64()
	case float64:
		parsed = typed
	case string:
		parsed, err = strconv.ParseFloat(strings.TrimSpace(typed), 64)
	default:
		return 0, false
	}
	if err != nil || parsed < 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, false
	}
	return parsed, true
}

func storedSiteGroupScalar(value any) string {
	switch typed := value.(type) {
	case json.Number:
		return typed.String()
	case string:
		return typed
	default:
		return ""
	}
}

func sameStoredSiteGroupKey(value, target string) bool {
	return strings.EqualFold(
		model.NormalizeSiteGroupKey(value),
		model.NormalizeSiteGroupKey(target),
	)
}
