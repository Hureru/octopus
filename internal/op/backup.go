package op

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	dbDumpVersion = 1

	// Keep import batches small enough for SQLite builds with low SQL variable limits.
	// Some exported tables (for example relay_logs) have many columns, so a conservative
	// row count avoids "too many SQL variables" during bulk insert/upsert.
	dbImportBatchSize    = 20
	dbExportLogBatchSize = 1000
)

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
	if err := conn.Find(&d.ProxyConfigurations).Error; err != nil {
		return nil, fmt.Errorf("export proxy_configurations: %w", err)
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
		if err := conn.Find(&d.StatsSiteModelHourly).Error; err != nil {
			return nil, fmt.Errorf("export stats_site_model_hourly: %w", err)
		}
	}

	if includeLogs {
		if err := exportRelayLogsPaged(ctx, conn, d); err != nil {
			return nil, err
		}
	}

	return d, nil
}

func exportRelayLogsPaged(ctx context.Context, conn *gorm.DB, d *model.DBDump) error {
	var lastID int64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var batch []model.RelayLog
		if err := conn.Where("id > ?", lastID).Order("id ASC").Limit(dbExportLogBatchSize).Find(&batch).Error; err != nil {
			return fmt.Errorf("export relay_logs: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		d.RelayLogs = append(d.RelayLogs, batch...)
		lastID = batch[len(batch)-1].ID
		if len(batch) < dbExportLogBatchSize {
			break
		}
	}
	return nil
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
		proxyConfigIDMap := make(map[int]int)
		siteIDMap := make(map[int]int)
		accountIDMap := make(map[int]int)
		userGroupIDMap := make(map[int]int)
		groupIDMap := make(map[int]int)
		apiKeyIDMap := make(map[int]int)

		migrateLegacyDumpProxyFields(dump)

		// 1. ProxyConfigurations (dedup by url; disambiguate name conflicts)
		for i := range dump.ProxyConfigurations {
			proxyConfig := dump.ProxyConfigurations[i]
			oldID := proxyConfig.ID
			proxyConfig.ID = 0
			proxyConfig.ReferenceCount = 0
			if err := proxyConfig.Validate(); err != nil {
				return fmt.Errorf("import proxy_configurations: %w", err)
			}

			var existing model.ProxyConfiguration
			if err := tx.Where("url = ?", proxyConfig.URL).First(&existing).Error; err == nil {
				if proxyConfig.Enabled && !existing.Enabled {
					if err := tx.Model(&existing).Update("enabled", true).Error; err != nil {
						return fmt.Errorf("import proxy_configurations: %w", err)
					}
				}
				proxyConfigIDMap[oldID] = existing.ID
				continue
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("import proxy_configurations: %w", err)
			}
			if err := tx.Where("name = ?", proxyConfig.Name).First(&existing).Error; err == nil {
				oldName := proxyConfig.Name
				proxyConfig.Name = uniqueProxyConfigName(proxyConfig.Name, tx)
				log.Warnw("proxy configuration name conflict during import",
					"old_id", oldID,
					"existing_id", existing.ID,
					"existing_url", existing.URL,
					"import_url", proxyConfig.URL,
					"old_name", oldName,
					"new_name", proxyConfig.Name,
				)
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("import proxy_configurations: %w", err)
			}
			if err := tx.Create(&proxyConfig).Error; err != nil {
				return fmt.Errorf("import proxy_configurations: %w", err)
			}
			proxyConfigIDMap[oldID] = proxyConfig.ID
			res.RowsAffected["proxy_configurations"]++
		}

		// 2. Channels (dedup by name)
		for i := range dump.Channels {
			ch := dump.Channels[i]
			oldID := ch.ID
			ch.ID = 0
			ch.Keys = nil
			ch.Stats = nil
			remapProxyConfigID(&ch.ProxyMode, &ch.ProxyConfigID, proxyConfigIDMap)

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

		// 3. ChannelKeys (remap channel_id, dedup by channel_id+channel_key)
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

		// 4. Sites (dedup by platform+base_url)
		for i := range dump.Sites {
			site := dump.Sites[i]
			oldID := site.ID
			site.ID = 0
			site.Accounts = nil
			remapProxyConfigID(&site.ProxyMode, &site.ProxyConfigID, proxyConfigIDMap)

			// Preserve the path in base_url (e.g. https://opencode.ai/zen/v1):
			// native backups already hold full, canonical URLs. Only trim like
			// Site.Normalize so dedup compares against the stored value. (Do not
			// use normalizeImportBaseURL here — it strips the path, which is only
			// correct for third-party imports.)
			site.BaseURL = strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")

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

		// 5. SiteAccounts (remap site_id, dedup by site_id+name)
		for i := range dump.SiteAccounts {
			account := dump.SiteAccounts[i]
			oldID := account.ID
			account.ID = 0
			account.Tokens = nil
			account.UserGroups = nil
			account.Models = nil
			account.ChannelBindings = nil
			remapProxyConfigID(&account.ProxyMode, &account.ProxyConfigID, proxyConfigIDMap)

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

		// 6. SiteTokens (remap site_account_id, dedup by site_account_id+token+group_key)
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

		// 7. SiteUserGroups (remap site_account_id, dedup by uniqueIndex)
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

		// 8. SiteModels (remap site_account_id, dedup by uniqueIndex)
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

		// 9. SiteChannelBindings (remap all FKs, dedup by both unique constraints)
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

		// 10. Groups (dedup by name)
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

		// 11. GroupItems (remap group_id+channel_id, dedup by uniqueIndex)
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

		// 12. LLMInfos (upsert by name - unchanged)
		if n, err := createUpsertAll(tx, dump.LLMInfos, []clause.Column{{Name: "name"}}); err != nil {
			return fmt.Errorf("import llm_infos: %w", err)
		} else {
			res.RowsAffected["llm_infos"] = n
		}

		// 13. APIKeys (dedup by api_key field)
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

		// 14. Settings (upsert by key - unchanged)
		if n, err := createUpsertSettings(tx, dump.Settings); err != nil {
			return fmt.Errorf("import settings: %w", err)
		} else {
			res.RowsAffected["settings"] = n
		}

		// 15. Stats (remap FK IDs, then upsert)
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

			// StatsModel: remap ChannelID, clear ID. Skip orphaned rows whose channel
			// is not present in the dump, otherwise SQLite foreign keys can fail.
			filteredStatsModel := make([]model.StatsModel, 0, len(dump.StatsModel))
			for _, row := range dump.StatsModel {
				newID, ok := channelIDMap[row.ChannelID]
				if !ok {
					continue
				}
				row.ID = 0
				row.ChannelID = newID
				filteredStatsModel = append(filteredStatsModel, row)
			}
			if n, err := createDoNothing(tx, filteredStatsModel); err != nil {
				return fmt.Errorf("import stats_model: %w", err)
			} else {
				res.RowsAffected["stats_model"] = n
			}

			// StatsChannel: remap ChannelID (which is the PK). Skip orphaned rows whose
			// channel is not present in the dump, otherwise SQLite foreign keys can fail.
			filteredStatsChannel := make([]model.StatsChannel, 0, len(dump.StatsChannel))
			for _, row := range dump.StatsChannel {
				newID, ok := channelIDMap[row.ChannelID]
				if !ok {
					continue
				}
				row.ChannelID = newID
				filteredStatsChannel = append(filteredStatsChannel, row)
			}
			if n, err := createUpsertAll(tx, filteredStatsChannel, []clause.Column{{Name: "channel_id"}}); err != nil {
				return fmt.Errorf("import stats_channel: %w", err)
			} else {
				res.RowsAffected["stats_channel"] = n
			}

			// StatsAPIKey: remap APIKeyID (which is the PK). Skip orphaned rows whose
			// API key is not present in the dump, otherwise SQLite foreign keys can fail.
			filteredStatsAPIKey := make([]model.StatsAPIKey, 0, len(dump.StatsAPIKey))
			for _, row := range dump.StatsAPIKey {
				newID, ok := apiKeyIDMap[row.APIKeyID]
				if !ok {
					continue
				}
				row.APIKeyID = newID
				filteredStatsAPIKey = append(filteredStatsAPIKey, row)
			}
			if n, err := createUpsertAll(tx, filteredStatsAPIKey, []clause.Column{{Name: "api_key_id"}}); err != nil {
				return fmt.Errorf("import stats_api_key: %w", err)
			} else {
				res.RowsAffected["stats_api_key"] = n
			}

			// StatsSiteModelHourly: remap SiteAccountID (composite PK)
			filteredSiteModelHourly := make([]model.StatsSiteModelHourly, 0, len(dump.StatsSiteModelHourly))
			for _, row := range dump.StatsSiteModelHourly {
				newID, ok := accountIDMap[row.SiteAccountID]
				if !ok {
					continue
				}
				row.SiteAccountID = newID
				filteredSiteModelHourly = append(filteredSiteModelHourly, row)
			}
			if n, err := createUpsertAll(tx, filteredSiteModelHourly, []clause.Column{
				{Name: "hour"}, {Name: "site_account_id"}, {Name: "group_key"}, {Name: "model_name"},
			}); err != nil {
				return fmt.Errorf("import stats_site_model_hourly: %w", err)
			} else {
				res.RowsAffected["stats_site_model_hourly"] = n
			}
		}

		// 16. RelayLogs (Snowflake IDs - keep createDoNothing)
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
	// The import transaction has already committed; cache refresh failures are non-fatal
	// and can be recovered by a later InitCache/refresh cycle.
	if err := proxyConfigurationRefreshCache(ctx); err != nil {
		log.Warnw("refresh proxy configuration cache after import failed",
			"operation", "db_import_incremental",
			"error", err,
		)
	}
	return res, nil
}

func migrateLegacyDumpProxyFields(dump *model.DBDump) {
	if dump == nil {
		return
	}
	proxyIDByURL := make(map[string]int)
	for _, proxyConfig := range dump.ProxyConfigurations {
		if normalized, err := model.NormalizeProxyURL(proxyConfig.URL); err == nil && proxyConfig.ID > 0 {
			proxyIDByURL[normalized] = proxyConfig.ID
		}
	}
	ensureProxyConfig := func(raw string) *int {
		normalized, err := model.NormalizeProxyURL(raw)
		if err != nil {
			return nil
		}
		if id, ok := proxyIDByURL[normalized]; ok {
			return &id
		}
		id := -len(proxyIDByURL) - 1
		proxyIDByURL[normalized] = id
		dump.ProxyConfigurations = append(dump.ProxyConfigurations, model.ProxyConfiguration{
			ID:      id,
			Name:    fmt.Sprintf("Imported Proxy %d", len(proxyIDByURL)),
			URL:     normalized,
			Enabled: true,
			Remark:  "由历史备份代理配置迁移生成",
		})
		return &id
	}
	for i := range dump.Channels {
		ch := &dump.Channels[i]
		if ch.ProxyMode != "" {
			continue
		}
		if !ch.Proxy {
			ch.ProxyMode = model.ProxyUsageModeDirect
			ch.ProxyConfigID = nil
		} else if ch.ChannelProxy != nil && strings.TrimSpace(*ch.ChannelProxy) != "" {
			ch.ProxyMode = model.ProxyUsageModePool
			ch.ProxyConfigID = ensureProxyConfig(*ch.ChannelProxy)
		} else {
			ch.ProxyMode = model.ProxyUsageModeSystem
			ch.ProxyConfigID = nil
		}
	}
	for i := range dump.Sites {
		site := &dump.Sites[i]
		if site.ProxyMode != "" {
			continue
		}
		if site.Proxy {
			if site.SiteProxy != nil && strings.TrimSpace(*site.SiteProxy) != "" {
				site.ProxyMode = model.ProxyUsageModePool
				site.ProxyConfigID = ensureProxyConfig(*site.SiteProxy)
			} else {
				site.ProxyMode = model.ProxyUsageModeSystem
				site.ProxyConfigID = nil
			}
		} else if site.UseSystemProxy {
			site.ProxyMode = model.ProxyUsageModeSystem
			site.ProxyConfigID = nil
		} else {
			site.ProxyMode = model.ProxyUsageModeDirect
			site.ProxyConfigID = nil
		}
	}
	for i := range dump.SiteAccounts {
		account := &dump.SiteAccounts[i]
		if account.ProxyMode != "" {
			continue
		}
		if account.AccountProxy != nil && strings.TrimSpace(*account.AccountProxy) != "" {
			account.ProxyMode = model.ProxyUsageModePool
			account.ProxyConfigID = ensureProxyConfig(*account.AccountProxy)
		} else {
			account.ProxyMode = model.ProxyUsageModeInherit
			account.ProxyConfigID = nil
		}
	}
}

func uniqueProxyConfigName(baseName string, tx *gorm.DB) string {
	baseName = strings.TrimSpace(baseName)
	if baseName == "" {
		baseName = "imported-proxy"
	}
	candidate := baseName
	index := 2
	for {
		var count int64
		if err := tx.Model(&model.ProxyConfiguration{}).Where("name = ?", candidate).Count(&count).Error; err != nil {
			return candidate
		}
		if count == 0 {
			return candidate
		}
		candidate = fmt.Sprintf("%s (%d)", baseName, index)
		index++
	}
}

func remapProxyConfigID(mode *model.ProxyUsageMode, id **int, idMap map[int]int) {
	if mode == nil || id == nil || *mode != model.ProxyUsageModePool {
		if id != nil {
			*id = nil
		}
		return
	}
	if *id == nil {
		log.Warnw("remapProxyConfigID downgraded proxy mode",
			"original_mode", *mode,
			"proxy_config_id", nil,
			"reason", "nil",
		)
		*mode = model.ProxyUsageModeDirect
		*id = nil
		return
	}
	if newID, ok := idMap[**id]; ok {
		*id = &newID
		return
	}
	if **id <= 0 {
		log.Warnw("remapProxyConfigID downgraded proxy mode",
			"original_mode", *mode,
			"proxy_config_id", **id,
			"reason", "invalid",
		)
		*mode = model.ProxyUsageModeDirect
		*id = nil
		return
	}
	log.Warnw("remapProxyConfigID downgraded proxy mode",
		"original_mode", *mode,
		"proxy_config_id", **id,
		"reason", "not found in idMap",
	)
	*mode = model.ProxyUsageModeDirect
	*id = nil
}

func createDoNothing[T any](tx *gorm.DB, rows []T) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&rows, dbImportBatchSize)
	return result.RowsAffected, result.Error
}

func createUpsertAll[T any](tx *gorm.DB, rows []T, columns []clause.Column) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   columns,
		UpdateAll: true,
	}).CreateInBatches(&rows, dbImportBatchSize)
	return result.RowsAffected, result.Error
}

func createUpsertSettings(tx *gorm.DB, rows []model.Setting) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).CreateInBatches(&rows, dbImportBatchSize)
	return result.RowsAffected, result.Error
}

// DBExportZip streams the database dump as a ZIP archive: small tables become
// JSON files, relay_logs become NDJSON to avoid building a giant in-memory
// slice. The writer is consumed once; failures partway through cannot return a
// JSON error to the client, so callers should validate inputs before invoking.
func DBExportZip(ctx context.Context, w io.Writer, includeLogs, includeStats bool) (err error) {
	zw := zip.NewWriter(w)
	defer func() {
		if closeErr := zw.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	conn := db.GetDB().WithContext(ctx)

	manifest := map[string]any{
		"version":       dbDumpVersion,
		"exported_at":   time.Now().UTC().Format(time.RFC3339),
		"include_logs":  includeLogs,
		"include_stats": includeStats,
		"format":        "zip-v1",
	}
	if err := writeZipJSON(zw, "manifest.json", manifest); err != nil {
		return err
	}

	if err := writeZipTable(ctx, zw, conn, "channels.json", &[]model.Channel{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "channel_keys.json", &[]model.ChannelKey{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "proxy_configurations.json", &[]model.ProxyConfiguration{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "sites.json", &[]model.Site{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "site_accounts.json", &[]model.SiteAccount{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "site_tokens.json", &[]model.SiteToken{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "site_user_groups.json", &[]model.SiteUserGroup{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "site_models.json", &[]model.SiteModel{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "site_channel_bindings.json", &[]model.SiteChannelBinding{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "groups.json", &[]model.Group{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "group_items.json", &[]model.GroupItem{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "llm_infos.json", &[]model.LLMInfo{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "api_keys.json", &[]model.APIKey{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "settings.json", &[]model.Setting{}); err != nil {
		return err
	}

	if includeStats {
		if err := writeZipTable(ctx, zw, conn, "stats_total.json", &[]model.StatsTotal{}); err != nil {
			return err
		}
		if err := writeZipTable(ctx, zw, conn, "stats_daily.json", &[]model.StatsDaily{}); err != nil {
			return err
		}
		if err := writeZipTable(ctx, zw, conn, "stats_hourly.json", &[]model.StatsHourly{}); err != nil {
			return err
		}
		if err := writeZipTable(ctx, zw, conn, "stats_model.json", &[]model.StatsModel{}); err != nil {
			return err
		}
		if err := writeZipTable(ctx, zw, conn, "stats_channel.json", &[]model.StatsChannel{}); err != nil {
			return err
		}
		if err := writeZipTable(ctx, zw, conn, "stats_api_key.json", &[]model.StatsAPIKey{}); err != nil {
			return err
		}
		if err := writeZipTable(ctx, zw, conn, "stats_site_model_hourly.json", &[]model.StatsSiteModelHourly{}); err != nil {
			return err
		}
	}

	if includeLogs {
		if err := writeZipRelayLogsNDJSON(ctx, zw, conn); err != nil {
			return err
		}
	}

	return nil
}

func writeZipJSON(zw *zip.Writer, name string, value any) error {
	f, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("zip create %s: %w", name, err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "")
	if err := enc.Encode(value); err != nil {
		return fmt.Errorf("zip encode %s: %w", name, err)
	}
	return nil
}

func writeZipTable[T any](ctx context.Context, zw *zip.Writer, conn *gorm.DB, name string, dest *[]T) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := conn.Find(dest).Error; err != nil {
		return fmt.Errorf("zip read %s: %w", name, err)
	}
	return writeZipJSON(zw, name, dest)
}

func writeZipRelayLogsNDJSON(ctx context.Context, zw *zip.Writer, conn *gorm.DB) error {
	f, err := zw.Create("relay_logs.ndjson")
	if err != nil {
		return fmt.Errorf("zip create relay_logs.ndjson: %w", err)
	}
	enc := json.NewEncoder(f)
	var lastID int64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var batch []model.RelayLog
		if err := conn.Where("id > ?", lastID).Order("id ASC").Limit(dbExportLogBatchSize).Find(&batch).Error; err != nil {
			return fmt.Errorf("zip read relay_logs: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		for i := range batch {
			if err := enc.Encode(&batch[i]); err != nil {
				return fmt.Errorf("zip encode relay_log: %w", err)
			}
		}
		lastID = batch[len(batch)-1].ID
		if len(batch) < dbExportLogBatchSize {
			break
		}
	}
	return nil
}
