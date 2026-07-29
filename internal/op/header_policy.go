package op

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"golang.org/x/net/http/httpguts"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var defaultAllowedClientHeaderPatterns = []string{
	"Accept",
	"Accept-Language",
	"Content-Type",
	"User-Agent",
	"OpenAI-Beta",
	"OpenAI-Organization",
	"OpenAI-Project",
	"Anthropic-Beta",
	"Anthropic-Version",
	"X-Stainless-*",
}

var protectedForwardHeaders = map[string]struct{}{
	"authorization":       {},
	"x-api-key":           {},
	"cookie":              {},
	"set-cookie":          {},
	"host":                {},
	"content-length":      {},
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"x-forwarded-for":     {},
	"x-forwarded-host":    {},
	"x-forwarded-proto":   {},
	"x-forwarded-port":    {},
	"x-real-ip":           {},
	"forwarded":           {},
	"cf-connecting-ip":    {},
	"true-client-ip":      {},
	"x-client-ip":         {},
	"x-cluster-client-ip": {},
}

var protectedForwardHeaderPrefixes = []string{
	"cf-",
	"proxy-",
	"sec-websocket-",
	"x-forwarded-",
}

func HeaderIsProtected(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	if _, ok := protectedForwardHeaders[normalized]; ok {
		return true
	}
	for _, prefix := range protectedForwardHeaderPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}

func HeaderPolicyRegistry() model.HeaderPolicyRegistry {
	protectedHeaders := make([]string, 0, len(protectedForwardHeaders))
	for header := range protectedForwardHeaders {
		protectedHeaders = append(protectedHeaders, http.CanonicalHeaderKey(header))
	}
	sort.Strings(protectedHeaders)
	protectedPrefixes := append([]string(nil), protectedForwardHeaderPrefixes...)
	sort.Strings(protectedPrefixes)
	return model.HeaderPolicyRegistry{
		DefaultAllowedClientHeaders: append([]string(nil), defaultAllowedClientHeaderPatterns...),
		ProtectedHeaders:            protectedHeaders,
		ProtectedPrefixes:           protectedPrefixes,
	}
}

func HeaderPolicyFailureFallback() model.ResolvedHeaderPolicy {
	return model.ResolvedHeaderPolicy{
		ForwardClientHeaders: false,
		SetHeaders:           []model.CustomHeader{},
		UnsetHeaders:         []string{},
		AllowedClientHeaders: []string{},
		Trace:                []model.HeaderPolicyTrace{},
	}
}

func HeaderPolicyList(ctx context.Context) ([]model.HeaderPolicy, error) {
	var items []model.HeaderPolicy
	err := db.GetDB().WithContext(ctx).Order("scope ASC, scope_id ASC").Find(&items).Error
	return items, err
}

func HeaderPolicyUpsert(ctx context.Context, item model.HeaderPolicy) (*model.HeaderPolicy, error) {
	nameProvided := strings.TrimSpace(item.Name) != ""
	if err := validateHeaderPolicy(&item); err != nil {
		return nil, err
	}
	var saved model.HeaderPolicy
	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.HeaderPolicy
		err := findHeaderPolicyForUpsert(tx, item, &existing)
		if err == gorm.ErrRecordNotFound {
			item.ID = 0
			item.Version = 1
			item.CreatedAt = time.Time{}
			item.UpdatedAt = time.Time{}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			saved = item
			return nil
		}
		if err != nil {
			return err
		}
		if !nameProvided && strings.TrimSpace(existing.Name) != "" {
			item.Name = existing.Name
		}
		item.ID = existing.ID
		item.Version = max(existing.Version, 1) + 1
		if err := tx.Model(&existing).
			Select(
				"Name",
				"Version",
				"Scope",
				"ScopeID",
				"Enabled",
				"ForwardClientHeaders",
				"UserAgent",
				"SetHeaders",
				"UnsetHeaders",
				"AllowedClientHeaders",
			).
			Updates(&item).Error; err != nil {
			return err
		}
		return tx.First(&saved, existing.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &saved, nil
}

func findHeaderPolicyForUpsert(tx *gorm.DB, item model.HeaderPolicy, existing *model.HeaderPolicy) error {
	query := tx.Clauses(clause.Locking{Strength: "UPDATE"})
	if item.ID > 0 {
		err := query.First(existing, item.ID).Error
		if err == nil || err != gorm.ErrRecordNotFound {
			return err
		}
	}
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("scope = ? AND scope_id = ?", item.Scope, item.ScopeID).
		First(existing).Error
}

func HeaderPolicyDelete(ctx context.Context, id int) error {
	if id <= 0 {
		return fmt.Errorf("header policy id is required")
	}
	return db.GetDB().WithContext(ctx).Delete(&model.HeaderPolicy{}, id).Error
}

func validateHeaderPolicy(item *model.HeaderPolicy) error {
	if item == nil {
		return fmt.Errorf("header policy is nil")
	}
	switch item.Scope {
	case model.HeaderPolicyScopeGlobal:
		item.ScopeID = 0
	case model.HeaderPolicyScopeSite, model.HeaderPolicyScopeSiteAccount, model.HeaderPolicyScopeChannel,
		model.HeaderPolicyScopeCanonicalModel, model.HeaderPolicyScopeRouteCandidate:
		if item.ScopeID <= 0 {
			return fmt.Errorf("scope_id is required")
		}
	default:
		return fmt.Errorf("invalid header policy scope")
	}
	item.Name = strings.TrimSpace(item.Name)
	if item.Name == "" {
		item.Name = model.HeaderPolicyDefaultName(item.Scope, item.ScopeID)
	}
	if utf8.RuneCountInString(item.Name) > 191 {
		return fmt.Errorf("header policy name is too long")
	}
	if strings.IndexFunc(item.Name, unicode.IsControl) >= 0 {
		return fmt.Errorf("header policy name contains invalid characters")
	}
	if item.Version < 1 {
		item.Version = 1
	}
	if item.UserAgent != nil && !httpguts.ValidHeaderFieldValue(*item.UserAgent) {
		return fmt.Errorf("user-agent contains invalid characters")
	}

	normalizedHeaders := make([]model.CustomHeader, 0, len(item.SetHeaders))
	seenSet := make(map[string]struct{}, len(item.SetHeaders))
	for _, header := range item.SetHeaders {
		rawKey := strings.TrimSpace(header.HeaderKey)
		if rawKey == "" {
			continue
		}
		if !httpguts.ValidHeaderFieldName(rawKey) {
			return fmt.Errorf("header name %q is invalid", rawKey)
		}
		key := http.CanonicalHeaderKey(rawKey)
		if HeaderIsProtected(key) {
			return fmt.Errorf("header %s is protected", key)
		}
		if !httpguts.ValidHeaderFieldValue(header.HeaderValue) {
			return fmt.Errorf("header %s contains invalid characters", key)
		}
		lower := strings.ToLower(key)
		if _, ok := seenSet[lower]; ok {
			continue
		}
		seenSet[lower] = struct{}{}
		normalizedHeaders = append(normalizedHeaders, model.CustomHeader{HeaderKey: key, HeaderValue: header.HeaderValue})
	}
	item.SetHeaders = normalizedHeaders
	var err error
	item.UnsetHeaders, err = normalizeHeaderNames(item.UnsetHeaders, true)
	if err != nil {
		return err
	}
	item.AllowedClientHeaders, err = normalizeAllowedHeaderPatterns(item.AllowedClientHeaders)
	if err != nil {
		return err
	}
	return nil
}

func normalizeHeaderNames(values []string, allowProtected bool) ([]string, error) {
	if values == nil {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		rawKey := strings.TrimSpace(value)
		if rawKey == "" {
			continue
		}
		if !httpguts.ValidHeaderFieldName(rawKey) {
			return nil, fmt.Errorf("header name %q is invalid", rawKey)
		}
		key := http.CanonicalHeaderKey(rawKey)
		if !allowProtected && HeaderIsProtected(key) {
			continue
		}
		lower := strings.ToLower(key)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		result = append(result, key)
	}
	sort.Strings(result)
	return result, nil
}

func normalizeAllowedHeaderPatterns(values []string) ([]string, error) {
	if values == nil {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		pattern := strings.TrimSpace(value)
		if pattern == "" {
			continue
		}
		lower := strings.ToLower(pattern)
		switch {
		case lower == "*":
			pattern = "*"
		case strings.HasSuffix(lower, "*"):
			if strings.Count(lower, "*") != 1 {
				return nil, fmt.Errorf("header pattern %q is invalid", value)
			}
			prefix := strings.TrimSuffix(lower, "*")
			if prefix == "" || !httpguts.ValidHeaderFieldName(prefix+"x") {
				return nil, fmt.Errorf("header pattern %q is invalid", value)
			}
			pattern = prefix + "*"
		default:
			if !httpguts.ValidHeaderFieldName(pattern) {
				return nil, fmt.Errorf("header name %q is invalid", value)
			}
			pattern = http.CanonicalHeaderKey(pattern)
		}
		key := strings.ToLower(pattern)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, pattern)
	}
	sort.Strings(result)
	return result, nil
}

func ResolveHeaderPolicy(
	ctx context.Context,
	channelID int,
	canonicalModelID int,
	routeCandidateID int,
) (model.ResolvedHeaderPolicy, error) {
	result := model.ResolvedHeaderPolicy{
		ForwardClientHeaders: true,
		SetHeaders:           []model.CustomHeader{},
		UnsetHeaders:         []string{},
		AllowedClientHeaders: append([]string(nil), defaultAllowedClientHeaderPatterns...),
		Trace:                []model.HeaderPolicyTrace{},
	}
	scopes := []struct {
		scope model.HeaderPolicyScope
		id    int
	}{{scope: model.HeaderPolicyScopeGlobal, id: 0}}

	if channelID > 0 {
		var binding model.SiteChannelBinding
		if err := db.GetDB().WithContext(ctx).Where("channel_id = ?", channelID).First(&binding).Error; err == nil {
			scopes = append(scopes,
				struct {
					scope model.HeaderPolicyScope
					id    int
				}{scope: model.HeaderPolicyScopeSite, id: binding.SiteID},
				struct {
					scope model.HeaderPolicyScope
					id    int
				}{scope: model.HeaderPolicyScopeSiteAccount, id: binding.SiteAccountID},
			)
		} else if err != gorm.ErrRecordNotFound {
			return HeaderPolicyFailureFallback(), err
		}
		scopes = append(scopes, struct {
			scope model.HeaderPolicyScope
			id    int
		}{scope: model.HeaderPolicyScopeChannel, id: channelID})
	}
	if canonicalModelID > 0 {
		scopes = append(scopes, struct {
			scope model.HeaderPolicyScope
			id    int
		}{scope: model.HeaderPolicyScopeCanonicalModel, id: canonicalModelID})
	}
	if routeCandidateID > 0 {
		scopes = append(scopes, struct {
			scope model.HeaderPolicyScope
			id    int
		}{scope: model.HeaderPolicyScopeRouteCandidate, id: routeCandidateID})
	}

	for _, scope := range scopes {
		var policy model.HeaderPolicy
		err := db.GetDB().WithContext(ctx).
			Where("scope = ? AND scope_id = ? AND enabled = ?", scope.scope, scope.id, true).
			First(&policy).Error
		if err == gorm.ErrRecordNotFound {
			continue
		}
		if err != nil {
			return HeaderPolicyFailureFallback(), err
		}
		applyHeaderPolicyLayer(&result, policy)
	}
	return result, nil
}

func ResolveSiteHeaderPolicy(ctx context.Context, siteID, accountID int) (model.ResolvedHeaderPolicy, error) {
	result := model.ResolvedHeaderPolicy{
		ForwardClientHeaders: true,
		SetHeaders:           []model.CustomHeader{},
		UnsetHeaders:         []string{},
		AllowedClientHeaders: append([]string(nil), defaultAllowedClientHeaderPatterns...),
		Trace:                []model.HeaderPolicyTrace{},
	}
	scopes := []struct {
		scope model.HeaderPolicyScope
		id    int
	}{
		{scope: model.HeaderPolicyScopeGlobal, id: 0},
		{scope: model.HeaderPolicyScopeSite, id: siteID},
	}
	if accountID > 0 {
		scopes = append(scopes, struct {
			scope model.HeaderPolicyScope
			id    int
		}{scope: model.HeaderPolicyScopeSiteAccount, id: accountID})
	}
	for _, scope := range scopes {
		var policy model.HeaderPolicy
		err := db.GetDB().WithContext(ctx).
			Where("scope = ? AND scope_id = ? AND enabled = ?", scope.scope, scope.id, true).
			First(&policy).Error
		if err == gorm.ErrRecordNotFound {
			continue
		}
		if err != nil {
			return HeaderPolicyFailureFallback(), err
		}
		applyHeaderPolicyLayer(&result, policy)
	}
	return result, nil
}

func applyHeaderPolicyLayer(result *model.ResolvedHeaderPolicy, policy model.HeaderPolicy) {
	if result == nil {
		return
	}
	trace := model.HeaderPolicyTrace{
		Scope:         policy.Scope,
		ScopeID:       policy.ScopeID,
		PolicyID:      policy.ID,
		PolicyName:    policy.Name,
		PolicyVersion: max(policy.Version, 1),
	}
	if policy.ForwardClientHeaders != nil {
		result.ForwardClientHeaders = *policy.ForwardClientHeaders
	}
	if policy.UserAgent != nil {
		result.UserAgent = *policy.UserAgent
		trace.AppliedKeys = append(trace.AppliedKeys, "User-Agent")
	}
	if policy.AllowedClientHeaders != nil {
		result.AllowedClientHeaders = append([]string{}, policy.AllowedClientHeaders...)
	}

	setByLower := make(map[string]model.CustomHeader, len(result.SetHeaders)+len(policy.SetHeaders))
	order := make([]string, 0, len(result.SetHeaders)+len(policy.SetHeaders))
	for _, header := range result.SetHeaders {
		key := strings.ToLower(header.HeaderKey)
		if _, ok := setByLower[key]; !ok {
			order = append(order, key)
		}
		setByLower[key] = header
	}
	for _, header := range policy.SetHeaders {
		key := strings.ToLower(header.HeaderKey)
		if _, ok := setByLower[key]; !ok {
			order = append(order, key)
		}
		setByLower[key] = header
		trace.AppliedKeys = append(trace.AppliedKeys, header.HeaderKey)
	}
	unset := make(map[string]string, len(result.UnsetHeaders)+len(policy.UnsetHeaders))
	for _, key := range result.UnsetHeaders {
		unset[strings.ToLower(key)] = key
	}
	for _, key := range policy.UnsetHeaders {
		lower := strings.ToLower(key)
		unset[lower] = key
		delete(setByLower, lower)
		trace.UnsetKeys = append(trace.UnsetKeys, key)
	}
	for _, header := range policy.SetHeaders {
		delete(unset, strings.ToLower(header.HeaderKey))
	}

	result.SetHeaders = result.SetHeaders[:0]
	for _, key := range order {
		if header, ok := setByLower[key]; ok {
			result.SetHeaders = append(result.SetHeaders, header)
		}
	}
	result.UnsetHeaders = result.UnsetHeaders[:0]
	for _, key := range unset {
		result.UnsetHeaders = append(result.UnsetHeaders, key)
	}
	sort.Strings(result.UnsetHeaders)
	result.Trace = append(result.Trace, trace)
}

func ApplyHeaderPolicy(outboundHeader, clientHeader http.Header, legacy []model.CustomHeader, policy model.ResolvedHeaderPolicy) {
	if outboundHeader == nil {
		return
	}
	allowedPatterns := policy.AllowedClientHeaders
	if allowedPatterns == nil {
		allowedPatterns = defaultAllowedClientHeaderPatterns
	}
	if policy.ForwardClientHeaders {
		for key, values := range clientHeader {
			lower := strings.ToLower(key)
			if HeaderIsProtected(lower) ||
				!httpguts.ValidHeaderFieldName(key) ||
				!headerValuesValid(values) ||
				!headerMatchesAllowedPatterns(lower, allowedPatterns) {
				continue
			}
			if lower == "anthropic-beta" {
				existing := outboundHeader.Get(key)
				for _, value := range values {
					existing = mergeCommaHeader(existing, value)
				}
				if existing != "" {
					outboundHeader.Set(key, existing)
				}
				continue
			}
			outboundHeader.Del(key)
			for _, value := range values {
				outboundHeader.Add(key, value)
			}
		}
	}
	for _, header := range legacy {
		key := strings.TrimSpace(header.HeaderKey)
		if key == "" ||
			HeaderIsProtected(key) ||
			!httpguts.ValidHeaderFieldName(key) ||
			!httpguts.ValidHeaderFieldValue(header.HeaderValue) {
			continue
		}
		outboundHeader.Set(key, header.HeaderValue)
	}
	for _, key := range policy.UnsetHeaders {
		if httpguts.ValidHeaderFieldName(key) {
			outboundHeader.Del(key)
		}
	}
	for _, header := range policy.SetHeaders {
		if !HeaderIsProtected(header.HeaderKey) &&
			httpguts.ValidHeaderFieldName(header.HeaderKey) &&
			httpguts.ValidHeaderFieldValue(header.HeaderValue) {
			outboundHeader.Set(header.HeaderKey, header.HeaderValue)
		}
	}
	if policy.UserAgent != "" && httpguts.ValidHeaderFieldValue(policy.UserAgent) {
		outboundHeader.Set("User-Agent", policy.UserAgent)
	}
}

func headerMatchesAllowedPatterns(headerName string, patterns []string) bool {
	headerName = strings.ToLower(strings.TrimSpace(headerName))
	for _, pattern := range patterns {
		normalized := strings.ToLower(strings.TrimSpace(pattern))
		switch {
		case normalized == "*":
			return true
		case strings.HasSuffix(normalized, "*"):
			if strings.HasPrefix(headerName, strings.TrimSuffix(normalized, "*")) {
				return true
			}
		case headerName == normalized:
			return true
		}
	}
	return false
}

func headerValuesValid(values []string) bool {
	for _, value := range values {
		if !httpguts.ValidHeaderFieldValue(value) {
			return false
		}
	}
	return true
}

func mergeCommaHeader(existing, incoming string) string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, source := range []string{existing, incoming} {
		for _, item := range strings.Split(source, ",") {
			value := strings.TrimSpace(item)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return strings.Join(result, ",")
}

func UserAgentProfileList(ctx context.Context) ([]model.UserAgentProfile, error) {
	if err := ensureBuiltInUserAgentProfiles(ctx); err != nil {
		return nil, err
	}
	var items []model.UserAgentProfile
	err := db.GetDB().WithContext(ctx).Order("built_in DESC, name ASC").Find(&items).Error
	return items, err
}

func UserAgentProfileUpsert(ctx context.Context, item model.UserAgentProfile) (*model.UserAgentProfile, error) {
	item.Name = strings.TrimSpace(item.Name)
	item.Value = strings.TrimSpace(item.Value)
	if item.Name == "" || item.Value == "" {
		return nil, fmt.Errorf("profile name and value are required")
	}
	if err := db.GetDB().WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&item).Error; err != nil {
		return nil, err
	}
	var saved model.UserAgentProfile
	if err := db.GetDB().WithContext(ctx).First(&saved, "name = ?", item.Name).Error; err != nil {
		return nil, err
	}
	return &saved, nil
}

func ensureBuiltInUserAgentProfiles(ctx context.Context) error {
	items := []model.UserAgentProfile{
		{Name: "Codex CLI", Value: "codex_cli_rs/0.0.0 (Octopus relay)", BuiltIn: true},
		{Name: "OpenAI SDK", Value: "OpenAI/Python Octopus", BuiltIn: true},
		{Name: "Browser", Value: "Mozilla/5.0 Octopus", BuiltIn: true},
	}
	for i := range items {
		if err := db.GetDB().WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&items[i]).Error; err != nil {
			return err
		}
	}
	return nil
}
