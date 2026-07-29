package op

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func exportExtendedCoreTables(conn *gorm.DB, dump *model.DBDump) error {
	if err := conn.Find(&dump.CanonicalModels).Error; err != nil {
		return fmt.Errorf("export canonical_models: %w", err)
	}
	if err := conn.Find(&dump.ModelAliases).Error; err != nil {
		return fmt.Errorf("export model_aliases: %w", err)
	}
	if err := conn.Find(&dump.RouteCandidates).Error; err != nil {
		return fmt.Errorf("export route_candidates: %w", err)
	}
	if err := conn.Find(&dump.HeaderPolicies).Error; err != nil {
		return fmt.Errorf("export header_policies: %w", err)
	}
	if err := conn.Find(&dump.UserAgentProfiles).Error; err != nil {
		return fmt.Errorf("export user_agent_profiles: %w", err)
	}
	if err := conn.Find(&dump.SiteModelPriceQuotes).Error; err != nil {
		return fmt.Errorf("export site_model_price_quotes: %w", err)
	}
	if err := conn.Find(&dump.CurrencyRates).Error; err != nil {
		return fmt.Errorf("export currency_rates: %w", err)
	}

	var controllers []model.ClashController
	if err := conn.Find(&controllers).Error; err != nil {
		return fmt.Errorf("export clash_controllers: %w", err)
	}
	dump.ClashControllers = make([]model.ClashControllerBackup, 0, len(controllers))
	for _, controller := range controllers {
		dump.ClashControllers = append(dump.ClashControllers, clashControllerBackupFromModel(controller))
	}
	if err := conn.Find(&dump.SiteProxyPreferences).Error; err != nil {
		return fmt.Errorf("export site_proxy_preferences: %w", err)
	}
	return nil
}

func exportExtendedStatsTables(conn *gorm.DB, dump *model.DBDump) error {
	if err := conn.Find(&dump.UsageRequestFacts).Error; err != nil {
		return fmt.Errorf("export usage_request_facts: %w", err)
	}
	if err := conn.Find(&dump.UsageAttemptFacts).Error; err != nil {
		return fmt.Errorf("export usage_attempt_facts: %w", err)
	}
	if err := conn.Find(&dump.UsageAggregates).Error; err != nil {
		return fmt.Errorf("export usage_aggregates: %w", err)
	}
	return nil
}

func exportExtendedLogTables(conn *gorm.DB, dump *model.DBDump) error {
	if err := conn.Find(&dump.RelayLogRepairAudits).Error; err != nil {
		return fmt.Errorf("export relay_log_repair_audits: %w", err)
	}
	if err := conn.Find(&dump.SiteOperationAttempts).Error; err != nil {
		return fmt.Errorf("export site_operation_attempts: %w", err)
	}
	return nil
}

func sanitizeSiteAccountsForBackup(accounts []model.SiteAccount) {
	for index := range accounts {
		account := &accounts[index]
		account.VerificationCookieEncrypted = ""
		account.VerificationSessionFenceID = 0
		account.VerificationUserAgent = ""
		account.VerificationProxyConfigID = nil
		account.VerificationClashNode = ""
		account.VerificationExpiresAt = nil
	}
}

func sanitizeSettingsForBackup(settings []model.Setting) []model.Setting {
	result := make([]model.Setting, 0, len(settings))
	for _, setting := range settings {
		switch setting.Key {
		case model.SettingKeyJWTSecret, model.SettingKeyWebDAVPassword:
			continue
		default:
			result = append(result, setting)
		}
	}
	return result
}

func clashControllerBackupFromModel(item model.ClashController) model.ClashControllerBackup {
	return model.ClashControllerBackup{
		ID:        item.ID,
		Name:      item.Name,
		APIURL:    item.APIURL,
		ProxyURL:  item.ProxyURL,
		GroupName: item.GroupName,
		Enabled:   item.Enabled,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}

func writeZipSettings(ctx context.Context, zw *zip.Writer, conn *gorm.DB) error {
	var settings []model.Setting
	if err := conn.WithContext(ctx).Find(&settings).Error; err != nil {
		return fmt.Errorf("zip read settings.json: %w", err)
	}
	return writeZipJSON(zw, "settings.json", sanitizeSettingsForBackup(settings))
}

func writeZipExtendedCoreTables(
	ctx context.Context,
	zw *zip.Writer,
	conn *gorm.DB,
) error {
	if err := writeZipTable(ctx, zw, conn, "canonical_models.json", &[]model.CanonicalModel{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "model_aliases.json", &[]model.ModelAlias{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "route_candidates.json", &[]model.RouteCandidate{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "header_policies.json", &[]model.HeaderPolicy{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "user_agent_profiles.json", &[]model.UserAgentProfile{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "site_model_price_quotes.json", &[]model.SiteModelPriceQuote{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "currency_rates.json", &[]model.CurrencyRate{}); err != nil {
		return err
	}

	var controllers []model.ClashController
	if err := conn.WithContext(ctx).Find(&controllers).Error; err != nil {
		return fmt.Errorf("zip read clash_controllers.json: %w", err)
	}
	items := make([]model.ClashControllerBackup, 0, len(controllers))
	for _, controller := range controllers {
		items = append(items, clashControllerBackupFromModel(controller))
	}
	if err := writeZipJSON(zw, "clash_controllers.json", items); err != nil {
		return err
	}
	return writeZipTable(ctx, zw, conn, "site_proxy_preferences.json", &[]model.SiteProxyPreference{})
}

func writeZipSiteAccounts(ctx context.Context, zw *zip.Writer, conn *gorm.DB) error {
	var accounts []model.SiteAccount
	if err := conn.WithContext(ctx).Find(&accounts).Error; err != nil {
		return fmt.Errorf("zip read site_accounts.json: %w", err)
	}
	sanitizeSiteAccountsForBackup(accounts)
	return writeZipJSON(zw, "site_accounts.json", accounts)
}

func writeZipExtendedStatsTables(ctx context.Context, zw *zip.Writer, conn *gorm.DB) error {
	if err := writeZipUsageRequestFactsNDJSON(ctx, zw, conn); err != nil {
		return err
	}
	if err := writeZipUsageAttemptFactsNDJSON(ctx, zw, conn); err != nil {
		return err
	}
	return writeZipUsageAggregatesNDJSON(ctx, zw, conn)
}

func writeZipExtendedLogTables(ctx context.Context, zw *zip.Writer, conn *gorm.DB) error {
	if err := writeZipTable(ctx, zw, conn, "relay_log_repair_audits.json", &[]model.RelayLogRepairAudit{}); err != nil {
		return err
	}
	return writeZipSiteOperationAttemptsNDJSON(ctx, zw, conn)
}

func writeZipUsageRequestFactsNDJSON(ctx context.Context, zw *zip.Writer, conn *gorm.DB) error {
	encoder, err := newZipNDJSONEncoder(zw, "usage_request_facts.ndjson")
	if err != nil {
		return err
	}
	var lastID int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var batch []model.UsageRequestFact
		if err := conn.WithContext(ctx).
			Where("relay_log_id > ?", lastID).
			Order("relay_log_id ASC").
			Limit(dbExportLogBatchSize).
			Find(&batch).Error; err != nil {
			return fmt.Errorf("zip read usage_request_facts: %w", err)
		}
		if len(batch) == 0 {
			return nil
		}
		for index := range batch {
			if err := encoder.Encode(&batch[index]); err != nil {
				return fmt.Errorf("zip encode usage_request_fact: %w", err)
			}
		}
		lastID = batch[len(batch)-1].RelayLogID
		if len(batch) < dbExportLogBatchSize {
			return nil
		}
	}
}

func writeZipUsageAttemptFactsNDJSON(ctx context.Context, zw *zip.Writer, conn *gorm.DB) error {
	encoder, err := newZipNDJSONEncoder(zw, "usage_attempt_facts.ndjson")
	if err != nil {
		return err
	}
	var lastRelayLogID int64
	var lastAttemptNumber int
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var batch []model.UsageAttemptFact
		query := conn.WithContext(ctx)
		if lastRelayLogID > 0 {
			query = query.Where(
				"relay_log_id > ? OR (relay_log_id = ? AND attempt_number > ?)",
				lastRelayLogID,
				lastRelayLogID,
				lastAttemptNumber,
			)
		}
		if err := query.
			Order("relay_log_id ASC, attempt_number ASC").
			Limit(dbExportLogBatchSize).
			Find(&batch).Error; err != nil {
			return fmt.Errorf("zip read usage_attempt_facts: %w", err)
		}
		if len(batch) == 0 {
			return nil
		}
		for index := range batch {
			if err := encoder.Encode(&batch[index]); err != nil {
				return fmt.Errorf("zip encode usage_attempt_fact: %w", err)
			}
		}
		last := batch[len(batch)-1]
		lastRelayLogID = last.RelayLogID
		lastAttemptNumber = last.AttemptNumber
		if len(batch) < dbExportLogBatchSize {
			return nil
		}
	}
}

func writeZipUsageAggregatesNDJSON(ctx context.Context, zw *zip.Writer, conn *gorm.DB) error {
	encoder, err := newZipNDJSONEncoder(zw, "usage_aggregates.ndjson")
	if err != nil {
		return err
	}
	lastKey := ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var batch []model.UsageAggregate
		query := conn.WithContext(ctx)
		if lastKey != "" {
			query = query.Where("aggregate_key > ?", lastKey)
		}
		if err := query.
			Order("aggregate_key ASC").
			Limit(dbExportLogBatchSize).
			Find(&batch).Error; err != nil {
			return fmt.Errorf("zip read usage_aggregates: %w", err)
		}
		if len(batch) == 0 {
			return nil
		}
		for index := range batch {
			if err := encoder.Encode(&batch[index]); err != nil {
				return fmt.Errorf("zip encode usage_aggregate: %w", err)
			}
		}
		lastKey = batch[len(batch)-1].AggregateKey
		if len(batch) < dbExportLogBatchSize {
			return nil
		}
	}
}

func writeZipSiteOperationAttemptsNDJSON(ctx context.Context, zw *zip.Writer, conn *gorm.DB) error {
	encoder, err := newZipNDJSONEncoder(zw, "site_operation_attempts.ndjson")
	if err != nil {
		return err
	}
	var lastID int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var batch []model.SiteOperationAttempt
		if err := conn.WithContext(ctx).
			Where("id > ?", lastID).
			Order("id ASC").
			Limit(dbExportLogBatchSize).
			Find(&batch).Error; err != nil {
			return fmt.Errorf("zip read site_operation_attempts: %w", err)
		}
		if len(batch) == 0 {
			return nil
		}
		for index := range batch {
			if err := encoder.Encode(&batch[index]); err != nil {
				return fmt.Errorf("zip encode site_operation_attempt: %w", err)
			}
		}
		lastID = batch[len(batch)-1].ID
		if len(batch) < dbExportLogBatchSize {
			return nil
		}
	}
}

func newZipNDJSONEncoder(zw *zip.Writer, name string) (*json.Encoder, error) {
	file, err := zw.Create(name)
	if err != nil {
		return nil, fmt.Errorf("zip create %s: %w", name, err)
	}
	return json.NewEncoder(file), nil
}
