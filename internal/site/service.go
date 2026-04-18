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

func ArchiveSite(ctx context.Context, siteID int) error {
	return sitesync.ArchiveSite(ctx, siteID)
}

func RestoreSite(ctx context.Context, siteID int) error {
	return sitesync.RestoreSite(ctx, siteID)
}

func ListArchivedSites(ctx context.Context) ([]model.Site, error) {
	return sitesync.ListArchivedSites(ctx)
}

func DeleteSiteAccount(ctx context.Context, accountID int) error {
	return sitesync.DeleteSiteAccount(ctx, accountID)
}

func DetectPlatform(ctx context.Context, rawURL string) (model.SitePlatform, error) {
	return sitesync.DetectPlatform(ctx, rawURL)
}

func CreateAccountToken(ctx context.Context, accountID int, req model.SiteChannelKeyCreateRequest) (*model.SiteSyncResult, error) {
	return sitesync.CreateAccountToken(ctx, accountID, req)
}
