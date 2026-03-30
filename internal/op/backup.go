package op

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const dbDumpVersion = 1

func DBExportAll(ctx context.Context, includeLogs, includeStats bool) (*model.DBDump, error) {
	conn := db.GetDB().WithContext(ctx)

	d := &model.DBDump{
		Version:      dbDumpVersion,
		ExportedAt:   time.Now().UTC(),
		IncludeLogs:  includeLogs,
		IncludeStats: includeStats,
	}

	if err := conn.Find(&d.Channels).Error; err != nil {
		return nil, fmt.Errorf("export channels: %w", err)
	}
	if err := conn.Find(&d.ChannelKeys).Error; err != nil {
		return nil, fmt.Errorf("export channel_keys: %w", err)
	}
	if err := conn.Find(&d.Sites).Error; err != nil {
		return nil, fmt.Errorf("export sites: %w", err)
	}
	if err := conn.Find(&d.SiteAccounts).Error; err != nil {
		return nil, fmt.Errorf("export site_accounts: %w", err)
	}
	if err := conn.Find(&d.SiteTokens).Error; err != nil {
		return nil, fmt.Errorf("export site_tokens: %w", err)
	}
	if err := conn.Find(&d.SiteUserGroups).Error; err != nil {
		return nil, fmt.Errorf("export site_user_groups: %w", err)
	}
	if err := conn.Find(&d.SiteModels).Error; err != nil {
		return nil, fmt.Errorf("export site_models: %w", err)
	}
	if err := conn.Find(&d.SiteChannelBindings).Error; err != nil {
		return nil, fmt.Errorf("export site_channel_bindings: %w", err)
	}
	if err := conn.Find(&d.Groups).Error; err != nil {
		return nil, fmt.Errorf("export groups: %w", err)
	}
	if err := conn.Find(&d.GroupItems).Error; err != nil {
		return nil, fmt.Errorf("export group_items: %w", err)
	}
	if err := conn.Find(&d.LLMInfos).Error; err != nil {
		return nil, fmt.Errorf("export llm_infos: %w", err)
	}
	if err := conn.Find(&d.APIKeys).Error; err != nil {
		return nil, fmt.Errorf("export api_keys: %w", err)
	}
	if err := conn.Find(&d.Settings).Error; err != nil {
		return nil, fmt.Errorf("export settings: %w", err)
	}

	if includeStats {
		if err := conn.Find(&d.StatsTotal).Error; err != nil {
			return nil, fmt.Errorf("export stats_total: %w", err)
		}
		if err := conn.Find(&d.StatsDaily).Error; err != nil {
			return nil, fmt.Errorf("export stats_daily: %w", err)
		}
		if err := conn.Find(&d.StatsHourly).Error; err != nil {
			return nil, fmt.Errorf("export stats_hourly: %w", err)
		}
		if err := conn.Find(&d.StatsModel).Error; err != nil {
			return nil, fmt.Errorf("export stats_model: %w", err)
		}
		if err := conn.Find(&d.StatsChannel).Error; err != nil {
			return nil, fmt.Errorf("export stats_channel: %w", err)
		}
		if err := conn.Find(&d.StatsAPIKey).Error; err != nil {
			return nil, fmt.Errorf("export stats_api_key: %w", err)
		}
	}

	if includeLogs {
		if err := conn.Find(&d.RelayLogs).Error; err != nil {
			return nil, fmt.Errorf("export relay_logs: %w", err)
		}
	}

	return d, nil
}

func DBImportIncremental(ctx context.Context, dump *model.DBDump) (*model.DBImportResult, error) {
	if dump == nil {
		return nil, fmt.Errorf("empty dump")
	}

	if dump.Version != 0 && dump.Version != dbDumpVersion {
		return nil, fmt.Errorf("unsupported dump version: %d", dump.Version)
	}

	conn := db.GetDB().WithContext(ctx)
	res := &model.DBImportResult{RowsAffected: map[string]int64{}}

	err := conn.Transaction(func(tx *gorm.DB) error {
		channelIDMap := make(map[int]int)
		siteIDMap := make(map[int]int)
		accountIDMap := make(map[int]int)
		userGroupIDMap := make(map[int]int)
		groupIDMap := make(map[int]int)
		apiKeyIDMap := make(map[int]int)

		// 1. Channels (dedup by name)
		for i := range dump.Channels {
			ch := dump.Channels[i]
			oldID := ch.ID
			ch.ID = 0
			ch.Keys = nil
			ch.Stats = nil

			var existing model.Channel
			if err := tx.Where("name = ?", ch.Name).First(&existing).Error; err == nil {
				channelIDMap[oldID] = existing.ID
				continue
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("import channels: %w", err)
			}
			if err := tx.Omit("Keys", "Stats").Create(&ch).Error; err != nil {
				return fmt.Errorf("import channels: %w", err)
			}
			channelIDMap[oldID] = ch.ID
			res.RowsAffected["channels"]++
		}

		// 2. ChannelKeys (remap channel_id, dedup by channel_id+channel_key)
		for i := range dump.ChannelKeys {
			key := dump.ChannelKeys[i]
			key.ID = 0
			if newID, ok := channelIDMap[key.ChannelID]; ok {
				key.ChannelID = newID
			}
			var existing model.ChannelKey
			if err := tx.Where("channel_id = ? AND channel_key = ?", key.ChannelID, key.ChannelKey).First(&existing).Error; err == nil {
				continue
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("import channel_keys: %w", err)
			}
			if err := tx.Create(&key).Error; err != nil {
				return fmt.Errorf("import channel_keys: %w", err)
			}
			res.RowsAffected["channel_keys"]++
		}

		// 3. Sites (dedup by platform+base_url)
		for i := range dump.Sites {
			site := dump.Sites[i]
			oldID := site.ID
			site.ID = 0
			site.Accounts = nil

			normalizedURL := normalizeImportBaseURL(site.BaseURL)
			if normalizedURL != "" {
				site.BaseURL = normalizedURL
			}

			var existing model.Site
			if err := tx.Where("platform = ? AND base_url = ?", site.Platform, site.BaseURL).First(&existing).Error; err == nil {
				siteIDMap[oldID] = existing.ID
				continue
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("import sites: %w", err)
			}
			site.Name = uniqueSiteName(tx, site.Name)
			if err := tx.Omit("Accounts").Create(&site).Error; err != nil {
				return fmt.Errorf("import sites: %w", err)
			}
			siteIDMap[oldID] = site.ID
			res.RowsAffected["sites"]++
		}

		// 4. SiteAccounts (remap site_id, dedup by site_id+name)
		for i := range dump.SiteAccounts {
			account := dump.SiteAccounts[i]
			oldID := account.ID
			account.ID = 0
			account.Tokens = nil
			account.UserGroups = nil
			account.Models = nil
			account.ChannelBindings = nil

			if newSiteID, ok := siteIDMap[account.SiteID]; ok {
				account.SiteID = newSiteID
			}

			var existing model.SiteAccount
			if err := tx.Where("site_id = ? AND name = ?", account.SiteID, strings.TrimSpace(account.Name)).First(&existing).Error; err == nil {
				accountIDMap[oldID] = existing.ID
				continue
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("import site_accounts: %w", err)
			}
			if err := tx.Omit("Tokens", "UserGroups", "Models", "ChannelBindings").Create(&account).Error; err != nil {
				return fmt.Errorf("import site_accounts: %w", err)
			}
			accountIDMap[oldID] = account.ID
			res.RowsAffected["site_accounts"]++
		}

		// 5. SiteTokens (remap site_account_id, dedup by site_account_id+token+group_key)
		for i := range dump.SiteTokens {
			token := dump.SiteTokens[i]
			token.ID = 0
			if newID, ok := accountIDMap[token.SiteAccountID]; ok {
				token.SiteAccountID = newID
			}
			var existing model.SiteToken
			if err := tx.Where("site_account_id = ? AND token = ? AND group_key = ?", token.SiteAccountID, token.Token, token.GroupKey).First(&existing).Error; err == nil {
				continue
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("import site_tokens: %w", err)
			}
			if err := tx.Create(&token).Error; err != nil {
				return fmt.Errorf("import site_tokens: %w", err)
			}
			res.RowsAffected["site_tokens"]++
		}

		// 6. SiteUserGroups (remap site_account_id, dedup by uniqueIndex)
		for i := range dump.SiteUserGroups {
			group := dump.SiteUserGroups[i]
			oldID := group.ID
			group.ID = 0
			if newID, ok := accountIDMap[group.SiteAccountID]; ok {
				group.SiteAccountID = newID
			}
			var existing model.SiteUserGroup
			if err := tx.Where("site_account_id = ? AND group_key = ?", group.SiteAccountID, group.GroupKey).First(&existing).Error; err == nil {
				userGroupIDMap[oldID] = existing.ID
				continue
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("import site_user_groups: %w", err)
			}
			if err := tx.Create(&group).Error; err != nil {
				return fmt.Errorf("import site_user_groups: %w", err)
			}
			userGroupIDMap[oldID] = group.ID
			res.RowsAffected["site_user_groups"]++
		}

		// 7. SiteModels (remap site_account_id, dedup by uniqueIndex)
		for i := range dump.SiteModels {
			m := dump.SiteModels[i]
			m.ID = 0
			if newID, ok := accountIDMap[m.SiteAccountID]; ok {
				m.SiteAccountID = newID
			}
			var existing model.SiteModel
			if err := tx.Where("site_account_id = ? AND group_key = ? AND model_name = ?", m.SiteAccountID, m.GroupKey, m.ModelName).First(&existing).Error; err == nil {
				continue
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("import site_models: %w", err)
			}
			if err := tx.Create(&m).Error; err != nil {
				return fmt.Errorf("import site_models: %w", err)
			}
			res.RowsAffected["site_models"]++
		}

		// 8. SiteChannelBindings (remap all FKs, dedup by both unique constraints)
		for i := range dump.SiteChannelBindings {
			binding := dump.SiteChannelBindings[i]
			binding.ID = 0
			if newID, ok := siteIDMap[binding.SiteID]; ok {
				binding.SiteID = newID
			}
			if newID, ok := accountIDMap[binding.SiteAccountID]; ok {
				binding.SiteAccountID = newID
			}
			if binding.SiteUserGroupID != nil {
				if newID, ok := userGroupIDMap[*binding.SiteUserGroupID]; ok {
					binding.SiteUserGroupID = &newID
				}
			}
			if newID, ok := channelIDMap[binding.ChannelID]; ok {
				binding.ChannelID = newID
			}

			var existing model.SiteChannelBinding
			if err := tx.Where("site_account_id = ? AND group_key = ?", binding.SiteAccountID, binding.GroupKey).First(&existing).Error; err == nil {
				continue
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("import site_channel_bindings: %w", err)
			}
			if err := tx.Where("channel_id = ?", binding.ChannelID).First(&existing).Error; err == nil {
				continue
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("import site_channel_bindings: %w", err)
			}
			if err := tx.Create(&binding).Error; err != nil {
				return fmt.Errorf("import site_channel_bindings: %w", err)
			}
			res.RowsAffected["site_channel_bindings"]++
		}

		// 9. Groups (dedup by name)
		for i := range dump.Groups {
			g := dump.Groups[i]
			oldID := g.ID
			g.ID = 0
			g.Items = nil

			var existing model.Group
			if err := tx.Where("name = ?", g.Name).First(&existing).Error; err == nil {
				groupIDMap[oldID] = existing.ID
				continue
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("import groups: %w", err)
			}
			if err := tx.Omit("Items").Create(&g).Error; err != nil {
				return fmt.Errorf("import groups: %w", err)
			}
			groupIDMap[oldID] = g.ID
			res.RowsAffected["groups"]++
		}

		// 10. GroupItems (remap group_id+channel_id, dedup by uniqueIndex)
		for i := range dump.GroupItems {
			item := dump.GroupItems[i]
			item.ID = 0
			if newID, ok := groupIDMap[item.GroupID]; ok {
				item.GroupID = newID
			}
			if newID, ok := channelIDMap[item.ChannelID]; ok {
				item.ChannelID = newID
			}
			var existing model.GroupItem
			if err := tx.Where("group_id = ? AND channel_id = ? AND model_name = ?", item.GroupID, item.ChannelID, item.ModelName).First(&existing).Error; err == nil {
				continue
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("import group_items: %w", err)
			}
			if err := tx.Create(&item).Error; err != nil {
				return fmt.Errorf("import group_items: %w", err)
			}
			res.RowsAffected["group_items"]++
		}

		// 11. LLMInfos (upsert by name - unchanged)
		if n, err := createUpsertAll(tx, dump.LLMInfos, []clause.Column{{Name: "name"}}); err != nil {
			return fmt.Errorf("import llm_infos: %w", err)
		} else {
			res.RowsAffected["llm_infos"] = n
		}

		// 12. APIKeys (dedup by api_key field)
		for i := range dump.APIKeys {
			key := dump.APIKeys[i]
			oldID := key.ID
			key.ID = 0

			var existing model.APIKey
			if err := tx.Where("api_key = ?", key.APIKey).First(&existing).Error; err == nil {
				apiKeyIDMap[oldID] = existing.ID
				continue
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("import api_keys: %w", err)
			}
			if err := tx.Create(&key).Error; err != nil {
				return fmt.Errorf("import api_keys: %w", err)
			}
			apiKeyIDMap[oldID] = key.ID
			res.RowsAffected["api_keys"]++
		}

		// 13. Settings (upsert by key - unchanged)
		if n, err := createUpsertSettings(tx, dump.Settings); err != nil {
			return fmt.Errorf("import settings: %w", err)
		} else {
			res.RowsAffected["settings"] = n
		}

		// 14. Stats (remap FK IDs, then upsert)
		if dump.IncludeStats {
			if n, err := createUpsertAll(tx, dump.StatsTotal, []clause.Column{{Name: "id"}}); err != nil {
				return fmt.Errorf("import stats_total: %w", err)
			} else {
				res.RowsAffected["stats_total"] = n
			}
			if n, err := createUpsertAll(tx, dump.StatsDaily, []clause.Column{{Name: "date"}}); err != nil {
				return fmt.Errorf("import stats_daily: %w", err)
			} else {
				res.RowsAffected["stats_daily"] = n
			}
			if n, err := createUpsertAll(tx, dump.StatsHourly, []clause.Column{{Name: "hour"}}); err != nil {
				return fmt.Errorf("import stats_hourly: %w", err)
			} else {
				res.RowsAffected["stats_hourly"] = n
			}

			// StatsModel: remap ChannelID, clear ID
			for i := range dump.StatsModel {
				dump.StatsModel[i].ID = 0
				if newID, ok := channelIDMap[dump.StatsModel[i].ChannelID]; ok {
					dump.StatsModel[i].ChannelID = newID
				}
			}
			if n, err := createDoNothing(tx, dump.StatsModel); err != nil {
				return fmt.Errorf("import stats_model: %w", err)
			} else {
				res.RowsAffected["stats_model"] = n
			}

			// StatsChannel: remap ChannelID (which is the PK)
			for i := range dump.StatsChannel {
				if newID, ok := channelIDMap[dump.StatsChannel[i].ChannelID]; ok {
					dump.StatsChannel[i].ChannelID = newID
				}
			}
			if n, err := createUpsertAll(tx, dump.StatsChannel, []clause.Column{{Name: "channel_id"}}); err != nil {
				return fmt.Errorf("import stats_channel: %w", err)
			} else {
				res.RowsAffected["stats_channel"] = n
			}

			// StatsAPIKey: remap APIKeyID (which is the PK)
			for i := range dump.StatsAPIKey {
				if newID, ok := apiKeyIDMap[dump.StatsAPIKey[i].APIKeyID]; ok {
					dump.StatsAPIKey[i].APIKeyID = newID
				}
			}
			if n, err := createUpsertAll(tx, dump.StatsAPIKey, []clause.Column{{Name: "api_key_id"}}); err != nil {
				return fmt.Errorf("import stats_api_key: %w", err)
			} else {
				res.RowsAffected["stats_api_key"] = n
			}
		}

		// 15. RelayLogs (Snowflake IDs - keep createDoNothing)
		if dump.IncludeLogs {
			if n, err := createDoNothing(tx, dump.RelayLogs); err != nil {
				return fmt.Errorf("import relay_logs: %w", err)
			} else {
				res.RowsAffected["relay_logs"] = n
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func createDoNothing[T any](tx *gorm.DB, rows []T) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows)
	return result.RowsAffected, result.Error
}

func createUpsertAll[T any](tx *gorm.DB, rows []T, columns []clause.Column) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   columns,
		UpdateAll: true,
	}).Create(&rows)
	return result.RowsAffected, result.Error
}

func createUpsertSettings(tx *gorm.DB, rows []model.Setting) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&rows)
	return result.RowsAffected, result.Error
}
