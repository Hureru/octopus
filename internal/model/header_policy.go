package model

import (
	"fmt"
	"time"
)

type HeaderPolicyScope string

const (
	HeaderPolicyScopeGlobal         HeaderPolicyScope = "global"
	HeaderPolicyScopeSite           HeaderPolicyScope = "site"
	HeaderPolicyScopeSiteAccount    HeaderPolicyScope = "site_account"
	HeaderPolicyScopeChannel        HeaderPolicyScope = "channel"
	HeaderPolicyScopeCanonicalModel HeaderPolicyScope = "canonical_model"
	HeaderPolicyScopeRouteCandidate HeaderPolicyScope = "route_candidate"
)

func HeaderPolicyDefaultName(scope HeaderPolicyScope, scopeID int) string {
	if scope == HeaderPolicyScopeGlobal {
		return "global"
	}
	return fmt.Sprintf("%s:%d", scope, scopeID)
}

type HeaderPolicy struct {
	ID                   int               `json:"id" gorm:"primaryKey"`
	Name                 string            `json:"name" gorm:"size:191;not null;default:''"`
	Version              int               `json:"version" gorm:"not null;default:1"`
	Scope                HeaderPolicyScope `json:"scope" gorm:"type:varchar(32);not null;uniqueIndex:idx_header_policy_scope"`
	ScopeID              int               `json:"scope_id" gorm:"not null;uniqueIndex:idx_header_policy_scope"`
	Enabled              bool              `json:"enabled" gorm:"not null;default:true"`
	ForwardClientHeaders *bool             `json:"forward_client_headers,omitempty"`
	UserAgent            *string           `json:"user_agent,omitempty" gorm:"type:text"`
	SetHeaders           []CustomHeader    `json:"set_headers" gorm:"serializer:json"`
	UnsetHeaders         []string          `json:"unset_headers" gorm:"serializer:json"`
	AllowedClientHeaders []string          `json:"allowed_client_headers" gorm:"serializer:json"`
	CreatedAt            time.Time         `json:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
}

type UserAgentProfile struct {
	ID        int       `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name" gorm:"size:128;unique;not null"`
	Value     string    `json:"value" gorm:"type:text;not null"`
	BuiltIn   bool      `json:"built_in" gorm:"not null;default:false"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type HeaderPolicyRegistry struct {
	DefaultAllowedClientHeaders []string `json:"default_allowed_client_headers"`
	ProtectedHeaders            []string `json:"protected_headers"`
	ProtectedPrefixes           []string `json:"protected_prefixes"`
}

type HeaderPolicyTrace struct {
	Scope         HeaderPolicyScope `json:"scope"`
	ScopeID       int               `json:"scope_id"`
	PolicyID      int               `json:"policy_id"`
	PolicyName    string            `json:"policy_name"`
	PolicyVersion int               `json:"policy_version"`
	AppliedKeys   []string          `json:"applied_keys"`
	UnsetKeys     []string          `json:"unset_keys"`
}

type ResolvedHeaderPolicy struct {
	ForwardClientHeaders bool                `json:"forward_client_headers"`
	UserAgent            string              `json:"user_agent,omitempty"`
	SetHeaders           []CustomHeader      `json:"set_headers"`
	UnsetHeaders         []string            `json:"unset_headers"`
	AllowedClientHeaders []string            `json:"allowed_client_headers"`
	Trace                []HeaderPolicyTrace `json:"trace"`
}
