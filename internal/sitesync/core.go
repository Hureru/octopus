package sitesync

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

type syncSnapshot struct {
	accessToken string
	groups      []model.SiteUserGroup
	tokens      []model.SiteToken
	models      []model.SiteModel
	balance     float64
	balanceUsed float64
	message     string
}

func SyncAccount(ctx context.Context, accountID int) (*model.SiteSyncResult, error) {
	siteRecord, account, err := loadSiteAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}

	snapshot, err := syncAccountState(ctx, siteRecord, account)
	if err != nil {
		updateErr := updateAccountSyncState(ctx, account.ID, model.SiteExecutionStatusFailed, err.Error(), "")
		if updateErr != nil {
			log.Warnf("failed to update site account sync state (account=%d): %v", account.ID, updateErr)
		}
		return nil, err
	}

	if err := persistSyncSnapshot(ctx, account.ID, snapshot); err != nil {
		return nil, err
	}

	channelIDs, err := ProjectAccount(ctx, account.ID)
	if err != nil {
		return nil, err
	}

	modelNames := make([]string, 0, len(snapshot.models))
	for _, item := range snapshot.models {
		modelNames = append(modelNames, item.ModelName)
	}
	slices.Sort(modelNames)

	return &model.SiteSyncResult{
		AccountID:       account.ID,
		SiteID:          siteRecord.ID,
		ChannelCount:    len(channelIDs),
		GroupCount:      len(snapshot.groups),
		TokenCount:      len(snapshot.tokens),
		ModelCount:      len(snapshot.models),
		ManagedChannels: channelIDs,
		Models:          modelNames,
		Message:         snapshot.message,
	}, nil
}

func CheckinAccount(ctx context.Context, accountID int) (*model.SiteCheckinResult, error) {
	siteRecord, account, err := loadSiteAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}

	result, resolvedAccessToken, err := checkinAccountState(ctx, siteRecord, account)
	if err != nil {
		status := model.SiteExecutionStatusFailed
		lowered := strings.ToLower(err.Error())
		if strings.Contains(lowered, "not supported") || strings.Contains(lowered, "not found") {
			status = model.SiteExecutionStatusSkipped
		}
		updateErr := updateAccountCheckinState(ctx, account, status, err.Error(), false, resolvedAccessToken)
		if updateErr != nil {
			return nil, updateErr
		}
		return &model.SiteCheckinResult{AccountID: account.ID, SiteID: siteRecord.ID, Status: status, Message: err.Error()}, nil
	}

	result.AccountID = account.ID
	result.SiteID = siteRecord.ID
	if err := updateAccountCheckinState(ctx, account, result.Status, result.Message, result.Status == model.SiteExecutionStatusSuccess, resolvedAccessToken); err != nil {
		return nil, err
	}
	return result, nil
}

func SyncAll(ctx context.Context) {
	sites, err := op.SiteList(ctx)
	if err != nil {
		log.Warnf("failed to list sites for sync: %v", err)
		return
	}
	for _, siteRecord := range sites {
		if !siteRecord.Enabled {
			continue
		}
		for _, account := range siteRecord.Accounts {
			if !account.Enabled || !account.AutoSync {
				continue
			}
			if _, err := SyncAccount(ctx, account.ID); err != nil {
				log.Warnf("site account sync failed (account=%d): %v", account.ID, err)
			}
		}
	}
}

func CheckinAll(ctx context.Context) {
	sites, err := op.SiteList(ctx)
	if err != nil {
		log.Warnf("failed to list sites for checkin: %v", err)
		return
	}
	now := time.Now()
	for _, siteRecord := range sites {
		if !siteRecord.Enabled {
			continue
		}
		for index := range siteRecord.Accounts {
			account := &siteRecord.Accounts[index]
			if !account.Enabled || !account.AutoCheckin {
				continue
			}
			if account.RandomCheckin {
				nextAt, scheduleErr := ensureRandomCheckinSchedule(ctx, account, now)
				if scheduleErr != nil {
					log.Warnf("failed to ensure site account checkin schedule (account=%d): %v", account.ID, scheduleErr)
					continue
				}
				if nextAt != nil && now.Before(*nextAt) {
					continue
				}
			}
			if _, err := CheckinAccount(ctx, account.ID); err != nil {
				log.Warnf("site account checkin failed (account=%d): %v", account.ID, err)
			}
		}
	}
}

func DeleteSite(ctx context.Context, siteID int) error {
	siteRecord, err := op.SiteGet(siteID, ctx)
	if err != nil {
		return err
	}
	for _, account := range siteRecord.Accounts {
		if err := deleteManagedChannelsByAccount(ctx, account.ID); err != nil {
			return err
		}
	}
	return op.SiteDel(siteID, ctx)
}

func DeleteSiteAccount(ctx context.Context, accountID int) error {
	if err := deleteManagedChannelsByAccount(ctx, accountID); err != nil {
		return err
	}
	return op.SiteAccountDel(accountID, ctx)
}
