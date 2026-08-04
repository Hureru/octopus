package op

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestStoredSiteGroupMultiplier(t *testing.T) {
	tests := []struct {
		name       string
		rawPayload string
		groupKey   string
		want       float64
		wantOK     bool
	}{
		{
			name:       "new api map ratio",
			rawPayload: `{"data":{"claude_code":{"name":"Claude Code","ratio":5},"complimentary":{"ratio":0}}}`,
			groupKey:   "claude_code",
			want:       5,
			wantOK:     true,
		},
		{
			name:       "explicit zero ratio",
			rawPayload: `{"data":{"claude_code":{"ratio":5},"complimentary":{"ratio":0}}}`,
			groupKey:   "complimentary",
			want:       0,
			wantOK:     true,
		},
		{
			name:       "sub2 list multiplier",
			rawPayload: `{"data":[{"id":21,"rate_multiplier":"0.06"}]}`,
			groupKey:   "21",
			want:       0.06,
			wantOK:     true,
		},
		{
			name:       "non numeric ratio remains unknown",
			rawPayload: `{"data":{"automatic":{"ratio":"auto"}}}`,
			groupKey:   "automatic",
			wantOK:     false,
		},
		{
			name:       "different group is not reused",
			rawPayload: `{"data":{"default":{"ratio":2}}}`,
			groupKey:   "claude_code",
			wantOK:     false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := storedSiteGroupMultiplier(test.rawPayload, test.groupKey)
			if ok != test.wantOK || got != test.want {
				t.Fatalf("storedSiteGroupMultiplier() = (%v, %v), want (%v, %v)", got, ok, test.want, test.wantOK)
			}
		})
	}
}

func TestPersistedSiteGroupMultiplierMapFallsBackToRawPayload(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	site := model.Site{
		Name: "raw-group-multiplier-site", Platform: model.SitePlatformNewAPI,
		BaseURL: "https://raw-group-multiplier.example.com", Enabled: true,
	}
	mustCreatePricingRow(t, ctx, &site)
	account := model.SiteAccount{
		SiteID: site.ID, Name: "raw-group-multiplier-account",
		CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token", Enabled: true,
	}
	mustCreatePricingRow(t, ctx, &account)
	group := model.SiteUserGroup{
		SiteAccountID: account.ID,
		GroupKey:      "complimentary",
		Name:          "Complimentary",
		RawPayload:    `{"data":{"claude_code":{"ratio":5},"complimentary":{"ratio":0}}}`,
	}
	mustCreatePricingRow(t, ctx, &group)

	values := persistedSiteGroupMultiplierMap(ctx, []int{account.ID})
	key := siteAccountGroupKey(account.ID, group.GroupKey)
	if multiplier, ok := values[key]; !ok || multiplier != 0 {
		t.Fatalf("raw payload zero multiplier missing: %+v", values)
	}
}
