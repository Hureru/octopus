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
	dbDumpVersion = 2

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
	sanitizeSiteAccountsForBackup(d.SiteAccounts)
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
	d.Settings = sanitizeSettingsForBackup(d.Settings)
	if err := exportExtendedCoreTables(conn, d); err != nil {
		return nil, err
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
		if err := exportExtendedStatsTables(conn, d); err != nil {
			return nil, err
		}
	}

	if includeLogs {
		if err := exportRelayLogsPaged(ctx, conn, d); err != nil {
			return nil, err
		}
		if err := exportExtendedLogTables(conn, d); err != nil {
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

type dbImportState struct {
	channelIDMap             map[int]int
	channelKeyIDMap          map[int]int
	clashControllerIDMap     map[int]int
	proxyConfigIDMap         map[int]int
	siteIDMap                map[int]int
	accountIDMap             map[int]int
	userGroupIDMap           map[int]int
	groupIDMap               map[int]int
	apiKeyIDMap              map[int]int
	canonicalModelIDMap      map[int]int
	canonicalGroupNameMap    map[string]string
	routeCandidateIDMap      map[int]int
	priceQuoteIDMap          map[int]int
	relayLogIDMap            map[int64]int64
	legacyProxyIDByURL       map[string]int
	nextLegacyProxyID        int
	seenRelayLogIDs          map[int64]struct{}
	seenUsageRequestIDs      map[int64]struct{}
	seenUsageAttemptKeys     map[usageAttemptSourceKey]struct{}
	sourceUsageAggregateKeys map[string]struct{}
}

type usageAttemptSourceKey struct {
	relayLogID    int64
	attemptNumber int
}

func newDBImportState() *dbImportState {
	return &dbImportState{
		channelIDMap:             make(map[int]int),
		channelKeyIDMap:          make(map[int]int),
		clashControllerIDMap:     make(map[int]int),
		proxyConfigIDMap:         make(map[int]int),
		siteIDMap:                make(map[int]int),
		accountIDMap:             make(map[int]int),
		userGroupIDMap:           make(map[int]int),
		groupIDMap:               make(map[int]int),
		apiKeyIDMap:              make(map[int]int),
		canonicalModelIDMap:      make(map[int]int),
		canonicalGroupNameMap:    make(map[string]string),
		routeCandidateIDMap:      make(map[int]int),
		priceQuoteIDMap:          make(map[int]int),
		relayLogIDMap:            make(map[int64]int64),
		legacyProxyIDByURL:       make(map[string]int),
		nextLegacyProxyID:        -1,
		seenRelayLogIDs:          make(map[int64]struct{}),
		seenUsageRequestIDs:      make(map[int64]struct{}),
		seenUsageAttemptKeys:     make(map[usageAttemptSourceKey]struct{}),
		sourceUsageAggregateKeys: make(map[string]struct{}),
	}
}

func (state *dbImportState) registerRelayLogRows(rows []model.RelayLog) error {
	for _, row := range rows {
		if row.ID <= 0 {
			return fmt.Errorf("import relay_logs: source id must be positive")
		}
		if _, exists := state.seenRelayLogIDs[row.ID]; exists {
			return fmt.Errorf("import relay_logs: duplicate source id %d", row.ID)
		}
		state.seenRelayLogIDs[row.ID] = struct{}{}
	}
	return nil
}

func (state *dbImportState) registerUsageRows(dump *model.DBDump) error {
	for _, row := range dump.UsageRequestFacts {
		if row.RelayLogID <= 0 {
			return fmt.Errorf("import usage_request_facts: relay_log_id must be positive")
		}
		if _, exists := state.seenUsageRequestIDs[row.RelayLogID]; exists {
			return fmt.Errorf(
				"import usage_request_facts: duplicate relay_log_id %d",
				row.RelayLogID,
			)
		}
		state.seenUsageRequestIDs[row.RelayLogID] = struct{}{}
	}
	for _, row := range dump.UsageAttemptFacts {
		key := usageAttemptSourceKey{
			relayLogID:    row.RelayLogID,
			attemptNumber: row.AttemptNumber,
		}
		if key.relayLogID <= 0 || key.attemptNumber <= 0 {
			return fmt.Errorf("import usage_attempt_facts: invalid source identity")
		}
		if _, exists := state.seenUsageAttemptKeys[key]; exists {
			return fmt.Errorf(
				"import usage_attempt_facts: duplicate source identity %d/%d",
				key.relayLogID,
				key.attemptNumber,
			)
		}
		state.seenUsageAttemptKeys[key] = struct{}{}
	}
	return nil
}

type dbImportWork func(
	tx *gorm.DB,
	state *dbImportState,
	result *model.DBImportResult,
) error

func DBImportIncremental(ctx context.Context, dump *model.DBDump) (*model.DBImportResult, error) {
	if dump == nil {
		return nil, fmt.Errorf("empty dump")
	}
	if dump.Version < 0 || dump.Version > dbDumpVersion {
		return nil, fmt.Errorf("unsupported dump version: %d", dump.Version)
	}
	return runDBImportTransaction(ctx, func(
		tx *gorm.DB,
		state *dbImportState,
		result *model.DBImportResult,
	) error {
		return importDBDumpBatched(tx, dump, state, result)
	})
}

func importDBDumpBatched(
	tx *gorm.DB,
	dump *model.DBDump,
	state *dbImportState,
	result *model.DBImportResult,
) error {
	base := *dump
	relayLogs := base.RelayLogs
	requestFacts := base.UsageRequestFacts
	attemptFacts := base.UsageAttemptFacts
	aggregates := base.UsageAggregates
	statsModel := base.StatsModel
	statsChannel := base.StatsChannel
	statsAPIKey := base.StatsAPIKey
	statsSiteModelHourly := base.StatsSiteModelHourly
	base.RelayLogs = nil
	base.UsageRequestFacts = nil
	base.UsageAttemptFacts = nil
	base.UsageAggregates = nil
	base.StatsModel = nil
	base.StatsChannel = nil
	base.StatsAPIKey = nil
	base.StatsSiteModelHourly = nil
	if err := importDBDump(tx, &base, state, result); err != nil {
		return err
	}
	if err := importDBDumpRows(tx, statsModel, func(rows []model.StatsModel) error {
		return importDBDump(tx, &model.DBDump{
			IncludeStats: true,
			StatsModel:   rows,
		}, state, result)
	}); err != nil {
		return err
	}
	if err := importDBDumpRows(tx, statsChannel, func(rows []model.StatsChannel) error {
		return importDBDump(tx, &model.DBDump{
			IncludeStats: true,
			StatsChannel: rows,
		}, state, result)
	}); err != nil {
		return err
	}
	if err := importDBDumpRows(tx, statsAPIKey, func(rows []model.StatsAPIKey) error {
		return importDBDump(tx, &model.DBDump{
			IncludeStats: true,
			StatsAPIKey:  rows,
		}, state, result)
	}); err != nil {
		return err
	}
	if err := importDBDumpRows(tx, statsSiteModelHourly, func(rows []model.StatsSiteModelHourly) error {
		return importDBDump(tx, &model.DBDump{
			IncludeStats:         true,
			StatsSiteModelHourly: rows,
		}, state, result)
	}); err != nil {
		return err
	}
	if err := importDBDumpRows(tx, relayLogs, func(rows []model.RelayLog) error {
		return importDBDump(tx, &model.DBDump{
			IncludeLogs: true,
			RelayLogs:   rows,
		}, state, result)
	}); err != nil {
		return err
	}
	if err := importDBDumpRows(tx, aggregates, func(rows []model.UsageAggregate) error {
		return importDBDump(tx, &model.DBDump{
			IncludeStats:    true,
			UsageAggregates: rows,
		}, state, result)
	}); err != nil {
		return err
	}
	if err := importDBDumpRows(tx, requestFacts, func(rows []model.UsageRequestFact) error {
		return importDBDump(tx, &model.DBDump{
			IncludeStats:      true,
			UsageRequestFacts: rows,
		}, state, result)
	}); err != nil {
		return err
	}
	return importDBDumpRows(tx, attemptFacts, func(rows []model.UsageAttemptFact) error {
		return importDBDump(tx, &model.DBDump{
			IncludeStats:      true,
			UsageAttemptFacts: rows,
		}, state, result)
	})
}

func importDBDumpRows[T any](
	tx *gorm.DB,
	rows []T,
	importBatch func([]T) error,
) error {
	for start := 0; start < len(rows); start += dbImportBatchSize {
		if err := tx.Statement.Context.Err(); err != nil {
			return err
		}
		end := min(start+dbImportBatchSize, len(rows))
		if err := importBatch(rows[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func importDBDump(
	tx *gorm.DB,
	dump *model.DBDump,
	state *dbImportState,
	res *model.DBImportResult,
) error {
	channelIDMap := state.channelIDMap
	channelKeyIDMap := state.channelKeyIDMap
	clashControllerIDMap := state.clashControllerIDMap
	proxyConfigIDMap := state.proxyConfigIDMap
	siteIDMap := state.siteIDMap
	accountIDMap := state.accountIDMap
	userGroupIDMap := state.userGroupIDMap
	groupIDMap := state.groupIDMap
	apiKeyIDMap := state.apiKeyIDMap
	canonicalModelIDMap := state.canonicalModelIDMap
	canonicalGroupNameMap := state.canonicalGroupNameMap
	routeCandidateIDMap := state.routeCandidateIDMap
	priceQuoteIDMap := state.priceQuoteIDMap
	relayLogIDMap := state.relayLogIDMap

	migrateLegacyDumpProxyFields(dump, state)
	settings := dump.Settings
	clashControllers := dump.ClashControllers
	if len(settings) > 0 || len(clashControllers) > 0 {
		var err error
		settings, clashControllers, err = prepareBackupSecurity(
			tx,
			settings,
			clashControllers,
		)
		if err != nil {
			return err
		}
	}

	// 1. Clash controllers (dedup by name; preserve encrypted secrets).
	if err := importClashControllers(tx, clashControllers, res, clashControllerIDMap); err != nil {
		return err
	}

	// 2. ProxyConfigurations (dedup by url; disambiguate name conflicts)
	for i := range dump.ProxyConfigurations {
		proxyConfig := dump.ProxyConfigurations[i]
		oldID := proxyConfig.ID
		if err := rejectDuplicateImportSourceID(
			"proxy_configurations",
			oldID,
			proxyConfigIDMap,
		); err != nil {
			return err
		}
		proxyConfig.ID = 0
		proxyConfig.ReferenceCount = 0
		if err := remapOptionalImportID(
			"proxy_configurations",
			"clash_controller_id",
			&proxyConfig.ClashControllerID,
			clashControllerIDMap,
		); err != nil {
			return err
		}
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

	// 3. Channels (dedup by name)
	for i := range dump.Channels {
		ch := dump.Channels[i]
		oldID := ch.ID
		if err := rejectDuplicateImportSourceID("channels", oldID, channelIDMap); err != nil {
			return err
		}
		ch.ID = 0
		ch.Keys = nil
		ch.Stats = nil
		if err := remapProxyConfigImportID(
			"channels",
			&ch.ProxyMode,
			&ch.ProxyConfigID,
			proxyConfigIDMap,
		); err != nil {
			return err
		}

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

	// 4. ChannelKeys (remap channel_id, dedup by channel_id+channel_key)
	for i := range dump.ChannelKeys {
		key := dump.ChannelKeys[i]
		oldID := key.ID
		if err := rejectDuplicateImportSourceID(
			"channel_keys",
			oldID,
			channelKeyIDMap,
		); err != nil {
			return err
		}
		key.ID = 0
		channelID, err := requireImportID(
			"channel_keys",
			"channel_id",
			key.ChannelID,
			channelIDMap,
		)
		if err != nil {
			return err
		}
		key.ChannelID = channelID
		var existing model.ChannelKey
		if err := tx.Where("channel_id = ? AND channel_key = ?", key.ChannelID, key.ChannelKey).First(&existing).Error; err == nil {
			channelKeyIDMap[oldID] = existing.ID
			continue
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("import channel_keys: %w", err)
		}
		if err := tx.Create(&key).Error; err != nil {
			return fmt.Errorf("import channel_keys: %w", err)
		}
		channelKeyIDMap[oldID] = key.ID
		res.RowsAffected["channel_keys"]++
	}

	// 5. Sites (dedup by platform+base_url)
	for i := range dump.Sites {
		site := dump.Sites[i]
		oldID := site.ID
		if err := rejectDuplicateImportSourceID("sites", oldID, siteIDMap); err != nil {
			return err
		}
		site.ID = 0
		site.Accounts = nil
		if err := remapProxyConfigImportID(
			"sites",
			&site.ProxyMode,
			&site.ProxyConfigID,
			proxyConfigIDMap,
		); err != nil {
			return err
		}
		if err := remapOptionalImportID(
			"sites",
			"preferred_proxy_config_id",
			&site.PreferredProxyConfigID,
			proxyConfigIDMap,
		); err != nil {
			return err
		}

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

	// 6. SiteAccounts (remap site_id, dedup by site_id+name)
	for i := range dump.SiteAccounts {
		account := dump.SiteAccounts[i]
		oldID := account.ID
		if err := rejectDuplicateImportSourceID(
			"site_accounts",
			oldID,
			accountIDMap,
		); err != nil {
			return err
		}
		account.ID = 0
		account.Tokens = nil
		account.UserGroups = nil
		account.Models = nil
		account.ChannelBindings = nil
		if err := remapProxyConfigImportID(
			"site_accounts",
			&account.ProxyMode,
			&account.ProxyConfigID,
			proxyConfigIDMap,
		); err != nil {
			return err
		}
		if err := remapOptionalImportID(
			"site_accounts",
			"preferred_proxy_config_id",
			&account.PreferredProxyConfigID,
			proxyConfigIDMap,
		); err != nil {
			return err
		}
		account.VerificationCookieEncrypted = ""
		account.VerificationSessionFenceID = 0
		account.VerificationUserAgent = ""
		account.VerificationProxyConfigID = nil
		account.VerificationClashNode = ""
		account.VerificationExpiresAt = nil

		siteID, err := requireImportID(
			"site_accounts",
			"site_id",
			account.SiteID,
			siteIDMap,
		)
		if err != nil {
			return err
		}
		account.SiteID = siteID

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

	if err := importSiteProxyPreferences(
		tx,
		dump.SiteProxyPreferences,
		res,
		siteIDMap,
		accountIDMap,
		proxyConfigIDMap,
		clashControllerIDMap,
	); err != nil {
		return err
	}

	// 7. SiteTokens (remap site_account_id, dedup by site_account_id+token+group_key)
	for i := range dump.SiteTokens {
		token := dump.SiteTokens[i]
		token.ID = 0
		accountID, err := requireImportID(
			"site_tokens",
			"site_account_id",
			token.SiteAccountID,
			accountIDMap,
		)
		if err != nil {
			return err
		}
		token.SiteAccountID = accountID
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

	// 8. SiteUserGroups (remap site_account_id, dedup by uniqueIndex)
	for i := range dump.SiteUserGroups {
		group := dump.SiteUserGroups[i]
		oldID := group.ID
		if err := rejectDuplicateImportSourceID(
			"site_user_groups",
			oldID,
			userGroupIDMap,
		); err != nil {
			return err
		}
		group.ID = 0
		accountID, err := requireImportID(
			"site_user_groups",
			"site_account_id",
			group.SiteAccountID,
			accountIDMap,
		)
		if err != nil {
			return err
		}
		group.SiteAccountID = accountID
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

	// 9. SiteModels (remap site_account_id, dedup by uniqueIndex)
	for i := range dump.SiteModels {
		m := dump.SiteModels[i]
		m.ID = 0
		accountID, err := requireImportID(
			"site_models",
			"site_account_id",
			m.SiteAccountID,
			accountIDMap,
		)
		if err != nil {
			return err
		}
		m.SiteAccountID = accountID
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

	// 10. SiteChannelBindings (remap all FKs, dedup by both unique constraints)
	for i := range dump.SiteChannelBindings {
		binding := dump.SiteChannelBindings[i]
		binding.ID = 0
		siteID, err := requireImportID(
			"site_channel_bindings",
			"site_id",
			binding.SiteID,
			siteIDMap,
		)
		if err != nil {
			return err
		}
		binding.SiteID = siteID
		accountID, err := requireImportID(
			"site_channel_bindings",
			"site_account_id",
			binding.SiteAccountID,
			accountIDMap,
		)
		if err != nil {
			return err
		}
		binding.SiteAccountID = accountID
		if binding.SiteUserGroupID != nil {
			userGroupID, err := requireImportID(
				"site_channel_bindings",
				"site_user_group_id",
				*binding.SiteUserGroupID,
				userGroupIDMap,
			)
			if err != nil {
				return err
			}
			binding.SiteUserGroupID = &userGroupID
		}
		channelID, err := requireImportID(
			"site_channel_bindings",
			"channel_id",
			binding.ChannelID,
			channelIDMap,
		)
		if err != nil {
			return err
		}
		binding.ChannelID = channelID

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

	// 11. Canonical model identities, needed to merge their Group projections.
	if err := importCanonicalModels(
		tx,
		dump.CanonicalModels,
		res,
		canonicalModelIDMap,
		canonicalGroupNameMap,
	); err != nil {
		return err
	}

	// 12. Groups (dedup by canonical target name when applicable)
	for i := range dump.Groups {
		g := dump.Groups[i]
		oldID := g.ID
		if err := rejectDuplicateImportSourceID("groups", oldID, groupIDMap); err != nil {
			return err
		}
		g.ID = 0
		g.Items = nil
		g.Name = strings.TrimSpace(g.Name)
		if targetName, ok := canonicalGroupNameMap[NormalizeModelIdentity(g.Name)]; ok {
			g.Name = targetName
		}

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

	// 13. GroupItems (remap group_id+channel_id, dedup by uniqueIndex)
	for i := range dump.GroupItems {
		item := dump.GroupItems[i]
		item.ID = 0
		groupID, err := requireImportID(
			"group_items",
			"group_id",
			item.GroupID,
			groupIDMap,
		)
		if err != nil {
			return err
		}
		item.GroupID = groupID
		channelID, err := requireImportID(
			"group_items",
			"channel_id",
			item.ChannelID,
			channelIDMap,
		)
		if err != nil {
			return err
		}
		item.ChannelID = channelID
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

	// 14. LLMInfos (upsert by name - unchanged)
	if n, err := createUpsertAll(tx, dump.LLMInfos, []clause.Column{{Name: "name"}}); err != nil {
		return fmt.Errorf("import llm_infos: %w", err)
	} else {
		res.RowsAffected["llm_infos"] += n
	}

	// 15. APIKeys (dedup by api_key field)
	for i := range dump.APIKeys {
		key := dump.APIKeys[i]
		oldID := key.ID
		if err := rejectDuplicateImportSourceID("api_keys", oldID, apiKeyIDMap); err != nil {
			return err
		}
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

	// 16. Settings (upsert by key - unchanged)
	if n, err := createUpsertSettings(tx, settings); err != nil {
		return fmt.Errorf("import settings: %w", err)
	} else {
		res.RowsAffected["settings"] += n
	}

	// 17. Catalog relations, protocol/header policy, pricing and user-agent configuration.
	if err := importModelAliases(tx, dump.ModelAliases, res, canonicalModelIDMap); err != nil {
		return err
	}
	if err := importRouteCandidates(
		tx,
		dump.RouteCandidates,
		res,
		canonicalModelIDMap,
		channelIDMap,
		siteIDMap,
		accountIDMap,
		routeCandidateIDMap,
	); err != nil {
		return err
	}
	if err := importHeaderPolicies(
		tx,
		dump.HeaderPolicies,
		res,
		siteIDMap,
		accountIDMap,
		channelIDMap,
		canonicalModelIDMap,
		routeCandidateIDMap,
	); err != nil {
		return err
	}
	if err := importUserAgentProfiles(tx, dump.UserAgentProfiles, res); err != nil {
		return err
	}
	if err := importCurrencyRates(tx, dump.CurrencyRates, res); err != nil {
		return err
	}
	if err := importSiteModelPriceQuotes(
		tx,
		dump.SiteModelPriceQuotes,
		res,
		siteIDMap,
		accountIDMap,
		routeCandidateIDMap,
		priceQuoteIDMap,
	); err != nil {
		return err
	}

	// 18. Stats (remap FK IDs, then upsert)
	if dump.IncludeStats {
		if n, err := createUpsertAll(tx, dump.StatsTotal, []clause.Column{{Name: "id"}}); err != nil {
			return fmt.Errorf("import stats_total: %w", err)
		} else {
			res.RowsAffected["stats_total"] += n
		}
		if n, err := createUpsertAll(tx, dump.StatsDaily, []clause.Column{{Name: "date"}}); err != nil {
			return fmt.Errorf("import stats_daily: %w", err)
		} else {
			res.RowsAffected["stats_daily"] += n
		}
		if n, err := createUpsertAll(tx, dump.StatsHourly, []clause.Column{{Name: "hour"}}); err != nil {
			return fmt.Errorf("import stats_hourly: %w", err)
		} else {
			res.RowsAffected["stats_hourly"] += n
		}

		// StatsModel: every positive foreign key must resolve inside this import.
		filteredStatsModel := make([]model.StatsModel, 0, len(dump.StatsModel))
		for _, row := range dump.StatsModel {
			newID, err := requireImportID("stats_model", "channel_id", row.ChannelID, channelIDMap)
			if err != nil {
				return err
			}
			row.ID = 0
			row.ChannelID = newID
			filteredStatsModel = append(filteredStatsModel, row)
		}
		if n, err := createDoNothing(tx, filteredStatsModel); err != nil {
			return fmt.Errorf("import stats_model: %w", err)
		} else {
			res.RowsAffected["stats_model"] += n
		}

		// StatsChannel: remap ChannelID (which is the PK).
		filteredStatsChannel := make([]model.StatsChannel, 0, len(dump.StatsChannel))
		for _, row := range dump.StatsChannel {
			newID, err := requireImportID("stats_channel", "channel_id", row.ChannelID, channelIDMap)
			if err != nil {
				return err
			}
			row.ChannelID = newID
			filteredStatsChannel = append(filteredStatsChannel, row)
		}
		if n, err := createUpsertAll(tx, filteredStatsChannel, []clause.Column{{Name: "channel_id"}}); err != nil {
			return fmt.Errorf("import stats_channel: %w", err)
		} else {
			res.RowsAffected["stats_channel"] += n
		}

		// StatsAPIKey: remap APIKeyID (which is the PK).
		filteredStatsAPIKey := make([]model.StatsAPIKey, 0, len(dump.StatsAPIKey))
		for _, row := range dump.StatsAPIKey {
			newID, err := requireImportID("stats_api_key", "api_key_id", row.APIKeyID, apiKeyIDMap)
			if err != nil {
				return err
			}
			row.APIKeyID = newID
			filteredStatsAPIKey = append(filteredStatsAPIKey, row)
		}
		if n, err := createUpsertAll(tx, filteredStatsAPIKey, []clause.Column{{Name: "api_key_id"}}); err != nil {
			return fmt.Errorf("import stats_api_key: %w", err)
		} else {
			res.RowsAffected["stats_api_key"] += n
		}

		// StatsSiteModelHourly: remap SiteAccountID (composite PK)
		filteredSiteModelHourly := make([]model.StatsSiteModelHourly, 0, len(dump.StatsSiteModelHourly))
		for _, row := range dump.StatsSiteModelHourly {
			newID, err := requireImportID(
				"stats_site_model_hourly",
				"site_account_id",
				row.SiteAccountID,
				accountIDMap,
			)
			if err != nil {
				return err
			}
			row.SiteAccountID = newID
			filteredSiteModelHourly = append(filteredSiteModelHourly, row)
		}
		if n, err := createUpsertAll(tx, filteredSiteModelHourly, []clause.Column{
			{Name: "hour"}, {Name: "site_account_id"}, {Name: "group_key"}, {Name: "model_name"},
		}); err != nil {
			return fmt.Errorf("import stats_site_model_hourly: %w", err)
		} else {
			res.RowsAffected["stats_site_model_hourly"] += n
		}
	}

	// 19. RelayLogs and operational audit trails.
	if dump.IncludeLogs {
		if err := state.registerRelayLogRows(dump.RelayLogs); err != nil {
			return err
		}
		relayLogs, remappedRelayLogIDs, err := remapRelayLogsForImport(
			tx,
			dump.RelayLogs,
			channelIDMap,
			channelKeyIDMap,
			apiKeyIDMap,
			routeCandidateIDMap,
			priceQuoteIDMap,
		)
		if err != nil {
			return err
		}
		for sourceID, targetID := range remappedRelayLogIDs {
			relayLogIDMap[sourceID] = targetID
		}
		if n, err := createDoNothing(tx, relayLogs); err != nil {
			return fmt.Errorf("import relay_logs: %w", err)
		} else {
			res.RowsAffected["relay_logs"] += n
		}
		if err := importRelayLogRepairAudits(tx, dump.RelayLogRepairAudits, res); err != nil {
			return err
		}
		if err := importSiteOperationAttempts(
			tx,
			dump.SiteOperationAttempts,
			res,
			siteIDMap,
			accountIDMap,
			proxyConfigIDMap,
			clashControllerIDMap,
		); err != nil {
			return err
		}
	}

	// 20. Long-term usage facts and aggregate snapshots.
	if dump.IncludeStats {
		if err := state.registerUsageRows(dump); err != nil {
			return err
		}
		if err := importUsageTables(
			tx,
			dump,
			res,
			siteIDMap,
			accountIDMap,
			channelIDMap,
			apiKeyIDMap,
			routeCandidateIDMap,
			priceQuoteIDMap,
			relayLogIDMap,
			state.sourceUsageAggregateKeys,
		); err != nil {
			return err
		}
	}

	return nil
}

func runDBImportTransaction(
	ctx context.Context,
	work dbImportWork,
) (*model.DBImportResult, error) {
	res := &model.DBImportResult{RowsAffected: map[string]int64{}}
	state := newDBImportState()
	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return work(tx, state, res)
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

func migrateLegacyDumpProxyFields(dump *model.DBDump, state *dbImportState) {
	if dump == nil || state == nil {
		return
	}
	// Import normalization is internal working state. Clone every slice this
	// migration mutates so callers can safely reuse or retry the same dump.
	dump.ProxyConfigurations = append([]model.ProxyConfiguration(nil), dump.ProxyConfigurations...)
	dump.Channels = append([]model.Channel(nil), dump.Channels...)
	dump.Sites = append([]model.Site(nil), dump.Sites...)
	dump.SiteAccounts = append([]model.SiteAccount(nil), dump.SiteAccounts...)

	for _, proxyConfig := range dump.ProxyConfigurations {
		normalized, err := model.NormalizeProxyURL(proxyConfig.URL)
		if err != nil || proxyConfig.ID == 0 {
			continue
		}
		if _, exists := state.legacyProxyIDByURL[normalized]; !exists {
			state.legacyProxyIDByURL[normalized] = proxyConfig.ID
		}
	}
	ensureProxyConfig := func(raw string) *int {
		normalized, err := model.NormalizeProxyURL(raw)
		if err != nil {
			return nil
		}
		if id, ok := state.legacyProxyIDByURL[normalized]; ok {
			return &id
		}
		id := state.nextLegacyProxyID
		state.nextLegacyProxyID--
		state.legacyProxyIDByURL[normalized] = id
		dump.ProxyConfigurations = append(dump.ProxyConfigurations, model.ProxyConfiguration{
			ID:      id,
			Name:    fmt.Sprintf("Imported Proxy %d", -id),
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

func remapProxyConfigImportID(
	table string,
	mode *model.ProxyUsageMode,
	id **int,
	idMap map[int]int,
) error {
	if mode == nil || id == nil {
		return fmt.Errorf("import %s: proxy mode reference is missing", table)
	}
	if *mode != model.ProxyUsageModePool {
		if id != nil {
			*id = nil
		}
		return nil
	}
	if *id == nil {
		return fmt.Errorf(
			"import %s: pool mode has no proxy_config_id",
			table,
		)
	}
	if newID, ok := idMap[**id]; ok {
		*id = &newID
		return nil
	}
	return fmt.Errorf(
		"import %s: proxy_config_id %d has no imported parent",
		table,
		**id,
	)
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
	for _, row := range rows {
		if err := row.Validate(); err != nil {
			return 0, fmt.Errorf("setting %s: %w", row.Key, err)
		}
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).CreateInBatches(&rows, dbImportBatchSize)
	return result.RowsAffected, result.Error
}

func prepareBackupSecurity(
	tx *gorm.DB,
	settings []model.Setting,
	controllers []model.ClashControllerBackup,
) ([]model.Setting, []model.ClashControllerBackup, error) {
	sourceJWT := ""
	filteredSettings := make([]model.Setting, 0, len(settings))
	for _, setting := range settings {
		if setting.Key == model.SettingKeyJWTSecret {
			sourceJWT = setting.Value
			continue
		}
		filteredSettings = append(filteredSettings, setting)
	}

	targetJWT := ""
	var target model.Setting
	err := tx.First(&target, "key = ?", model.SettingKeyJWTSecret).Error
	if err == nil {
		targetJWT = target.Value
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, fmt.Errorf("read target jwt secret: %w", err)
	}
	if targetJWT == "" && sourceJWT != "" {
		filteredSettings = append(filteredSettings, model.Setting{
			Key:   model.SettingKeyJWTSecret,
			Value: sourceJWT,
		})
		targetJWT = sourceJWT
	}

	resultControllers := append([]model.ClashControllerBackup(nil), controllers...)
	for index := range resultControllers {
		if resultControllers[index].SecretEncrypted == "" {
			continue
		}
		if targetJWT == "" {
			return nil, nil, fmt.Errorf(
				"import clash_controllers: encrypted secret has no usable jwt key",
			)
		}
		decryptionJWT := sourceJWT
		if decryptionJWT == "" {
			// Legacy backups without Settings can only be restored onto the
			// instance that encrypted them. Validation below rejects a mismatch.
			decryptionJWT = targetJWT
		}
		rewrapped, err := reencryptSecret(
			resultControllers[index].SecretEncrypted,
			decryptionJWT,
			targetJWT,
		)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"import clash_controllers: re-encrypt %q: %w",
				resultControllers[index].Name,
				err,
			)
		}
		resultControllers[index].SecretEncrypted = rewrapped
	}
	return filteredSettings, resultControllers, nil
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
	if err := writeZipSiteAccounts(ctx, zw, conn); err != nil {
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
	if err := writeZipSettings(ctx, zw, conn); err != nil {
		return err
	}
	if err := writeZipExtendedCoreTables(ctx, zw, conn); err != nil {
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
		if err := writeZipExtendedStatsTables(ctx, zw, conn); err != nil {
			return err
		}
	}

	if includeLogs {
		if err := writeZipRelayLogsNDJSON(ctx, zw, conn); err != nil {
			return err
		}
		if err := writeZipExtendedLogTables(ctx, zw, conn); err != nil {
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
