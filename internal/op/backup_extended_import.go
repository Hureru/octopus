package op

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func importClashControllers(
	tx *gorm.DB,
	rows []model.ClashControllerBackup,
	result *model.DBImportResult,
	idMap map[int]int,
) error {
	for _, row := range rows {
		oldID := row.ID
		if err := rejectDuplicateImportSourceID(
			"clash_controllers",
			oldID,
			idMap,
		); err != nil {
			return err
		}
		item := model.ClashController{
			Name:            strings.TrimSpace(row.Name),
			APIURL:          strings.TrimRight(strings.TrimSpace(row.APIURL), "/"),
			ProxyURL:        strings.TrimSpace(row.ProxyURL),
			GroupName:       strings.TrimSpace(row.GroupName),
			SecretEncrypted: row.SecretEncrypted,
			Enabled:         row.Enabled,
			CreatedAt:       row.CreatedAt,
			UpdatedAt:       row.UpdatedAt,
		}
		if item.Name == "" || item.APIURL == "" || item.ProxyURL == "" || item.GroupName == "" {
			return fmt.Errorf("import clash_controllers: required field is empty")
		}
		if err := validateHTTPURL(item.APIURL); err != nil {
			return fmt.Errorf("import clash_controllers: %w", err)
		}
		normalizedProxy, err := model.NormalizeProxyURL(item.ProxyURL)
		if err != nil {
			return fmt.Errorf("import clash_controllers: %w", err)
		}
		item.ProxyURL = normalizedProxy

		var existing model.ClashController
		if err := tx.Where("name = ?", item.Name).First(&existing).Error; err == nil {
			updates := map[string]any{
				"api_url":    item.APIURL,
				"proxy_url":  item.ProxyURL,
				"group_name": item.GroupName,
				"enabled":    item.Enabled,
				"updated_at": item.UpdatedAt,
			}
			if item.SecretEncrypted != "" {
				updates["secret_encrypted"] = item.SecretEncrypted
			}
			if err := tx.Model(&existing).Updates(updates).Error; err != nil {
				return fmt.Errorf("import clash_controllers: %w", err)
			}
			idMap[oldID] = existing.ID
			continue
		} else if err != gorm.ErrRecordNotFound {
			return fmt.Errorf("import clash_controllers: %w", err)
		}
		if err := tx.Create(&item).Error; err != nil {
			return fmt.Errorf("import clash_controllers: %w", err)
		}
		idMap[oldID] = item.ID
		result.RowsAffected["clash_controllers"]++
	}
	return nil
}

func importSiteProxyPreferences(
	tx *gorm.DB,
	rows []model.SiteProxyPreference,
	result *model.DBImportResult,
	siteIDMap map[int]int,
	accountIDMap map[int]int,
	proxyConfigIDMap map[int]int,
	clashControllerIDMap map[int]int,
) error {
	for _, row := range rows {
		siteID, err := requireImportID(
			"site_proxy_preferences",
			"site_id",
			row.SiteID,
			siteIDMap,
		)
		if err != nil {
			return err
		}
		accountID := 0
		if row.SiteAccountID > 0 {
			accountID, err = requireImportID(
				"site_proxy_preferences",
				"site_account_id",
				row.SiteAccountID,
				accountIDMap,
			)
			if err != nil {
				return err
			}
		}
		proxyConfigID := 0
		if row.ProxyConfigID > 0 {
			proxyConfigID, err = requireImportID(
				"site_proxy_preferences",
				"proxy_config_id",
				row.ProxyConfigID,
				proxyConfigIDMap,
			)
			if err != nil {
				return err
			}
		}
		clashControllerID := 0
		if row.ClashControllerID > 0 {
			clashControllerID, err = requireImportID(
				"site_proxy_preferences",
				"clash_controller_id",
				row.ClashControllerID,
				clashControllerIDMap,
			)
			if err != nil {
				return err
			}
		}
		if err := row.ProxyMode.Validate(false); err != nil {
			return fmt.Errorf("import site_proxy_preferences: %w", err)
		}
		if row.ProxyMode == model.ProxyUsageModePool && proxyConfigID == 0 {
			return fmt.Errorf(
				"import site_proxy_preferences: pool path has no imported proxy configuration",
			)
		}

		item := row
		item.ID = 0
		item.SiteID = siteID
		item.SiteAccountID = accountID
		item.ProxyConfigID = proxyConfigID
		item.ClashControllerID = clashControllerID
		item.ClashNode = strings.TrimSpace(item.ClashNode)
		item.IdentityKey = (SiteProxyPathDescriptor{
			SiteID:            siteID,
			SiteAccountID:     accountID,
			ProxyMode:         item.ProxyMode,
			ProxyConfigID:     proxyConfigID,
			ClashControllerID: clashControllerID,
			ClashNode:         item.ClashNode,
		}).IdentityKey()

		var existing model.SiteProxyPreference
		if err := tx.Where("identity_key = ?", item.IdentityKey).First(&existing).Error; err == nil {
			item.ID = existing.ID
			if item.CreatedAt.IsZero() {
				item.CreatedAt = existing.CreatedAt
			}
			if err := tx.Save(&item).Error; err != nil {
				return fmt.Errorf("import site_proxy_preferences: %w", err)
			}
			continue
		} else if err != gorm.ErrRecordNotFound {
			return fmt.Errorf("import site_proxy_preferences: %w", err)
		}
		if err := tx.Create(&item).Error; err != nil {
			return fmt.Errorf("import site_proxy_preferences: %w", err)
		}
		result.RowsAffected["site_proxy_preferences"]++
	}
	return nil
}

func importCanonicalModels(
	tx *gorm.DB,
	rows []model.CanonicalModel,
	result *model.DBImportResult,
	idMap map[int]int,
	groupNameMap map[string]string,
) error {
	for _, row := range rows {
		oldID := row.ID
		if err := rejectDuplicateImportSourceID(
			"canonical_models",
			oldID,
			idMap,
		); err != nil {
			return err
		}
		item := row
		item.ID = 0
		item.Aliases = nil
		item.RouteCandidates = nil
		item.Name = strings.TrimSpace(item.Name)
		sourceName := NormalizeModelIdentity(item.Name)
		item.NormalizedName = NormalizeModelIdentity(item.NormalizedName)
		if item.NormalizedName == "" {
			item.NormalizedName = sourceName
		}
		if item.Name == "" || item.NormalizedName == "" {
			return fmt.Errorf("import canonical_models: name is required")
		}
		item.RoutingStrategy = item.RoutingStrategy.Normalize()
		item.ProtocolPolicy = item.ProtocolPolicy.Normalize(model.ProtocolPolicyAuto)

		var existing model.CanonicalModel
		if err := tx.Where("normalized_name = ?", item.NormalizedName).First(&existing).Error; err == nil {
			idMap[oldID] = existing.ID
			groupNameMap[item.NormalizedName] = existing.Name
			groupNameMap[sourceName] = existing.Name
			if err := importCanonicalMergeAlias(tx, item, existing, result); err != nil {
				return err
			}
			if existing.Manual {
				continue
			}
			updates := map[string]any{
				"routing_strategy": item.RoutingStrategy,
				"protocol_policy":  item.ProtocolPolicy,
				"allow_lossy":      item.AllowLossy,
				"enabled":          item.Enabled,
				"manual":           item.Manual,
				"updated_at":       item.UpdatedAt,
			}
			if err := tx.Model(&existing).Updates(updates).Error; err != nil {
				return fmt.Errorf("import canonical_models: %w", err)
			}
			continue
		} else if err != gorm.ErrRecordNotFound {
			return fmt.Errorf("import canonical_models: %w", err)
		}
		if err := tx.Create(&item).Error; err != nil {
			return fmt.Errorf("import canonical_models: %w", err)
		}
		idMap[oldID] = item.ID
		groupNameMap[item.NormalizedName] = item.Name
		groupNameMap[sourceName] = item.Name
		result.RowsAffected["canonical_models"]++
	}
	return nil
}

func importCanonicalMergeAlias(
	tx *gorm.DB,
	source model.CanonicalModel,
	target model.CanonicalModel,
	result *model.DBImportResult,
) error {
	normalizedAlias := NormalizeModelIdentity(source.Name)
	if source.Name == target.Name ||
		normalizedAlias == "" ||
		normalizedAlias == target.NormalizedName {
		return nil
	}
	var canonicalConflict model.CanonicalModel
	err := tx.Where("normalized_name = ?", normalizedAlias).First(&canonicalConflict).Error
	if err == nil {
		return fmt.Errorf(
			"import canonical_models: source name %q conflicts with canonical model %q",
			source.Name,
			canonicalConflict.Name,
		)
	}
	if err != gorm.ErrRecordNotFound {
		return fmt.Errorf("import canonical_models: resolve source name: %w", err)
	}
	var existing model.ModelAlias
	err = tx.Where("normalized_alias = ?", normalizedAlias).First(&existing).Error
	if err == nil {
		if existing.CanonicalModelID != target.ID {
			return fmt.Errorf(
				"import canonical_models: source name %q is assigned to another canonical model",
				source.Name,
			)
		}
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return fmt.Errorf("import canonical_models: resolve source alias: %w", err)
	}
	alias := model.ModelAlias{
		CanonicalModelID: target.ID,
		Alias:            source.Name,
		NormalizedAlias:  normalizedAlias,
		Manual:           source.Manual,
		CreatedAt:        source.CreatedAt,
		UpdatedAt:        source.UpdatedAt,
	}
	if err := tx.Create(&alias).Error; err != nil {
		return fmt.Errorf("import canonical_models: create source alias: %w", err)
	}
	result.RowsAffected["model_aliases"]++
	return nil
}

func importModelAliases(
	tx *gorm.DB,
	rows []model.ModelAlias,
	result *model.DBImportResult,
	canonicalIDMap map[int]int,
) error {
	for _, row := range rows {
		canonicalID, err := requireImportID(
			"model_aliases",
			"canonical_model_id",
			row.CanonicalModelID,
			canonicalIDMap,
		)
		if err != nil {
			return err
		}
		item := row
		item.ID = 0
		item.CanonicalModelID = canonicalID
		item.Alias = strings.TrimSpace(item.Alias)
		item.NormalizedAlias = NormalizeModelIdentity(item.NormalizedAlias)
		if item.NormalizedAlias == "" {
			item.NormalizedAlias = NormalizeModelIdentity(item.Alias)
		}
		if item.Alias == "" || item.NormalizedAlias == "" {
			continue
		}

		var canonicalConflict model.CanonicalModel
		err = tx.Where("normalized_name = ?", item.NormalizedAlias).First(&canonicalConflict).Error
		if err == nil {
			if canonicalConflict.ID == canonicalID {
				continue
			}
			return fmt.Errorf(
				"import model_aliases: alias %q conflicts with canonical model %q",
				item.Alias,
				canonicalConflict.Name,
			)
		}
		if err != nil && err != gorm.ErrRecordNotFound {
			return fmt.Errorf("import model_aliases: %w", err)
		}

		var existing model.ModelAlias
		if err := tx.Where("normalized_alias = ?", item.NormalizedAlias).First(&existing).Error; err == nil {
			if existing.CanonicalModelID != canonicalID {
				return fmt.Errorf(
					"import model_aliases: alias %q is assigned to another canonical model",
					item.Alias,
				)
			}
			if err := tx.Model(&existing).Updates(map[string]any{
				"alias":      item.Alias,
				"manual":     item.Manual,
				"updated_at": item.UpdatedAt,
			}).Error; err != nil {
				return fmt.Errorf("import model_aliases: %w", err)
			}
			continue
		} else if err != gorm.ErrRecordNotFound {
			return fmt.Errorf("import model_aliases: %w", err)
		}
		if err := tx.Create(&item).Error; err != nil {
			return fmt.Errorf("import model_aliases: %w", err)
		}
		result.RowsAffected["model_aliases"]++
	}
	return nil
}

func importRouteCandidates(
	tx *gorm.DB,
	rows []model.RouteCandidate,
	result *model.DBImportResult,
	canonicalIDMap map[int]int,
	channelIDMap map[int]int,
	siteIDMap map[int]int,
	accountIDMap map[int]int,
	idMap map[int]int,
) error {
	for _, row := range rows {
		oldID := row.ID
		if err := rejectDuplicateImportSourceID(
			"route_candidates",
			oldID,
			idMap,
		); err != nil {
			return err
		}
		canonicalID, err := requireImportID(
			"route_candidates",
			"canonical_model_id",
			row.CanonicalModelID,
			canonicalIDMap,
		)
		if err != nil {
			return err
		}
		channelID, err := requireImportID(
			"route_candidates",
			"channel_id",
			row.ChannelID,
			channelIDMap,
		)
		if err != nil {
			return err
		}
		item := row
		item.ID = 0
		item.CanonicalModelID = canonicalID
		item.ChannelID = channelID
		item.UpstreamModelName = strings.TrimSpace(item.UpstreamModelName)
		if item.UpstreamModelName == "" {
			continue
		}
		if err := remapOptionalImportID(
			"route_candidates",
			"site_id",
			&item.SiteID,
			siteIDMap,
		); err != nil {
			return err
		}
		if err := remapOptionalImportID(
			"route_candidates",
			"site_account_id",
			&item.SiteAccountID,
			accountIDMap,
		); err != nil {
			return err
		}
		var binding model.SiteChannelBinding
		if err := tx.Where("channel_id = ?", item.ChannelID).First(&binding).Error; err == nil {
			siteID := binding.SiteID
			accountID := binding.SiteAccountID
			groupKey, _ := model.ParseSiteChannelBindingKey(binding.GroupKey)
			item.SiteID = &siteID
			item.SiteAccountID = &accountID
			item.SiteGroupKey = groupKey
		} else if err != gorm.ErrRecordNotFound {
			return fmt.Errorf("import route_candidates: resolve channel binding: %w", err)
		}

		var existing model.RouteCandidate
		err = tx.Where(
			"canonical_model_id = ? AND channel_id = ? AND upstream_model_name = ?",
			item.CanonicalModelID,
			item.ChannelID,
			item.UpstreamModelName,
		).First(&existing).Error
		if err == nil {
			idMap[oldID] = existing.ID
			if existing.Manual {
				continue
			}
			item.ID = existing.ID
			if item.CreatedAt.IsZero() {
				item.CreatedAt = existing.CreatedAt
			}
			if err := tx.Save(&item).Error; err != nil {
				return fmt.Errorf("import route_candidates: %w", err)
			}
			continue
		}
		if err != gorm.ErrRecordNotFound {
			return fmt.Errorf("import route_candidates: %w", err)
		}
		if err := tx.Create(&item).Error; err != nil {
			return fmt.Errorf("import route_candidates: %w", err)
		}
		idMap[oldID] = item.ID
		result.RowsAffected["route_candidates"]++
	}
	return nil
}

func importHeaderPolicies(
	tx *gorm.DB,
	rows []model.HeaderPolicy,
	result *model.DBImportResult,
	siteIDMap map[int]int,
	accountIDMap map[int]int,
	channelIDMap map[int]int,
	canonicalIDMap map[int]int,
	routeCandidateIDMap map[int]int,
) error {
	for _, row := range rows {
		item := row
		item.ID = 0
		scopeID, ok := remapHeaderPolicyScopeID(
			item.Scope,
			item.ScopeID,
			siteIDMap,
			accountIDMap,
			channelIDMap,
			canonicalIDMap,
			routeCandidateIDMap,
		)
		if !ok {
			return fmt.Errorf(
				"import header_policies: scope %q id %d has no imported parent",
				item.Scope,
				item.ScopeID,
			)
		}
		item.ScopeID = scopeID
		if err := validateHeaderPolicy(&item); err != nil {
			return fmt.Errorf("import header_policies: %w", err)
		}

		var existing model.HeaderPolicy
		err := tx.Where("scope = ? AND scope_id = ?", item.Scope, item.ScopeID).First(&existing).Error
		if err == nil {
			item.ID = existing.ID
			item.CreatedAt = existing.CreatedAt
			if item.UpdatedAt.IsZero() {
				item.UpdatedAt = existing.UpdatedAt
			}
			if err := tx.Model(&existing).
				Select(
					"Name",
					"Version",
					"Enabled",
					"ForwardClientHeaders",
					"UserAgent",
					"SetHeaders",
					"UnsetHeaders",
					"AllowedClientHeaders",
					"UpdatedAt",
				).
				Updates(&item).Error; err != nil {
				return fmt.Errorf("import header_policies: %w", err)
			}
			continue
		}
		if err != gorm.ErrRecordNotFound {
			return fmt.Errorf("import header_policies: %w", err)
		}
		if err := tx.Create(&item).Error; err != nil {
			return fmt.Errorf("import header_policies: %w", err)
		}
		result.RowsAffected["header_policies"]++
	}
	return nil
}

func importUserAgentProfiles(
	tx *gorm.DB,
	rows []model.UserAgentProfile,
	result *model.DBImportResult,
) error {
	for _, row := range rows {
		item := row
		item.ID = 0
		item.Name = strings.TrimSpace(item.Name)
		item.Value = strings.TrimSpace(item.Value)
		if item.Name == "" || item.Value == "" {
			continue
		}
		var existing model.UserAgentProfile
		if err := tx.Where("name = ?", item.Name).First(&existing).Error; err == nil {
			if err := tx.Model(&existing).Updates(map[string]any{
				"value":      item.Value,
				"built_in":   item.BuiltIn,
				"updated_at": item.UpdatedAt,
			}).Error; err != nil {
				return fmt.Errorf("import user_agent_profiles: %w", err)
			}
			continue
		} else if err != gorm.ErrRecordNotFound {
			return fmt.Errorf("import user_agent_profiles: %w", err)
		}
		if err := tx.Create(&item).Error; err != nil {
			return fmt.Errorf("import user_agent_profiles: %w", err)
		}
		result.RowsAffected["user_agent_profiles"]++
	}
	return nil
}

func importCurrencyRates(
	tx *gorm.DB,
	rows []model.CurrencyRate,
	result *model.DBImportResult,
) error {
	for _, row := range rows {
		item := row
		item.Currency = strings.ToUpper(strings.TrimSpace(item.Currency))
		if item.Currency == "" || item.RateToUSD <= 0 {
			continue
		}
		if item.Currency == "USD" {
			item.RateToUSD = 1
		}
		var existing model.CurrencyRate
		if err := tx.First(&existing, "currency = ?", item.Currency).Error; err == nil {
			if err := tx.Model(&existing).Updates(map[string]any{
				"rate_to_usd": item.RateToUSD,
				"updated_at":  item.UpdatedAt,
			}).Error; err != nil {
				return fmt.Errorf("import currency_rates: %w", err)
			}
			continue
		} else if err != gorm.ErrRecordNotFound {
			return fmt.Errorf("import currency_rates: %w", err)
		}
		if err := tx.Create(&item).Error; err != nil {
			return fmt.Errorf("import currency_rates: %w", err)
		}
		result.RowsAffected["currency_rates"]++
	}
	return nil
}

func importSiteModelPriceQuotes(
	tx *gorm.DB,
	rows []model.SiteModelPriceQuote,
	result *model.DBImportResult,
	siteIDMap map[int]int,
	accountIDMap map[int]int,
	routeCandidateIDMap map[int]int,
	idMap map[int]int,
) error {
	for _, row := range rows {
		oldID := row.ID
		if err := rejectDuplicateImportSourceID(
			"site_model_price_quotes",
			oldID,
			idMap,
		); err != nil {
			return err
		}
		siteID, err := requireImportID(
			"site_model_price_quotes",
			"site_id",
			row.SiteID,
			siteIDMap,
		)
		if err != nil {
			return err
		}
		item := row
		item.ID = 0
		item.SiteID = siteID
		if item.SiteAccountID != nil {
			accountID, err := requireImportID(
				"site_model_price_quotes",
				"site_account_id",
				*item.SiteAccountID,
				accountIDMap,
			)
			if err != nil {
				return err
			}
			item.SiteAccountID = &accountID
		}
		if err := remapOptionalImportID(
			"site_model_price_quotes",
			"route_candidate_id",
			&item.RouteCandidateID,
			routeCandidateIDMap,
		); err != nil {
			return err
		}
		normalizePriceQuoteBackup(&item)
		if item.ModelName == "" {
			continue
		}

		var existing model.SiteModelPriceQuote
		if err := tx.Where("identity_key = ?", item.IdentityKey).First(&existing).Error; err == nil {
			item.ID = existing.ID
			if item.CreatedAt.IsZero() {
				item.CreatedAt = existing.CreatedAt
			}
			if err := tx.Save(&item).Error; err != nil {
				return fmt.Errorf("import site_model_price_quotes: %w", err)
			}
			idMap[oldID] = existing.ID
			continue
		} else if err != gorm.ErrRecordNotFound {
			return fmt.Errorf("import site_model_price_quotes: %w", err)
		}
		if err := tx.Create(&item).Error; err != nil {
			return fmt.Errorf("import site_model_price_quotes: %w", err)
		}
		idMap[oldID] = item.ID
		result.RowsAffected["site_model_price_quotes"]++
	}
	return nil
}

func importRelayLogRepairAudits(
	tx *gorm.DB,
	rows []model.RelayLogRepairAudit,
	result *model.DBImportResult,
) error {
	for _, row := range rows {
		item := row
		item.ID = 0
		var count int64
		if err := tx.Model(&model.RelayLogRepairAudit{}).
			Where("batch_id = ? AND dry_run = ?", item.BatchID, item.DryRun).
			Count(&count).Error; err != nil {
			return fmt.Errorf("import relay_log_repair_audits: %w", err)
		}
		if count > 0 {
			continue
		}
		if err := tx.Create(&item).Error; err != nil {
			return fmt.Errorf("import relay_log_repair_audits: %w", err)
		}
		result.RowsAffected["relay_log_repair_audits"]++
	}
	return nil
}

func importSiteOperationAttempts(
	tx *gorm.DB,
	rows []model.SiteOperationAttempt,
	result *model.DBImportResult,
	siteIDMap map[int]int,
	accountIDMap map[int]int,
	proxyConfigIDMap map[int]int,
	clashControllerIDMap map[int]int,
) error {
	for _, row := range rows {
		siteID, err := requireImportID(
			"site_operation_attempts",
			"site_id",
			row.SiteID,
			siteIDMap,
		)
		if err != nil {
			return err
		}
		accountID, err := requireImportID(
			"site_operation_attempts",
			"site_account_id",
			row.SiteAccountID,
			accountIDMap,
		)
		if err != nil {
			return err
		}
		item := row
		item.ID = 0
		item.SiteID = siteID
		item.SiteAccountID = accountID
		if err := remapOptionalImportID(
			"site_operation_attempts",
			"proxy_config_id",
			&item.ProxyConfigID,
			proxyConfigIDMap,
		); err != nil {
			return err
		}
		if err := remapOptionalImportID(
			"site_operation_attempts",
			"clash_controller_id",
			&item.ClashControllerID,
			clashControllerIDMap,
		); err != nil {
			return err
		}

		var count int64
		if err := tx.Model(&model.SiteOperationAttempt{}).Where(
			"site_id = ? AND site_account_id = ? AND operation = ? AND attempt_number = ? AND started_at = ?",
			item.SiteID,
			item.SiteAccountID,
			item.Operation,
			item.AttemptNumber,
			item.StartedAt,
		).Count(&count).Error; err != nil {
			return fmt.Errorf("import site_operation_attempts: %w", err)
		}
		if count > 0 {
			continue
		}
		if err := tx.Create(&item).Error; err != nil {
			return fmt.Errorf("import site_operation_attempts: %w", err)
		}
		result.RowsAffected["site_operation_attempts"]++
	}
	return nil
}

func remapRelayLogsForImport(
	tx *gorm.DB,
	rows []model.RelayLog,
	channelIDMap map[int]int,
	channelKeyIDMap map[int]int,
	apiKeyIDMap map[int]int,
	routeCandidateIDMap map[int]int,
	priceQuoteIDMap map[int]int,
) ([]model.RelayLog, map[int64]int64, error) {
	result := make([]model.RelayLog, len(rows))
	relayLogIDMap := make(map[int64]int64, len(rows))
	copy(result, rows)
	for rowIndex := range result {
		result[rowIndex].Attempts = append(
			[]model.ChannelAttempt(nil),
			rows[rowIndex].Attempts...,
		)
		for attemptIndex := range result[rowIndex].Attempts {
			if rows[rowIndex].Attempts[attemptIndex].Usage == nil {
				continue
			}
			usage := *rows[rowIndex].Attempts[attemptIndex].Usage
			result[rowIndex].Attempts[attemptIndex].Usage = &usage
		}
	}
	for index := range result {
		row := &result[index]
		oldID := row.ID
		if row.TokenSource == "" {
			row.TokenSource = model.UsageValueSourceUnknown
		}
		row.ChannelId = remapRequiredID(row.ChannelId, channelIDMap)
		row.RequestAPIKeyID = remapRequiredID(row.RequestAPIKeyID, apiKeyIDMap)
		row.RouteCandidateID = remapRequiredID(row.RouteCandidateID, routeCandidateIDMap)
		row.PriceQuoteID = remapRequiredID(row.PriceQuoteID, priceQuoteIDMap)
		for attemptIndex := range row.Attempts {
			attempt := &row.Attempts[attemptIndex]
			attempt.ChannelID = remapRequiredID(attempt.ChannelID, channelIDMap)
			attempt.ChannelKeyID = remapRequiredID(attempt.ChannelKeyID, channelKeyIDMap)
			attempt.RouteCandidateID = remapRequiredID(
				attempt.RouteCandidateID,
				routeCandidateIDMap,
			)
			if attempt.Usage != nil {
				attempt.Usage.PriceQuoteID = remapRequiredID(
					attempt.Usage.PriceQuoteID,
					priceQuoteIDMap,
				)
			}
		}
		newID, err := resolveRelayLogImportID(tx, *row)
		if err != nil {
			return nil, nil, err
		}
		row.ID = newID
		relayLogIDMap[oldID] = newID
	}
	return result, relayLogIDMap, nil
}

func resolveRelayLogImportID(tx *gorm.DB, row model.RelayLog) (int64, error) {
	fingerprint, err := relayLogImportFingerprint(row)
	if err != nil {
		return 0, err
	}
	if row.ID > 0 {
		var existing model.RelayLog
		err := tx.First(&existing, "id = ?", row.ID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return row.ID, nil
		}
		if err != nil {
			return 0, fmt.Errorf("import relay_logs: resolve id: %w", err)
		}
		existingFingerprint, fingerprintErr := relayLogImportFingerprint(existing)
		if fingerprintErr != nil {
			return 0, fingerprintErr
		}
		if bytes.Equal(existingFingerprint, fingerprint) {
			return row.ID, nil
		}
	}

	for salt := uint64(0); salt < 100; salt++ {
		candidate := deterministicRelayLogImportID(row.ID, fingerprint, salt)
		var existing model.RelayLog
		err := tx.First(&existing, "id = ?", candidate).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return candidate, nil
		}
		if err != nil {
			return 0, fmt.Errorf("import relay_logs: resolve deterministic id: %w", err)
		}
		existingFingerprint, fingerprintErr := relayLogImportFingerprint(existing)
		if fingerprintErr != nil {
			return 0, fingerprintErr
		}
		if bytes.Equal(existingFingerprint, fingerprint) {
			return candidate, nil
		}
	}
	return 0, fmt.Errorf("import relay_logs: unable to allocate deterministic id for %d", row.ID)
}

func relayLogImportFingerprint(row model.RelayLog) ([]byte, error) {
	row.ID = 0
	if row.TokenSource == "" {
		row.TokenSource = model.UsageValueSourceUnknown
	}
	payload, err := json.Marshal(row)
	if err != nil {
		return nil, fmt.Errorf("import relay_logs: fingerprint: %w", err)
	}
	sum := sha256.Sum256(payload)
	return sum[:], nil
}

func deterministicRelayLogImportID(sourceID int64, fingerprint []byte, salt uint64) int64 {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("octopus-relay-import-v1\x00"))
	var value [8]byte
	binary.BigEndian.PutUint64(value[:], uint64(sourceID))
	_, _ = hasher.Write(value[:])
	_, _ = hasher.Write(fingerprint)
	binary.BigEndian.PutUint64(value[:], salt)
	_, _ = hasher.Write(value[:])
	sum := hasher.Sum(nil)
	id := binary.BigEndian.Uint64(sum[:8]) & ((uint64(1) << 62) - 1)
	id |= uint64(1) << 62
	return int64(id)
}

func resolveUsageRequestFactImportID(
	tx *gorm.DB,
	row model.UsageRequestFact,
) (int64, error) {
	fingerprint, err := usageRequestFactImportFingerprint(row)
	if err != nil {
		return 0, err
	}
	return resolveUsageFactImportID(
		tx,
		row.RelayLogID,
		fingerprint,
		func(candidate int64) (bool, bool, error) {
			var existing model.UsageRequestFact
			err := tx.First(&existing, "relay_log_id = ?", candidate).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return false, false, nil
			}
			if err != nil {
				return false, false, err
			}
			existingFingerprint, err := usageRequestFactImportFingerprint(existing)
			return true, bytes.Equal(existingFingerprint, fingerprint), err
		},
	)
}

func resolveUsageAttemptFactImportID(
	tx *gorm.DB,
	row model.UsageAttemptFact,
) (int64, error) {
	fingerprint, err := usageAttemptFactImportFingerprint(row)
	if err != nil {
		return 0, err
	}
	return resolveUsageFactImportID(
		tx,
		row.RelayLogID,
		fingerprint,
		func(candidate int64) (bool, bool, error) {
			var existing model.UsageAttemptFact
			err := tx.First(
				&existing,
				"relay_log_id = ? AND attempt_number = ?",
				candidate,
				row.AttemptNumber,
			).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return false, false, nil
			}
			if err != nil {
				return false, false, err
			}
			existingFingerprint, err := usageAttemptFactImportFingerprint(existing)
			return true, bytes.Equal(existingFingerprint, fingerprint), err
		},
	)
}

func resolveUsageFactImportID(
	tx *gorm.DB,
	sourceID int64,
	fingerprint []byte,
	findMatch func(int64) (bool, bool, error),
) (int64, error) {
	if sourceID <= 0 {
		return 0, fmt.Errorf("import usage facts: relay_log_id must be positive")
	}
	candidates := make([]int64, 0, 101)
	candidates = append(candidates, sourceID)
	for salt := uint64(0); salt < 100; salt++ {
		candidates = append(candidates, deterministicUsageFactImportID(sourceID, fingerprint, salt))
	}
	for _, candidate := range candidates {
		exists, matches, err := findMatch(candidate)
		if err != nil {
			return 0, fmt.Errorf("import usage facts: resolve id: %w", err)
		}
		if matches {
			return candidate, nil
		}
		if exists {
			continue
		}
		occupied, err := usageRelayLogIDOccupied(tx, candidate)
		if err != nil {
			return 0, err
		}
		if !occupied {
			return candidate, nil
		}
	}
	return 0, fmt.Errorf("import usage facts: unable to allocate deterministic id for %d", sourceID)
}

func usageRelayLogIDOccupied(tx *gorm.DB, relayLogID int64) (bool, error) {
	checks := []struct {
		table any
		field string
	}{
		{table: &model.RelayLog{}, field: "id"},
		{table: &model.UsageRequestFact{}, field: "relay_log_id"},
		{table: &model.UsageAttemptFact{}, field: "relay_log_id"},
	}
	for _, check := range checks {
		var count int64
		if err := tx.Model(check.table).
			Where(check.field+" = ?", relayLogID).
			Count(&count).Error; err != nil {
			return false, fmt.Errorf("import usage facts: check id collision: %w", err)
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}

func usageRequestFactImportFingerprint(row model.UsageRequestFact) ([]byte, error) {
	row.RelayLogID = 0
	if row.TokenSource == "" {
		row.TokenSource = model.UsageValueSourceUnknown
	}
	payload, err := json.Marshal(row)
	if err != nil {
		return nil, fmt.Errorf("import usage_request_facts: fingerprint: %w", err)
	}
	sum := sha256.Sum256(payload)
	return sum[:], nil
}

func usageAttemptFactImportFingerprint(row model.UsageAttemptFact) ([]byte, error) {
	row.RelayLogID = 0
	if row.TokenSource == "" {
		row.TokenSource = model.UsageValueSourceUnknown
	}
	payload, err := json.Marshal(row)
	if err != nil {
		return nil, fmt.Errorf("import usage_attempt_facts: fingerprint: %w", err)
	}
	sum := sha256.Sum256(payload)
	return sum[:], nil
}

func validateUsageRequestFactImportTarget(tx *gorm.DB, row model.UsageRequestFact) error {
	var existing model.UsageRequestFact
	err := tx.First(&existing, "relay_log_id = ?", row.RelayLogID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("import usage_request_facts: validate target: %w", err)
	}
	sourceFingerprint, err := usageRequestFactImportFingerprint(row)
	if err != nil {
		return err
	}
	targetFingerprint, err := usageRequestFactImportFingerprint(existing)
	if err != nil {
		return err
	}
	if !bytes.Equal(sourceFingerprint, targetFingerprint) {
		return fmt.Errorf(
			"import usage_request_facts: target identity collision for relay_log_id %d",
			row.RelayLogID,
		)
	}
	return nil
}

func validateUsageAttemptFactImportTarget(tx *gorm.DB, row model.UsageAttemptFact) error {
	var existing model.UsageAttemptFact
	err := tx.First(
		&existing,
		"relay_log_id = ? AND attempt_number = ?",
		row.RelayLogID,
		row.AttemptNumber,
	).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("import usage_attempt_facts: validate target: %w", err)
	}
	sourceFingerprint, err := usageAttemptFactImportFingerprint(row)
	if err != nil {
		return err
	}
	targetFingerprint, err := usageAttemptFactImportFingerprint(existing)
	if err != nil {
		return err
	}
	if !bytes.Equal(sourceFingerprint, targetFingerprint) {
		return fmt.Errorf(
			"import usage_attempt_facts: target identity collision for %d/%d",
			row.RelayLogID,
			row.AttemptNumber,
		)
	}
	return nil
}

func deterministicUsageFactImportID(sourceID int64, fingerprint []byte, salt uint64) int64 {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("octopus-usage-import-v1\x00"))
	var value [8]byte
	binary.BigEndian.PutUint64(value[:], uint64(sourceID))
	_, _ = hasher.Write(value[:])
	_, _ = hasher.Write(fingerprint)
	binary.BigEndian.PutUint64(value[:], salt)
	_, _ = hasher.Write(value[:])
	sum := hasher.Sum(nil)
	id := binary.BigEndian.Uint64(sum[:8]) & ((uint64(1) << 62) - 1)
	id |= uint64(1) << 62
	return int64(id)
}

func importUsageTables(
	tx *gorm.DB,
	dump *model.DBDump,
	result *model.DBImportResult,
	siteIDMap map[int]int,
	accountIDMap map[int]int,
	channelIDMap map[int]int,
	apiKeyIDMap map[int]int,
	routeCandidateIDMap map[int]int,
	priceQuoteIDMap map[int]int,
	relayLogIDMap map[int64]int64,
	clearAggregatedAt bool,
) error {
	requestFacts := append([]model.UsageRequestFact(nil), dump.UsageRequestFacts...)
	attemptFacts := append([]model.UsageAttemptFact(nil), dump.UsageAttemptFacts...)
	aggregates := append([]model.UsageAggregate(nil), dump.UsageAggregates...)

	for index := range requestFacts {
		row := &requestFacts[index]
		sourceID := row.RelayLogID
		remapUsageDimensions(
			&row.SiteID,
			&row.SiteAccountID,
			&row.ChannelID,
			&row.APIKeyID,
			&row.RouteCandidateID,
			siteIDMap,
			accountIDMap,
			channelIDMap,
			apiKeyIDMap,
			routeCandidateIDMap,
		)
		row.PriceQuoteID = remapRequiredID(row.PriceQuoteID, priceQuoteIDMap)
		if clearAggregatedAt {
			row.AggregatedAt = nil
		}
		if remapped, ok := relayLogIDMap[row.RelayLogID]; ok {
			row.RelayLogID = remapped
		} else {
			remapped, err := resolveUsageRequestFactImportID(tx, *row)
			if err != nil {
				return err
			}
			relayLogIDMap[sourceID] = remapped
			row.RelayLogID = remapped
		}
	}
	for index := range attemptFacts {
		row := &attemptFacts[index]
		sourceID := row.RelayLogID
		remapUsageDimensions(
			&row.SiteID,
			&row.SiteAccountID,
			&row.ChannelID,
			&row.APIKeyID,
			&row.RouteCandidateID,
			siteIDMap,
			accountIDMap,
			channelIDMap,
			apiKeyIDMap,
			routeCandidateIDMap,
		)
		row.PriceQuoteID = remapRequiredID(row.PriceQuoteID, priceQuoteIDMap)
		if clearAggregatedAt {
			row.AggregatedAt = nil
		}
		if remapped, ok := relayLogIDMap[row.RelayLogID]; ok {
			row.RelayLogID = remapped
		} else {
			remapped, err := resolveUsageAttemptFactImportID(tx, *row)
			if err != nil {
				return err
			}
			relayLogIDMap[sourceID] = remapped
			row.RelayLogID = remapped
		}
	}
	for index := range aggregates {
		row := &aggregates[index]
		row.SiteID = remapRequiredID(row.SiteID, siteIDMap)
		row.SiteAccountID = remapRequiredID(row.SiteAccountID, accountIDMap)
		row.ChannelID = remapRequiredID(row.ChannelID, channelIDMap)
		row.APIKeyID = remapRequiredID(row.APIKeyID, apiKeyIDMap)
		row.AggregateKey = usageAggregateKey(
			usageAggregateFact{
				scope:          UsageMetricScope(row.MetricScope),
				siteID:         row.SiteID,
				siteAccountID:  row.SiteAccountID,
				channelID:      row.ChannelID,
				apiKeyID:       row.APIKeyID,
				requestModel:   row.RequestModel,
				actualModel:    row.ActualModel,
				canonicalModel: row.CanonicalModel,
			},
			row.Granularity,
			row.BucketStart,
		)
	}

	for _, row := range requestFacts {
		if err := validateUsageRequestFactImportTarget(tx, row); err != nil {
			return err
		}
	}
	for _, row := range attemptFacts {
		if err := validateUsageAttemptFactImportTarget(tx, row); err != nil {
			return err
		}
	}
	if n, err := createDoNothing(tx, requestFacts); err != nil {
		return fmt.Errorf("import usage_request_facts: %w", err)
	} else {
		result.RowsAffected["usage_request_facts"] += n
	}
	if n, err := createDoNothing(tx, attemptFacts); err != nil {
		return fmt.Errorf("import usage_attempt_facts: %w", err)
	} else {
		result.RowsAffected["usage_attempt_facts"] += n
	}
	if n, err := createUpsertAll(tx, aggregates, []clause.Column{{Name: "aggregate_key"}}); err != nil {
		return fmt.Errorf("import usage_aggregates: %w", err)
	} else {
		result.RowsAffected["usage_aggregates"] += n
	}
	return nil
}

func remapUsageDimensions(
	siteID *int,
	accountID *int,
	channelID *int,
	apiKeyID *int,
	routeCandidateID *int,
	siteIDMap map[int]int,
	accountIDMap map[int]int,
	channelIDMap map[int]int,
	apiKeyIDMap map[int]int,
	routeCandidateIDMap map[int]int,
) {
	*siteID = remapRequiredID(*siteID, siteIDMap)
	*accountID = remapRequiredID(*accountID, accountIDMap)
	*channelID = remapRequiredID(*channelID, channelIDMap)
	*apiKeyID = remapRequiredID(*apiKeyID, apiKeyIDMap)
	*routeCandidateID = remapRequiredID(*routeCandidateID, routeCandidateIDMap)
}

func remapHeaderPolicyScopeID(
	scope model.HeaderPolicyScope,
	scopeID int,
	siteIDMap map[int]int,
	accountIDMap map[int]int,
	channelIDMap map[int]int,
	canonicalIDMap map[int]int,
	routeCandidateIDMap map[int]int,
) (int, bool) {
	switch scope {
	case model.HeaderPolicyScopeGlobal:
		return 0, true
	case model.HeaderPolicyScopeSite:
		value, ok := siteIDMap[scopeID]
		return value, ok
	case model.HeaderPolicyScopeSiteAccount:
		value, ok := accountIDMap[scopeID]
		return value, ok
	case model.HeaderPolicyScopeChannel:
		value, ok := channelIDMap[scopeID]
		return value, ok
	case model.HeaderPolicyScopeCanonicalModel:
		value, ok := canonicalIDMap[scopeID]
		return value, ok
	case model.HeaderPolicyScopeRouteCandidate:
		value, ok := routeCandidateIDMap[scopeID]
		return value, ok
	default:
		return 0, false
	}
}

func normalizePriceQuoteBackup(item *model.SiteModelPriceQuote) {
	item.ModelName = strings.TrimSpace(item.ModelName)
	item.GroupKey = model.NormalizeSiteGroupKey(item.GroupKey)
	if item.Source == "" {
		item.Source = model.PriceQuoteSourceSiteExact
	}
	if item.Unit == "" {
		item.Unit = model.PriceUnitPerMillionTokens
	}
	item.Currency = strings.ToUpper(strings.TrimSpace(item.Currency))
	if item.Currency == "" {
		if item.Unit == model.PriceUnitSiteCredit {
			item.Currency = "SITE_CREDIT"
		} else {
			item.Currency = "USD"
		}
	}
	if item.GroupMultiplier == 0 {
		item.GroupMultiplier = 1
	}
	if item.Currency == "USD" && item.ExchangeRateToUSD == 0 {
		item.ExchangeRateToUSD = 1
	}
	if item.ObservedAt.IsZero() {
		item.ObservedAt = time.Now()
	}
	if item.Source == model.PriceQuoteSourceManualOverride {
		item.ManualOverride = true
	}
	if err := validateSiteModelPriceQuote(*item); err != nil {
		item.Status = model.PriceQuoteStatusRejected
		item.LastError = err.Error()
	} else if item.Status == "" {
		item.Status = model.PriceQuoteStatusValid
	}
	item.RefreshIdentityKey()
}

func remapRequiredID(value int, idMap map[int]int) int {
	if value <= 0 {
		return 0
	}
	if remapped, ok := idMap[value]; ok {
		return remapped
	}
	return 0
}

func requireImportID(
	table string,
	field string,
	sourceID int,
	idMap map[int]int,
) (int, error) {
	if sourceID > 0 {
		if targetID, ok := idMap[sourceID]; ok {
			return targetID, nil
		}
	}
	return 0, fmt.Errorf(
		"import %s: %s %d has no imported parent",
		table,
		field,
		sourceID,
	)
}

func rejectDuplicateImportSourceID(
	table string,
	sourceID int,
	idMap map[int]int,
) error {
	if _, exists := idMap[sourceID]; exists {
		return fmt.Errorf("import %s: duplicate source id %d", table, sourceID)
	}
	return nil
}

func remapOptionalImportID(
	table string,
	field string,
	value **int,
	idMap map[int]int,
) error {
	if value == nil || *value == nil {
		return nil
	}
	targetID, err := requireImportID(table, field, **value, idMap)
	if err != nil {
		return err
	}
	*value = &targetID
	return nil
}
