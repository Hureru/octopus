package model

import (
	"encoding/json"
	"sort"
	"strings"
)

const (
	SiteModelRouteMetadataKind    = "site_route_metadata"
	SiteModelRouteMetadataVersion = 1
)

type SiteModelRouteMetadata struct {
	Kind                    string             `json:"kind"`
	Version                 int                `json:"version"`
	Source                  string             `json:"source,omitempty"`
	RouteSupported          bool               `json:"route_supported"`
	RouteType               SiteModelRouteType `json:"route_type,omitempty"`
	EnableGroups            []string           `json:"enable_groups,omitempty"`
	SupportedEndpointTypes  []string           `json:"supported_endpoint_types,omitempty"`
	HeuristicEndpointTypes  []string           `json:"heuristic_endpoint_types,omitempty"`
	NormalizedEndpointTypes []string           `json:"normalized_endpoint_types,omitempty"`
	UnsupportedReason       string             `json:"unsupported_reason,omitempty"`
}

func (m SiteModelRouteMetadata) Marshal() string {
	m.Kind = SiteModelRouteMetadataKind
	m.Version = SiteModelRouteMetadataVersion
	m.Source = strings.TrimSpace(m.Source)
	m.EnableGroups = NormalizeSiteModelRouteMetadataGroupKeys(m.EnableGroups)
	m.SupportedEndpointTypes = normalizeRouteMetadataStrings(m.SupportedEndpointTypes)
	m.HeuristicEndpointTypes = normalizeRouteMetadataStrings(m.HeuristicEndpointTypes)
	m.NormalizedEndpointTypes = normalizeRouteMetadataStrings(m.NormalizedEndpointTypes)
	if m.RouteSupported {
		m.RouteType = NormalizeSiteModelRouteType(m.RouteType)
		if !IsProjectedSiteModelRouteType(m.RouteType) {
			m.RouteSupported = false
			m.RouteType = SiteModelRouteTypeUnknown
		}
	} else {
		m.RouteType = SiteModelRouteTypeUnknown
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(payload)
}

func ParseSiteModelRouteMetadata(raw string) (*SiteModelRouteMetadata, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return nil, false
	}

	var metadata SiteModelRouteMetadata
	if err := json.Unmarshal([]byte(trimmed), &metadata); err != nil {
		return nil, false
	}
	if metadata.Kind != SiteModelRouteMetadataKind || metadata.Version != SiteModelRouteMetadataVersion {
		return nil, false
	}

	metadata.Source = strings.TrimSpace(metadata.Source)
	metadata.EnableGroups = NormalizeSiteModelRouteMetadataGroupKeys(metadata.EnableGroups)
	metadata.SupportedEndpointTypes = normalizeRouteMetadataStrings(metadata.SupportedEndpointTypes)
	metadata.HeuristicEndpointTypes = normalizeRouteMetadataStrings(metadata.HeuristicEndpointTypes)
	metadata.NormalizedEndpointTypes = normalizeRouteMetadataStrings(metadata.NormalizedEndpointTypes)
	if metadata.RouteSupported {
		metadata.RouteType = NormalizeSiteModelRouteType(metadata.RouteType)
		if !IsProjectedSiteModelRouteType(metadata.RouteType) {
			return nil, false
		}
	} else {
		metadata.RouteType = SiteModelRouteTypeUnknown
	}
	return &metadata, true
}

func NormalizeSiteModelRouteMetadataGroupKeys(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]string, len(values))
	for _, value := range values {
		key := NormalizeSiteGroupKey(value)
		if key == "" {
			continue
		}
		lookupKey := strings.ToLower(key)
		if _, ok := seen[lookupKey]; ok {
			continue
		}
		seen[lookupKey] = key
	}
	if len(seen) == 0 {
		return nil
	}
	result := make([]string, 0, len(seen))
	for _, value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizeRouteMetadataStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]string, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = trimmed
	}
	if len(seen) == 0 {
		return nil
	}
	result := make([]string, 0, len(seen))
	for _, value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
