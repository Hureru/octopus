package site

import (
	"context"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/sitesync"
)

func SyncAccount(ctx context.Context, accountID int) (*model.SiteSyncResult, error) {
	return sitesync.SyncAccount(ctx, accountID)
}

func CheckinAccount(ctx context.Context, accountID int) (*model.SiteCheckinResult, error) {
	return sitesync.CheckinAccount(ctx, accountID)
}

func ProjectAccount(ctx context.Context, accountID int) ([]int, error) {
	return sitesync.ProjectAccount(ctx, accountID)
}

func ProjectSite(ctx context.Context, siteID int) error {
	return sitesync.ProjectSite(ctx, siteID)
}

func SyncAll(ctx context.Context) {
	sitesync.SyncAll(ctx)
}

func CheckinAll(ctx context.Context) {
	sitesync.CheckinAll(ctx)
}

func RefreshAccountRandomCheckinSchedule(ctx context.Context, accountID int) error {
	return sitesync.RefreshAccountRandomCheckinSchedule(ctx, accountID)
}

func DeleteSite(ctx context.Context, siteID int) error {
	return sitesync.DeleteSite(ctx, siteID)
}

func DeleteSiteAccount(ctx context.Context, accountID int) error {
	return sitesync.DeleteSiteAccount(ctx, accountID)
}
