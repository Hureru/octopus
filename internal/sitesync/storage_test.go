package sitesync

import (
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

func TestSiteMaskedTokenMatchesIgnoresOptionalSKPrefix(t *testing.T) {
	tests := []struct {
		name      string
		fullToken string
		masked    string
	}{
		{name: "full has sk prefix", fullToken: "sk-yzFyREALREALOTkb", masked: "yzFy**********OTkb"},
		{name: "masked has sk prefix", fullToken: "yzFyREALREALOTkb", masked: "sk-yzFy**********OTkb"},
		{name: "both have sk prefix", fullToken: "sk-yzFyREALREALOTkb", masked: "sk-yzFy**********OTkb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !siteMaskedTokenMatches(tt.fullToken, tt.masked) {
				t.Fatalf("expected %q to match %q", tt.fullToken, tt.masked)
			}
		})
	}
}

func TestMergePersistedSiteTokensPreservesManualFullTokenWhenIncomingIsMasked(t *testing.T) {
	now := time.Unix(1711929600, 0)
	existing := []model.SiteToken{{
		ID:            41,
		SiteAccountID: 9,
		Name:          "primary",
		Token:         "sk-yzFyREALREALOTkb",
		GroupKey:      model.SiteDefaultGroupKey,
		GroupName:     model.SiteDefaultGroupName,
		Enabled:       true,
		ValueStatus:   model.SiteTokenValueStatusReady,
		Source:        "manual",
	}}
	incoming := []model.SiteToken{{
		Name:        "primary",
		Token:       "yzFy**********OTkb",
		GroupKey:    model.SiteDefaultGroupKey,
		GroupName:   model.SiteDefaultGroupName,
		Enabled:     true,
		ValueStatus: model.SiteTokenValueStatusMaskedPending,
		Source:      "sync",
	}}

	merged := mergePersistedSiteTokens(9, existing, incoming, now)
	if len(merged) != 1 {
		t.Fatalf("expected exactly one merged token, got %+v", merged)
	}
	if merged[0].Token != "sk-yzFyREALREALOTkb" {
		t.Fatalf("expected merged token to keep full manual value, got %q", merged[0].Token)
	}
	if merged[0].ValueStatus != model.SiteTokenValueStatusReady {
		t.Fatalf("expected merged token to remain ready, got %q", merged[0].ValueStatus)
	}
	if !merged[0].Enabled {
		t.Fatalf("expected merged token to remain enabled")
	}
}

func TestMergePersistedSiteTokensTreatsOptionalSKPrefixAsSameReadyToken(t *testing.T) {
	now := time.Unix(1711929600, 0)
	existing := []model.SiteToken{{
		ID:            7,
		SiteAccountID: 9,
		Name:          "primary",
		Token:         "sk-abc123",
		GroupKey:      model.SiteDefaultGroupKey,
		GroupName:     model.SiteDefaultGroupName,
		Enabled:       true,
		ValueStatus:   model.SiteTokenValueStatusReady,
		Source:        "manual",
	}}
	incoming := []model.SiteToken{{
		Name:      "primary",
		Token:     "abc123",
		GroupKey:  model.SiteDefaultGroupKey,
		GroupName: model.SiteDefaultGroupName,
		Enabled:   true,
		Source:    "sync",
	}}

	merged := mergePersistedSiteTokens(9, existing, incoming, now)
	if len(merged) != 1 {
		t.Fatalf("expected exactly one merged token, got %+v", merged)
	}
	if merged[0].Token != "sk-abc123" {
		t.Fatalf("expected merged token to preserve stored full token format, got %q", merged[0].Token)
	}
	if merged[0].ValueStatus != model.SiteTokenValueStatusReady {
		t.Fatalf("expected merged token to remain ready, got %q", merged[0].ValueStatus)
	}
}

func TestMergePersistedSiteTokensKeepsMaskedPendingWhenMatchIsAmbiguous(t *testing.T) {
	now := time.Unix(1711929600, 0)
	existing := []model.SiteToken{
		{
			ID:            1,
			SiteAccountID: 9,
			Name:          "alpha",
			Token:         "sk-yzFyONEOTkb",
			GroupKey:      model.SiteDefaultGroupKey,
			GroupName:     model.SiteDefaultGroupName,
			Enabled:       true,
			ValueStatus:   model.SiteTokenValueStatusReady,
			Source:        "manual",
		},
		{
			ID:            2,
			SiteAccountID: 9,
			Name:          "beta",
			Token:         "sk-yzFyTWOOTkb",
			GroupKey:      model.SiteDefaultGroupKey,
			GroupName:     model.SiteDefaultGroupName,
			Enabled:       true,
			ValueStatus:   model.SiteTokenValueStatusReady,
			Source:        "manual",
		},
	}
	incoming := []model.SiteToken{{
		Name:        "",
		Token:       "yzFy**********OTkb",
		GroupKey:    model.SiteDefaultGroupKey,
		GroupName:   model.SiteDefaultGroupName,
		Enabled:     true,
		ValueStatus: model.SiteTokenValueStatusMaskedPending,
		Source:      "sync",
	}}

	merged := mergePersistedSiteTokens(9, existing, incoming, now)
	if len(merged) != 3 {
		t.Fatalf("expected masked pending token plus two preserved manual tokens, got %+v", merged)
	}
	maskedCount := 0
	for _, item := range merged {
		if item.Token == "yzFy**********OTkb" {
			maskedCount++
			if item.ValueStatus != model.SiteTokenValueStatusMaskedPending {
				t.Fatalf("expected ambiguous incoming token to remain masked_pending, got %+v", item)
			}
			if item.Enabled {
				t.Fatalf("expected ambiguous masked_pending token to stay disabled")
			}
		}
	}
	if maskedCount != 1 {
		t.Fatalf("expected exactly one preserved masked_pending token, got %+v", merged)
	}
}

func TestMergePersistedSiteTokensDoesNotOverwriteReadyTokenOnNameOnlyMaskedFallback(t *testing.T) {
	now := time.Unix(1711929600, 0)
	existing := []model.SiteToken{{
		ID:            5,
		SiteAccountID: 9,
		Name:          "primary",
		Token:         "sk-different-full-token",
		GroupKey:      model.SiteDefaultGroupKey,
		GroupName:     model.SiteDefaultGroupName,
		Enabled:       true,
		ValueStatus:   model.SiteTokenValueStatusReady,
		Source:        "manual",
	}}
	incoming := []model.SiteToken{{
		Name:        "primary",
		Token:       "yzFy**********OTkb",
		GroupKey:    model.SiteDefaultGroupKey,
		GroupName:   model.SiteDefaultGroupName,
		Enabled:     true,
		ValueStatus: model.SiteTokenValueStatusMaskedPending,
		Source:      "sync",
	}}

	merged := mergePersistedSiteTokens(9, existing, incoming, now)
	if len(merged) != 1 {
		t.Fatalf("expected exactly one merged token, got %+v", merged)
	}
	if merged[0].Token != "sk-different-full-token" {
		t.Fatalf("expected ready token to be preserved on name-only fallback, got %q", merged[0].Token)
	}
	if merged[0].ValueStatus != model.SiteTokenValueStatusReady {
		t.Fatalf("expected ready token to stay ready, got %q", merged[0].ValueStatus)
	}
}
