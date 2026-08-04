package sitesync

import (
	"slices"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestFailureGroupTracksEveryAffectedAccount(t *testing.T) {
	summary := newSiteBatchSummary(SiteBatchPhaseSync, SiteBatchOptions{Trigger: SiteBatchTriggerManual}, 3)
	summary.addFailure(7, model.SitePlatformNewAPI, 101, SiteBatchReasonUnauthorized, "unauthorized")
	summary.addFailure(7, model.SitePlatformNewAPI, 102, SiteBatchReasonUnauthorized, "unauthorized")
	summary.addFailure(7, model.SitePlatformNewAPI, 101, SiteBatchReasonUnauthorized, "unauthorized again")

	groups := sortedSiteBatchGroups(summary.failureGroups)
	if len(groups) != 1 {
		t.Fatalf("got %d failure groups, want 1", len(groups))
	}
	if got := groups[0].AccountIDs; !slices.Equal(got, []int{101, 102}) {
		t.Fatalf("account IDs = %v, want [101 102]", got)
	}
}

func TestSiteBatchReasonClassifiesWrappedDirectProviderHTTPFailures(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    SiteBatchReason
	}{
		{
			name:    "cloudflare challenge",
			message: "default（http 403: Attention Required!）",
			want:    SiteBatchReasonCloudflareProtection,
		},
		{
			name:    "ordinary forbidden",
			message: "default（http 403: access denied）",
			want:    SiteBatchReasonUnauthorized,
		},
		{
			name:    "upstream server failure",
			message: "default（http 500: upstream unavailable）",
			want:    SiteBatchReasonUpstreamHTTPError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newAllGroupsUnresolvedError(tt.message)
			if got := siteBatchReason(err); got != tt.want {
				t.Fatalf("siteBatchReason() = %q, want %q", got, tt.want)
			}
		})
	}
}
