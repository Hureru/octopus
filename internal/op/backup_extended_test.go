package op

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

type extendedBackupSeed struct {
	controller model.ClashController
	proxy      model.ProxyConfiguration
	channel    model.Channel
	site       model.Site
	account    model.SiteAccount
	apiKey     model.APIKey
	canonical  model.CanonicalModel
	candidate  model.RouteCandidate
	quote      model.SiteModelPriceQuote
}

func TestDBExportImportExtendedDataRoundTrip(t *testing.T) {
	ctx := setupBackupTestDB(t)
	seed := seedExtendedBackupData(t, ctx)

	dump, err := DBExportAll(ctx, true, true)
	if err != nil {
		t.Fatalf("DBExportAll failed: %v", err)
	}
	if dump.Version != dbDumpVersion {
		t.Fatalf("dump version = %d, want %d", dump.Version, dbDumpVersion)
	}
	if len(dump.ClashControllers) != 1 || dump.ClashControllers[0].SecretEncrypted != "" {
		t.Fatalf("ordinary backup exported a controller secret: %+v", dump.ClashControllers)
	}
	for _, setting := range dump.Settings {
		if setting.Key == model.SettingKeyJWTSecret || setting.Key == model.SettingKeyWebDAVPassword {
			t.Fatalf("ordinary backup exported sensitive setting %s", setting.Key)
		}
	}
	if len(dump.SiteAccounts) != 1 ||
		dump.SiteAccounts[0].VerificationCookieEncrypted != "" ||
		dump.SiteAccounts[0].VerificationUserAgent != "" ||
		dump.SiteAccounts[0].VerificationProxyConfigID != nil ||
		dump.SiteAccounts[0].VerificationExpiresAt != nil {
		t.Fatalf("verification state leaked into ordinary backup: %+v", dump.SiteAccounts)
	}

	payload, err := json.Marshal(dump)
	if err != nil {
		t.Fatalf("marshal dump failed: %v", err)
	}
	if strings.Contains(string(payload), "verification-cookie") {
		t.Fatal("verification cookie leaked into JSON backup")
	}
	var restored model.DBDump
	if err := json.Unmarshal(payload, &restored); err != nil {
		t.Fatalf("unmarshal dump failed: %v", err)
	}
	if len(restored.ClashControllers) != 1 || restored.ClashControllers[0].SecretEncrypted != "" {
		t.Fatalf("controller secret reappeared in JSON round trip: %+v", restored.ClashControllers)
	}

	_ = dbpkg.Close()
	freshDBPath := filepath.Join(t.TempDir(), "octopus-extended-restore.db")
	if err := dbpkg.InitDB("sqlite", freshDBPath, false); err != nil {
		t.Fatalf("InitDB for restore failed: %v", err)
	}
	seedDestinationIDCollisions(t)

	result, err := DBImportIncremental(ctx, &restored)
	if err != nil {
		t.Fatalf("DBImportIncremental failed: %v", err)
	}
	for _, table := range []string{
		"clash_controllers",
		"canonical_models",
		"model_aliases",
		"route_candidates",
		"header_policies",
		"user_agent_profiles",
		"site_model_price_quotes",
		"currency_rates",
		"site_proxy_preferences",
		"usage_request_facts",
		"usage_attempt_facts",
		"usage_aggregates",
		"relay_log_repair_audits",
		"site_operation_attempts",
	} {
		if result.RowsAffected[table] != 1 && table != "header_policies" {
			t.Fatalf("%s rows affected = %d, want 1", table, result.RowsAffected[table])
		}
	}
	if result.RowsAffected["header_policies"] != 3 {
		t.Fatalf("header_policies rows affected = %d, want 3", result.RowsAffected["header_policies"])
	}
	if len(result.Warnings) != 1 ||
		result.Warnings[0].Code != "credential_not_restored" ||
		result.Warnings[0].ResourceName != seed.controller.Name {
		t.Fatalf("missing controller credential warning: %+v", result.Warnings)
	}

	assertExtendedBackupRelations(t, seed)

	second, err := DBImportIncremental(ctx, &restored)
	if err != nil {
		t.Fatalf("second DBImportIncremental failed: %v", err)
	}
	for _, table := range []string{
		"clash_controllers",
		"canonical_models",
		"model_aliases",
		"route_candidates",
		"site_proxy_preferences",
		"relay_logs",
		"usage_request_facts",
		"usage_attempt_facts",
		"relay_log_repair_audits",
		"site_operation_attempts",
	} {
		if second.RowsAffected[table] != 0 {
			t.Fatalf("%s was not idempotent: %d rows affected", table, second.RowsAffected[table])
		}
	}
}

func TestDBImportVersionCompatibility(t *testing.T) {
	ctx := setupBackupTestDB(t)
	for _, version := range []int{0, 1, dbDumpVersion} {
		dump := &model.DBDump{Version: version}
		if _, err := DBImportIncremental(ctx, dump); err != nil {
			t.Fatalf("version %d should be supported: %v", version, err)
		}
	}
	if _, err := DBImportIncremental(ctx, &model.DBDump{Version: dbDumpVersion + 1}); err == nil {
		t.Fatal("future dump version should be rejected")
	}
}

func TestDBImportLegacyHeaderPolicyMetadataIsStable(t *testing.T) {
	ctx := setupBackupTestDB(t)
	dump := &model.DBDump{
		Version: dbDumpVersion,
		HeaderPolicies: []model.HeaderPolicy{{
			Scope:   model.HeaderPolicyScopeGlobal,
			Enabled: true,
		}},
	}

	if _, err := DBImportIncremental(ctx, dump); err != nil {
		t.Fatalf("import legacy header policy: %v", err)
	}
	var first model.HeaderPolicy
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("scope = ? AND scope_id = ?", model.HeaderPolicyScopeGlobal, 0).
		First(&first).Error; err != nil {
		t.Fatalf("load imported header policy: %v", err)
	}
	if first.Name != model.HeaderPolicyDefaultName(model.HeaderPolicyScopeGlobal, 0) ||
		first.Version != 1 {
		t.Fatalf("legacy metadata was not normalized: %+v", first)
	}

	if _, err := DBImportIncremental(ctx, dump); err != nil {
		t.Fatalf("re-import legacy header policy: %v", err)
	}
	var second model.HeaderPolicy
	if err := dbpkg.GetDB().WithContext(ctx).First(&second, first.ID).Error; err != nil {
		t.Fatalf("reload imported header policy: %v", err)
	}
	if second.Name != first.Name || second.Version != first.Version {
		t.Fatalf("idempotent import changed policy metadata: first=%+v second=%+v", first, second)
	}
}

func TestDBImportZipLegacyInlineProxiesRemainUniqueAcrossBatchesAndTables(t *testing.T) {
	ctx := setupBackupTestDB(t)
	channels := make([]map[string]any, 0, dbImportBatchSize+1)
	for index := 0; index <= dbImportBatchSize; index++ {
		channels = append(channels, map[string]any{
			"id":            index + 1,
			"name":          fmt.Sprintf("legacy-proxy-channel-%d", index),
			"enabled":       true,
			"proxy":         true,
			"channel_proxy": fmt.Sprintf("http://127.0.0.1:%d", 7000+index),
		})
	}
	payload := buildBackupImportZip(t, backupZipManifest{
		Version:    1,
		ExportedAt: "2026-07-28T00:00:00Z",
		Format:     "zip-v1",
	}, map[string]any{
		"channels.json": channels,
		"sites.json": []map[string]any{{
			"id":         100,
			"name":       "legacy-proxy-site",
			"platform":   model.SitePlatformNewAPI,
			"base_url":   "https://legacy-proxy.example.com",
			"enabled":    true,
			"proxy":      true,
			"site_proxy": "http://127.0.0.1:7999",
		}},
		"site_accounts.json": []map[string]any{{
			"id":              200,
			"site_id":         100,
			"name":            "legacy-proxy-account",
			"credential_type": model.SiteCredentialTypeAPIKey,
			"api_key":         "legacy-proxy-key",
			"enabled":         true,
			"account_proxy":   "http://127.0.0.1:8999",
		}},
	}, nil)

	result, err := DBImportZip(ctx, bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("DBImportZip failed: %v", err)
	}
	if result.RowsAffected["proxy_configurations"] != dbImportBatchSize+3 {
		t.Fatalf("legacy proxy rows = %d, want %d",
			result.RowsAffected["proxy_configurations"], dbImportBatchSize+3)
	}
	conn := dbpkg.GetDB().WithContext(ctx)
	var channelsWithoutProxy int64
	if err := conn.Model(&model.Channel{}).
		Where("name LIKE ? AND (proxy_mode <> ? OR proxy_config_id IS NULL)",
			"legacy-proxy-channel-%", model.ProxyUsageModePool).
		Count(&channelsWithoutProxy).Error; err != nil {
		t.Fatalf("count legacy channels failed: %v", err)
	}
	if channelsWithoutProxy != 0 {
		t.Fatalf("%d legacy channels lost their proxy mapping", channelsWithoutProxy)
	}
	var site model.Site
	mustFindBackupRow(t, conn.Where("name = ?", "legacy-proxy-site"), &site)
	if site.ProxyMode != model.ProxyUsageModePool || site.ProxyConfigID == nil {
		t.Fatalf("legacy site lost its proxy mapping: %+v", site)
	}
	var account model.SiteAccount
	mustFindBackupRow(t, conn.Where("name = ?", "legacy-proxy-account"), &account)
	if account.ProxyMode != model.ProxyUsageModePool || account.ProxyConfigID == nil {
		t.Fatalf("legacy account lost its proxy mapping: %+v", account)
	}
	second, err := DBImportZip(ctx, bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("second DBImportZip failed: %v", err)
	}
	for _, table := range []string{"proxy_configurations", "channels", "sites", "site_accounts"} {
		if second.RowsAffected[table] != 0 {
			t.Fatalf("%s was not idempotent: %+v", table, second.RowsAffected)
		}
	}
}

func TestDBImportZipLegacyRelayLogTokenSourceIsIdempotent(t *testing.T) {
	ctx := setupBackupTestDB(t)
	payload := buildBackupImportZip(t, backupZipManifest{
		Version:     1,
		ExportedAt:  "2026-07-28T00:00:00Z",
		IncludeLogs: true,
		Format:      "zip-v1",
	}, nil, map[string]string{
		"relay_logs.ndjson": `{"id":1001,"time":1,"request_model_name":"legacy-token-source","success":true,"outcome":"success"}` + "\n",
	})

	first, err := DBImportZip(ctx, bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("first DBImportZip failed: %v", err)
	}
	second, err := DBImportZip(ctx, bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("second DBImportZip failed: %v", err)
	}
	if first.RowsAffected["relay_logs"] != 1 || second.RowsAffected["relay_logs"] != 0 {
		t.Fatalf("legacy relay log import was not idempotent: first=%+v second=%+v",
			first.RowsAffected, second.RowsAffected)
	}
	var count int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.RelayLog{}).
		Where("request_model_name = ?", "legacy-token-source").
		Count(&count).Error; err != nil {
		t.Fatalf("count legacy relay logs failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("legacy relay log count = %d, want 1", count)
	}
}

func TestDBImportPreservesTargetJWTAndReencryptsClashSecrets(t *testing.T) {
	ctx := setupBackupTestDB(t)
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("refresh source settings: %v", err)
	}
	if err := SettingSetString(model.SettingKeyJWTSecret, "source-jwt-secret"); err != nil {
		t.Fatalf("set source jwt: %v", err)
	}
	encrypted, err := EncryptSecret("clash-controller-secret")
	if err != nil {
		t.Fatalf("encrypt source secret: %v", err)
	}
	dump := &model.DBDump{
		Version: dbDumpVersion,
		Settings: []model.Setting{{
			Key: model.SettingKeyJWTSecret, Value: "source-jwt-secret",
		}},
		ClashControllers: []model.ClashControllerBackup{{
			ID: 1, Name: "secure-controller",
			APIURL: "http://127.0.0.1:9090", ProxyURL: "http://127.0.0.1:7890",
			GroupName: "OCTOPUS", SecretEncrypted: encrypted, Enabled: true,
		}},
	}

	_ = dbpkg.Close()
	targetPath := filepath.Join(t.TempDir(), "target-jwt.db")
	if err := dbpkg.InitDB("sqlite", targetPath, false); err != nil {
		t.Fatalf("InitDB target: %v", err)
	}
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("refresh target settings: %v", err)
	}
	if err := SettingSetString(model.SettingKeyJWTSecret, "target-jwt-secret"); err != nil {
		t.Fatalf("set target jwt: %v", err)
	}
	if _, err := DBImportIncremental(ctx, dump); err != nil {
		t.Fatalf("DBImportIncremental failed: %v", err)
	}
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("refresh settings: %v", err)
	}
	jwt, err := SettingGetString(model.SettingKeyJWTSecret)
	if err != nil {
		t.Fatalf("read target jwt: %v", err)
	}
	if jwt != "target-jwt-secret" {
		t.Fatalf("target jwt was overwritten: %q", jwt)
	}
	var controller model.ClashController
	mustFindBackupRow(t, dbpkg.GetDB().Where("name = ?", "secure-controller"), &controller)
	plain, err := DecryptSecret(controller.SecretEncrypted)
	if err != nil {
		t.Fatalf("decrypt imported controller secret: %v", err)
	}
	if plain != "clash-controller-secret" {
		t.Fatalf("imported secret = %q", plain)
	}
	if controller.SecretEncrypted == encrypted {
		t.Fatal("controller secret was not re-encrypted for the target instance")
	}
}

func TestDBImportPreservesJWTWhitespaceWhenReencryptingClashSecrets(t *testing.T) {
	ctx := setupBackupTestDB(t)
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("refresh source settings: %v", err)
	}
	const sourceJWT = " source-jwt-secret "
	if err := SettingSetString(model.SettingKeyJWTSecret, sourceJWT); err != nil {
		t.Fatalf("set source jwt: %v", err)
	}
	encrypted, err := EncryptSecret("whitespace-controller-secret")
	if err != nil {
		t.Fatalf("encrypt source secret: %v", err)
	}
	dump := &model.DBDump{
		Version: dbDumpVersion,
		Settings: []model.Setting{{
			Key: model.SettingKeyJWTSecret, Value: sourceJWT,
		}},
		ClashControllers: []model.ClashControllerBackup{{
			ID: 1, Name: "whitespace-controller",
			APIURL: "http://127.0.0.1:9090", ProxyURL: "http://127.0.0.1:7890",
			GroupName: "OCTOPUS", SecretEncrypted: encrypted, Enabled: true,
		}},
	}

	_ = dbpkg.Close()
	targetPath := filepath.Join(t.TempDir(), "target-whitespace-jwt.db")
	if err := dbpkg.InitDB("sqlite", targetPath, false); err != nil {
		t.Fatalf("InitDB target: %v", err)
	}
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("refresh target settings: %v", err)
	}
	const targetJWT = " target-jwt-secret "
	if err := SettingSetString(model.SettingKeyJWTSecret, targetJWT); err != nil {
		t.Fatalf("set target jwt: %v", err)
	}
	if _, err := DBImportIncremental(ctx, dump); err != nil {
		t.Fatalf("DBImportIncremental failed: %v", err)
	}
	var controller model.ClashController
	mustFindBackupRow(t, dbpkg.GetDB().Where("name = ?", "whitespace-controller"), &controller)
	plain, err := DecryptSecret(controller.SecretEncrypted)
	if err != nil {
		t.Fatalf("decrypt imported controller secret: %v", err)
	}
	if plain != "whitespace-controller-secret" {
		t.Fatalf("imported secret = %q", plain)
	}
}

func TestDBImportRejectsChildRowsWithoutImportedParents(t *testing.T) {
	ctx := setupBackupTestDB(t)
	conn := dbpkg.GetDB().WithContext(ctx)
	targetChannel := model.Channel{Name: "unrelated-target-channel", Enabled: true}
	mustCreateBackupRow(t, conn, &targetChannel)

	_, err := DBImportIncremental(ctx, &model.DBDump{
		Version: dbDumpVersion,
		ChannelKeys: []model.ChannelKey{{
			ID: 100, ChannelID: targetChannel.ID,
			ChannelKey: "must-not-attach", Enabled: true,
		}},
	})
	if err == nil {
		t.Fatal("child row without an imported parent should be rejected")
	}
	var count int64
	if err := conn.Model(&model.ChannelKey{}).
		Where("channel_key = ?", "must-not-attach").
		Count(&count).Error; err != nil {
		t.Fatalf("count channel keys failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("orphaned child attached to unrelated target channel: %d", count)
	}
}

func TestDBImportRejectsUnmappedPositiveObservabilityDimensions(t *testing.T) {
	tests := []struct {
		name  string
		field string
		dump  *model.DBDump
	}{
		{
			name:  "relay log channel",
			field: "import relay_logs: channel_id 999 has no imported parent",
			dump: &model.DBDump{
				Version:     dbDumpVersion,
				IncludeLogs: true,
				Sites: []model.Site{{
					ID: 1, Name: "rollback-site", Platform: model.SitePlatformNewAPI,
					BaseURL: "https://rollback.example.com", Enabled: true,
				}},
				RelayLogs: []model.RelayLog{{
					ID: 10, Time: 10, ChannelId: 999, Outcome: model.RequestOutcomeSuccess,
				}},
			},
		},
		{
			name:  "usage request site",
			field: "import usage_request_facts: site_id 999 has no imported parent",
			dump: &model.DBDump{
				Version:      dbDumpVersion,
				IncludeStats: true,
				UsageRequestFacts: []model.UsageRequestFact{{
					RelayLogID: 10, Time: 10, SiteID: 999, Outcome: model.RequestOutcomeSuccess,
				}},
			},
		},
		{
			name:  "usage aggregate api key",
			field: "import usage_aggregates: api_key_id 999 has no imported parent",
			dump: &model.DBDump{
				Version:      dbDumpVersion,
				IncludeStats: true,
				UsageAggregates: []model.UsageAggregate{{
					AggregateKey: "source", Granularity: model.UsageAggregateHourly,
					MetricScope: string(UsageMetricScopeRequest), BucketStart: 10, APIKeyID: 999,
				}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := setupBackupTestDB(t)
			if _, err := DBImportIncremental(ctx, test.dump); err == nil ||
				!strings.Contains(err.Error(), test.field) {
				t.Fatalf("unmapped positive dimension was not rejected: %v", err)
			}
			var siteCount int64
			if err := dbpkg.GetDB().WithContext(ctx).Model(&model.Site{}).
				Count(&siteCount).Error; err != nil {
				t.Fatalf("count sites after rollback: %v", err)
			}
			if siteCount != 0 {
				t.Fatalf("failed import left partial parent rows: %d", siteCount)
			}
		})
	}

	targetID, err := remapImportID("usage_request_facts", "site_id", 0, nil)
	if err != nil || targetID != 0 {
		t.Fatalf("zero optional dimension should remain zero: id=%d err=%v", targetID, err)
	}
}

func TestDBImportDoesNotMutateReusableLegacyDump(t *testing.T) {
	ctx := setupBackupTestDB(t)
	proxyURL := "http://127.0.0.1:18888"
	dump := &model.DBDump{
		Version: dbDumpVersion,
		Channels: []model.Channel{{
			ID:           100,
			Name:         "legacy-proxy-channel",
			Enabled:      true,
			Proxy:        true,
			ChannelProxy: &proxyURL,
		}},
	}
	before := *dump
	before.Channels = append([]model.Channel(nil), dump.Channels...)
	before.ProxyConfigurations = append(
		[]model.ProxyConfiguration(nil),
		dump.ProxyConfigurations...,
	)

	if _, err := DBImportIncremental(ctx, dump); err != nil {
		t.Fatalf("first legacy import failed: %v", err)
	}
	if !reflect.DeepEqual(*dump, before) {
		t.Fatal("legacy import mutated the caller-owned dump")
	}
	if _, err := DBImportIncremental(ctx, dump); err != nil {
		t.Fatalf("reusing the same legacy dump failed: %v", err)
	}
	if !reflect.DeepEqual(*dump, before) {
		t.Fatal("second legacy import mutated the caller-owned dump")
	}
}

func TestDBImportRejectsPoolProxyWithoutImportedParent(t *testing.T) {
	tests := []struct {
		name string
		dump *model.DBDump
	}{
		{
			name: "channel",
			dump: &model.DBDump{
				Version: dbDumpVersion,
				Channels: []model.Channel{{
					ID: 100, Name: "missing-proxy-channel", Enabled: true,
					ProxyMode: model.ProxyUsageModePool, ProxyConfigID: intPtr(999),
				}},
			},
		},
		{
			name: "site",
			dump: &model.DBDump{
				Version: dbDumpVersion,
				Sites: []model.Site{{
					ID: 100, Name: "missing-proxy-site",
					Platform: model.SitePlatformNewAPI, BaseURL: "https://missing-proxy.example.com",
					Enabled: true, ProxyMode: model.ProxyUsageModePool, ProxyConfigID: intPtr(999),
				}},
			},
		},
		{
			name: "site account",
			dump: &model.DBDump{
				Version: dbDumpVersion,
				Sites: []model.Site{{
					ID: 100, Name: "missing-account-proxy-site",
					Platform: model.SitePlatformNewAPI, BaseURL: "https://missing-account-proxy.example.com",
					Enabled: true,
				}},
				SiteAccounts: []model.SiteAccount{{
					ID: 200, SiteID: 100, Name: "missing-proxy-account",
					CredentialType: model.SiteCredentialTypeAPIKey, APIKey: "missing-proxy-key",
					Enabled: true, ProxyMode: model.ProxyUsageModePool, ProxyConfigID: intPtr(999),
				}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := setupBackupTestDB(t)
			if _, err := DBImportIncremental(ctx, test.dump); err == nil ||
				!strings.Contains(err.Error(), "proxy_config_id 999 has no imported parent") {
				t.Fatalf("missing pool proxy parent was not rejected: %v", err)
			}
		})
	}
}

func TestDBImportHydratesCandidateScopeFromTargetChannelBinding(t *testing.T) {
	ctx := setupBackupTestDB(t)
	conn := dbpkg.GetDB().WithContext(ctx)
	targetSite := model.Site{
		Name: "target-site", Platform: model.SitePlatformNewAPI,
		BaseURL: "https://target.example.com", Enabled: true,
	}
	mustCreateBackupRow(t, conn, &targetSite)
	targetAccount := model.SiteAccount{
		SiteID: targetSite.ID, Name: "target-account",
		CredentialType: model.SiteCredentialTypeAPIKey,
		APIKey:         "target-key", Enabled: true,
	}
	mustCreateBackupRow(t, conn, &targetAccount)
	targetChannel := model.Channel{Name: "shared-channel", Enabled: true}
	mustCreateBackupRow(t, conn, &targetChannel)
	mustCreateBackupRow(t, conn, &model.SiteChannelBinding{
		SiteID: targetSite.ID, SiteAccountID: targetAccount.ID,
		GroupKey: "target-group", ChannelID: targetChannel.ID,
	})

	dump := &model.DBDump{
		Version: dbDumpVersion,
		Channels: []model.Channel{{
			ID: 100, Name: targetChannel.Name, Enabled: true,
		}},
		Sites: []model.Site{{
			ID: 200, Name: "source-site", Platform: model.SitePlatformNewAPI,
			BaseURL: "https://source.example.com", Enabled: true,
		}},
		SiteAccounts: []model.SiteAccount{{
			ID: 300, SiteID: 200, Name: "source-account",
			CredentialType: model.SiteCredentialTypeAPIKey,
			APIKey:         "source-key", Enabled: true,
		}},
		SiteChannelBindings: []model.SiteChannelBinding{{
			ID: 400, SiteID: 200, SiteAccountID: 300,
			GroupKey: "source-group", ChannelID: 100,
		}},
		CanonicalModels: []model.CanonicalModel{{
			ID: 500, Name: "Shared Model", NormalizedName: "shared-model", Enabled: true,
		}},
		RouteCandidates: []model.RouteCandidate{{
			ID: 600, CanonicalModelID: 500, ChannelID: 100,
			UpstreamModelName: "provider/shared-model",
			SiteID:            intPtr(200), SiteAccountID: intPtr(300),
			SiteGroupKey: "source-group", Status: model.RouteCandidateActive,
		}},
	}
	if _, err := DBImportIncremental(ctx, dump); err != nil {
		t.Fatalf("DBImportIncremental failed: %v", err)
	}
	var candidate model.RouteCandidate
	mustFindBackupRow(t, conn.Where(
		"channel_id = ? AND upstream_model_name = ?",
		targetChannel.ID,
		"provider/shared-model",
	), &candidate)
	if candidate.SiteID == nil || *candidate.SiteID != targetSite.ID ||
		candidate.SiteAccountID == nil || *candidate.SiteAccountID != targetAccount.ID ||
		candidate.SiteGroupKey != "target-group" {
		t.Fatalf("candidate scope diverged from target binding: %+v", candidate)
	}
}

func TestDBImportCanonicalMergePreservesTargetIdentityAndAliasesSourceName(t *testing.T) {
	ctx := setupBackupTestDB(t)
	conn := dbpkg.GetDB().WithContext(ctx)
	target := model.CanonicalModel{
		Name: "model-1", NormalizedName: "model-1", Enabled: true,
	}
	mustCreateBackupRow(t, conn, &target)

	_, err := DBImportIncremental(ctx, &model.DBDump{
		Version: dbDumpVersion,
		CanonicalModels: []model.CanonicalModel{{
			ID: 100, Name: "model.1", NormalizedName: "model-1", Enabled: true,
		}},
	})
	if err != nil {
		t.Fatalf("DBImportIncremental failed: %v", err)
	}

	var reloaded model.CanonicalModel
	mustFindBackupRow(t, conn.Where("id = ?", target.ID), &reloaded)
	if reloaded.Name != target.Name ||
		reloaded.NormalizedName != target.NormalizedName {
		t.Fatalf("import changed target canonical identity: %+v", reloaded)
	}
	var alias model.ModelAlias
	mustFindBackupRow(t, conn.Where("normalized_alias = ?", "model.1"), &alias)
	if alias.CanonicalModelID != target.ID || alias.Alias != "model.1" {
		t.Fatalf("source canonical name was not retained as an alias: %+v", alias)
	}
}

func TestDBImportCanonicalMergeKeepsGroupProjectionReachable(t *testing.T) {
	ctx := setupBackupTestDB(t)
	conn := dbpkg.GetDB().WithContext(ctx)
	channel := model.Channel{
		Name: "canonical-merge-channel", Model: "upstream-model", Enabled: true,
	}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	target := model.CanonicalModel{
		Name:            "Stable-Model",
		NormalizedName:  "stable-model",
		RoutingStrategy: model.RoutingStrategyManual,
		ProtocolPolicy:  model.ProtocolPolicyAuto,
		Enabled:         true,
	}
	mustCreateBackupRow(t, conn, &target)
	targetGroup := model.Group{Name: target.Name, Mode: model.GroupModeFailover}
	mustCreateBackupRow(t, conn, &targetGroup)

	dump := &model.DBDump{
		Version: dbDumpVersion,
		Channels: []model.Channel{{
			ID: 100, Name: channel.Name, Model: channel.Model, Enabled: true,
		}},
		CanonicalModels: []model.CanonicalModel{{
			ID: 200, Name: "stable-model", NormalizedName: "stable-model", Enabled: true,
		}},
		ModelAliases: []model.ModelAlias{{
			ID: 250, CanonicalModelID: 200,
			Alias: "upstream-model", NormalizedAlias: "upstream-model",
		}},
		Groups: []model.Group{{
			ID: 300, Name: "stable-model", Mode: model.GroupModeFailover,
		}},
		GroupItems: []model.GroupItem{{
			ID: 400, GroupID: 300, ChannelID: 100,
			ModelName: "upstream-model", Priority: 1, Weight: 1,
		}},
		RouteCandidates: []model.RouteCandidate{{
			ID: 500, CanonicalModelID: 200, ChannelID: 100,
			UpstreamModelName: "upstream-model",
			Status:            model.RouteCandidateActive,
			Weight:            1,
		}},
	}
	if _, err := DBImportIncremental(ctx, dump); err != nil {
		t.Fatalf("DBImportIncremental failed: %v", err)
	}
	if err := InitCache(); err != nil {
		t.Fatalf("InitCache after import failed: %v", err)
	}
	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("CatalogSync after import failed: %v", err)
	}

	var canonical model.CanonicalModel
	mustFindBackupRow(t, conn.Where("normalized_name = ?", target.NormalizedName), &canonical)
	if canonical.ID != target.ID || canonical.Name != target.Name {
		t.Fatalf("target canonical identity was not preserved: %+v", canonical)
	}
	var groups []model.Group
	if err := conn.Preload("Items").Find(&groups).Error; err != nil {
		t.Fatalf("load groups failed: %v", err)
	}
	if len(groups) != 1 || groups[0].ID != targetGroup.ID || len(groups[0].Items) != 1 {
		t.Fatalf("source group projection was not merged into target: %+v", groups)
	}
	planned, _, resolved, err := CatalogPlanGroup(
		ctx,
		target.Name,
		model.ProtocolRouteRequirements{InboundProtocol: model.ProtocolOpenAIChat},
		groups[0],
	)
	if err != nil {
		t.Fatalf("CatalogPlanGroup failed: %v", err)
	}
	if resolved == nil || resolved.ID != target.ID || len(planned.Items) != 1 {
		t.Fatalf("imported candidate is unreachable: canonical=%+v items=%+v", resolved, planned.Items)
	}
}

func TestDBImportPreservesManualCandidateAndSkipsSelfAlias(t *testing.T) {
	ctx := setupBackupTestDB(t)
	conn := dbpkg.GetDB().WithContext(ctx)
	channel := model.Channel{Name: "manual-candidate-channel", Enabled: true}
	mustCreateBackupRow(t, conn, &channel)
	canonical := model.CanonicalModel{
		Name: "manual-model", NormalizedName: "manual-model", Enabled: true,
	}
	mustCreateBackupRow(t, conn, &canonical)
	target := model.RouteCandidate{
		CanonicalModelID:  canonical.ID,
		ChannelID:         channel.ID,
		UpstreamModelName: "upstream-model",
		Status:            model.RouteCandidateDisabled,
		Priority:          9,
		Weight:            7,
		Manual:            true,
	}
	mustCreateBackupRow(t, conn, &target)

	dump := &model.DBDump{
		Version: dbDumpVersion,
		Channels: []model.Channel{{
			ID: 100, Name: channel.Name, Enabled: true,
		}},
		CanonicalModels: []model.CanonicalModel{{
			ID: 200, Name: canonical.Name, NormalizedName: canonical.NormalizedName, Enabled: true,
		}},
		ModelAliases: []model.ModelAlias{{
			ID: 300, CanonicalModelID: 200,
			Alias: canonical.Name, NormalizedAlias: canonical.NormalizedName,
		}},
		RouteCandidates: []model.RouteCandidate{{
			ID: 400, CanonicalModelID: 200, ChannelID: 100,
			UpstreamModelName: target.UpstreamModelName,
			Status:            model.RouteCandidateActive,
			Priority:          1,
			Weight:            1,
			Manual:            false,
		}},
	}
	if _, err := DBImportIncremental(ctx, dump); err != nil {
		t.Fatalf("DBImportIncremental failed: %v", err)
	}

	var reloaded model.RouteCandidate
	mustFindBackupRow(t, conn.Where("id = ?", target.ID), &reloaded)
	if !reloaded.Manual ||
		reloaded.Status != model.RouteCandidateDisabled ||
		reloaded.Priority != target.Priority ||
		reloaded.Weight != target.Weight {
		t.Fatalf("automatic import overwrote manual candidate: %+v", reloaded)
	}
	var aliasCount int64
	if err := conn.Model(&model.ModelAlias{}).Count(&aliasCount).Error; err != nil {
		t.Fatalf("count aliases failed: %v", err)
	}
	if aliasCount != 0 {
		t.Fatalf("canonical self-alias was imported: %d", aliasCount)
	}
}

func TestDBExportZipIncludesExtendedTablesWithoutVerificationState(t *testing.T) {
	ctx := setupBackupTestDB(t)
	seed := seedExtendedBackupData(t, ctx)

	var buffer bytesBuffer
	if err := DBExportZip(ctx, &buffer, true, true); err != nil {
		t.Fatalf("DBExportZip failed: %v", err)
	}
	reader, err := zipReaderFromBytes(buffer.Bytes())
	if err != nil {
		t.Fatalf("open ZIP failed: %v", err)
	}
	names := make(map[string]bool, len(reader.File))
	for _, file := range reader.File {
		names[file.Name] = true
	}
	for _, required := range []string{
		"canonical_models.json",
		"model_aliases.json",
		"route_candidates.json",
		"header_policies.json",
		"user_agent_profiles.json",
		"site_model_price_quotes.json",
		"currency_rates.json",
		"clash_controllers.json",
		"site_proxy_preferences.json",
		"usage_request_facts.ndjson",
		"usage_attempt_facts.ndjson",
		"usage_aggregates.ndjson",
		"relay_log_repair_audits.json",
		"site_operation_attempts.ndjson",
	} {
		if !names[required] {
			t.Fatalf("ZIP missing %q (have %v)", required, names)
		}
	}

	controllers := readZipFile(t, reader, "clash_controllers.json")
	if strings.Contains(controllers, seed.controller.SecretEncrypted) {
		t.Fatal("ZIP exported the encrypted Clash secret")
	}
	settings := readZipFile(t, reader, "settings.json")
	if strings.Contains(settings, "source-backup-jwt-secret") {
		t.Fatal("ZIP exported the JWT secret")
	}
	accounts := readZipFile(t, reader, "site_accounts.json")
	if strings.Contains(accounts, "verification-cookie") || strings.Contains(accounts, "verification-agent") {
		t.Fatalf("verification state leaked into ZIP: %s", accounts)
	}
	if linesCount(readZipFile(t, reader, "usage_request_facts.ndjson")) != 1 {
		t.Fatal("ZIP request facts were not streamed")
	}
	if linesCount(readZipFile(t, reader, "usage_attempt_facts.ndjson")) != 1 {
		t.Fatal("ZIP attempt facts were not streamed")
	}
}

func TestDBExportZipCanBeImported(t *testing.T) {
	ctx := setupBackupTestDB(t)
	seed := seedExtendedBackupData(t, ctx)
	var buffer bytesBuffer
	if err := DBExportZip(ctx, &buffer, true, true); err != nil {
		t.Fatalf("DBExportZip failed: %v", err)
	}

	_ = dbpkg.Close()
	targetPath := filepath.Join(t.TempDir(), "zip-restore.db")
	if err := dbpkg.InitDB("sqlite", targetPath, false); err != nil {
		t.Fatalf("InitDB target: %v", err)
	}
	seedDestinationIDCollisions(t)
	payload := buffer.Bytes()
	result, err := DBImportZip(ctx, bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("DBImportZip failed: %v", err)
	}
	if result.RowsAffected["route_candidates"] != 1 ||
		result.RowsAffected["usage_request_facts"] != 1 {
		t.Fatalf("unexpected ZIP import result: %+v", result)
	}
	assertExtendedBackupRelations(t, seed)
}

func TestDBImportZipStreamsBatchesAndRemapsUsageFacts(t *testing.T) {
	ctx := setupBackupTestDB(t)
	conn := dbpkg.GetDB().WithContext(ctx)
	targetChannel := model.Channel{Name: "stream-import-channel", Enabled: true}
	mustCreateBackupRow(t, conn, &targetChannel)
	for id := int64(1); id <= 45; id++ {
		mustCreateBackupRow(t, conn, &model.RelayLog{
			ID: id, Time: id, RequestModelName: fmt.Sprintf("collision-%d", id),
			ChannelId: targetChannel.ID, Outcome: model.RequestOutcomeFailed,
		})
	}

	logs := make([]model.RelayLog, 0, 45)
	facts := make([]model.UsageRequestFact, 0, 45)
	for id := int64(1); id <= 45; id++ {
		logs = append(logs, model.RelayLog{
			ID: id, Time: 1_000 + id,
			RequestModelName: fmt.Sprintf("stream-source-%d", id),
			RequestAPIKeyID:  200,
			ChannelId:        100,
			Success:          true,
			Outcome:          model.RequestOutcomeSuccess,
		})
		facts = append(facts, model.UsageRequestFact{
			RelayLogID: id,
			Time:       1_000 + id,
			ChannelID:  100,
			APIKeyID:   200,
			Outcome:    model.RequestOutcomeSuccess,
		})
	}
	payload := buildBackupImportZip(t, backupZipManifest{
		Version:      dbDumpVersion,
		ExportedAt:   "2026-07-28T00:00:00Z",
		IncludeLogs:  true,
		IncludeStats: true,
		Format:       "zip-v1",
	}, map[string]any{
		"channels.json": []model.Channel{{
			ID: 100, Name: targetChannel.Name, Enabled: true,
		}},
		"api_keys.json": []model.APIKey{{
			ID: 200, Name: "stream-import-key", APIKey: "stream-import-secret", Enabled: true,
		}},
	}, map[string]string{
		"relay_logs.ndjson":          encodeBackupNDJSON(t, logs),
		"usage_request_facts.ndjson": encodeBackupNDJSON(t, facts),
		"usage_aggregates.ndjson":    "",
	})

	result, err := DBImportZip(ctx, bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("DBImportZip failed: %v", err)
	}
	if result.RowsAffected["relay_logs"] != 45 ||
		result.RowsAffected["usage_request_facts"] != 45 {
		t.Fatalf("streamed batch counts were not accumulated: %+v", result.RowsAffected)
	}
	var importedLogs int64
	if err := conn.Model(&model.RelayLog{}).
		Where("request_model_name LIKE ?", "stream-source-%").
		Count(&importedLogs).Error; err != nil {
		t.Fatalf("count imported logs failed: %v", err)
	}
	if importedLogs != 45 {
		t.Fatalf("imported logs = %d, want 45", importedLogs)
	}
	var factsWithSourceID int64
	if err := conn.Model(&model.UsageRequestFact{}).
		Where("relay_log_id BETWEEN ? AND ?", 1, 45).
		Count(&factsWithSourceID).Error; err != nil {
		t.Fatalf("count source fact ids failed: %v", err)
	}
	if factsWithSourceID != 0 {
		t.Fatalf("%d usage facts kept colliding source relay ids", factsWithSourceID)
	}
}

func TestDBImportZipRollsBackEarlierBatchesOnLateDecodeError(t *testing.T) {
	ctx := setupBackupTestDB(t)
	logs := make([]model.RelayLog, 0, dbImportBatchSize+1)
	for index := 0; index <= dbImportBatchSize; index++ {
		logs = append(logs, model.RelayLog{
			ID: int64(1_000 + index), Time: int64(index + 1),
			RequestModelName: fmt.Sprintf("rollback-source-%d", index),
			ChannelId:        100,
			Outcome:          model.RequestOutcomeSuccess,
		})
	}
	brokenLogs := encodeBackupNDJSON(t, logs) + "{"
	payload := buildBackupImportZip(t, backupZipManifest{
		Version:     dbDumpVersion,
		ExportedAt:  "2026-07-28T00:00:00Z",
		IncludeLogs: true,
		Format:      "zip-v1",
	}, map[string]any{
		"channels.json": []model.Channel{{
			ID: 100, Name: "rollback-stream-channel", Enabled: true,
		}},
	}, map[string]string{"relay_logs.ndjson": brokenLogs})

	if _, err := DBImportZip(ctx, bytes.NewReader(payload), int64(len(payload))); err == nil {
		t.Fatal("late NDJSON corruption should fail the import")
	}
	conn := dbpkg.GetDB().WithContext(ctx)
	var channelCount int64
	if err := conn.Model(&model.Channel{}).
		Where("name = ?", "rollback-stream-channel").
		Count(&channelCount).Error; err != nil {
		t.Fatalf("count channels failed: %v", err)
	}
	var logCount int64
	if err := conn.Model(&model.RelayLog{}).
		Where("request_model_name LIKE ?", "rollback-source-%").
		Count(&logCount).Error; err != nil {
		t.Fatalf("count logs failed: %v", err)
	}
	if channelCount != 0 || logCount != 0 {
		t.Fatalf("failed stream import partially committed: channels=%d logs=%d", channelCount, logCount)
	}
}

func TestDBImportZipStatsOnlyRemapsCollidingUsageFacts(t *testing.T) {
	ctx := setupBackupTestDB(t)
	conn := dbpkg.GetDB().WithContext(ctx)
	mustCreateBackupRow(t, conn, &model.RelayLog{
		ID: 1, Time: 1, RequestModelName: "target-log",
		Outcome: model.RequestOutcomeSuccess,
	})
	mustCreateBackupRow(t, conn, &model.UsageRequestFact{
		RelayLogID: 1, Time: 1, RequestModel: "target-request",
		Outcome: model.RequestOutcomeSuccess,
	})
	mustCreateBackupRow(t, conn, &model.UsageAttemptFact{
		RelayLogID: 1, AttemptNumber: 1, Time: 1,
		RequestModel: "target-request", Status: model.AttemptSuccess,
		Outcome: model.RequestOutcomeSuccess,
	})
	requestFacts := []model.UsageRequestFact{{
		RelayLogID: 1, Time: 2, RequestModel: "source-request",
		Outcome: model.RequestOutcomeSuccess,
	}}
	attemptFacts := []model.UsageAttemptFact{{
		RelayLogID: 1, AttemptNumber: 1, Time: 2,
		RequestModel: "source-request", Status: model.AttemptSuccess,
		Outcome: model.RequestOutcomeSuccess,
	}}
	payload := buildBackupImportZip(t, backupZipManifest{
		Version:      dbDumpVersion,
		ExportedAt:   "2026-07-28T00:00:00Z",
		IncludeStats: true,
		Format:       "zip-v1",
	}, nil, map[string]string{
		"usage_aggregates.ndjson":    "",
		"usage_request_facts.ndjson": encodeBackupNDJSON(t, requestFacts),
		"usage_attempt_facts.ndjson": encodeBackupNDJSON(t, attemptFacts),
	})

	result, err := DBImportZip(ctx, bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("DBImportZip failed: %v", err)
	}
	if result.RowsAffected["usage_request_facts"] != 1 ||
		result.RowsAffected["usage_attempt_facts"] != 1 {
		t.Fatalf("colliding usage facts were not imported: %+v", result.RowsAffected)
	}
	var sourceRequest model.UsageRequestFact
	mustFindBackupRow(t, conn.Where("request_model = ?", "source-request"), &sourceRequest)
	if sourceRequest.RelayLogID == 1 {
		t.Fatal("stats-only request fact kept a colliding relay log id")
	}
	var sourceAttempt model.UsageAttemptFact
	mustFindBackupRow(t, conn.Where("request_model = ?", "source-request"), &sourceAttempt)
	if sourceAttempt.RelayLogID != sourceRequest.RelayLogID {
		t.Fatalf("request/attempt remap diverged: request=%d attempt=%d",
			sourceRequest.RelayLogID, sourceAttempt.RelayLogID)
	}

	second, err := DBImportZip(ctx, bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("second DBImportZip failed: %v", err)
	}
	if second.RowsAffected["usage_request_facts"] != 0 ||
		second.RowsAffected["usage_attempt_facts"] != 0 {
		t.Fatalf("stats-only import was not idempotent: %+v", second.RowsAffected)
	}
}

func TestDBImportUsageFactsRemainIdempotentAfterAggregation(t *testing.T) {
	ctx := setupBackupTestDB(t)
	now := time.Now().UTC().Truncate(time.Second).Unix()
	dump := &model.DBDump{
		Version:      dbDumpVersion,
		IncludeStats: true,
		UsageRequestFacts: []model.UsageRequestFact{{
			RelayLogID:   42,
			Time:         now,
			RequestModel: "aggregate-lifecycle",
			Outcome:      model.RequestOutcomeSuccess,
			InputTokens:  3,
		}},
		UsageAttemptFacts: []model.UsageAttemptFact{{
			RelayLogID:    42,
			AttemptNumber: 1,
			Time:          now,
			RequestModel:  "aggregate-lifecycle",
			Status:        model.AttemptSuccess,
			Outcome:       model.RequestOutcomeSuccess,
			InputTokens:   3,
		}},
	}

	first, err := DBImportIncremental(ctx, dump)
	if err != nil {
		t.Fatalf("first import failed: %v", err)
	}
	if first.RowsAffected["usage_request_facts"] != 1 ||
		first.RowsAffected["usage_attempt_facts"] != 1 {
		t.Fatalf("facts were not imported: %+v", first.RowsAffected)
	}
	if processed, err := UsageAggregatePending(ctx, 100); err != nil || processed != 2 {
		t.Fatalf("aggregate imported facts: processed=%d err=%v", processed, err)
	}

	second, err := DBImportIncremental(ctx, dump)
	if err != nil {
		t.Fatalf("second import after aggregation failed: %v", err)
	}
	if second.RowsAffected["usage_request_facts"] != 0 ||
		second.RowsAffected["usage_attempt_facts"] != 0 {
		t.Fatalf("aggregation changed fact identity: %+v", second.RowsAffected)
	}
	if processed, err := UsageAggregatePending(ctx, 100); err != nil || processed != 0 {
		t.Fatalf("duplicate facts were queued after re-import: processed=%d err=%v", processed, err)
	}
}

func TestDBImportUsageAggregateSnapshotsAreIdempotent(t *testing.T) {
	ctx := setupBackupTestDB(t)
	aggregatedAt := time.Now().UTC()
	fact := model.UsageRequestFact{
		RelayLogID:       501,
		Time:             aggregatedAt.Unix(),
		RequestModel:     "snapshot-idempotency",
		Outcome:          model.RequestOutcomeSuccess,
		InputTokens:      7,
		OutputTokens:     11,
		DurationMS:       250,
		TokenSource:      model.UsageValueSourceReported,
		PriceSource:      model.PriceQuoteSourceGlobal,
		PriceConvertible: true,
		AggregatedAt:     &aggregatedAt,
	}
	aggregates := usageAggregateSnapshotsForRequestFact(fact)
	dump := &model.DBDump{
		Version:           dbDumpVersion,
		IncludeStats:      true,
		UsageRequestFacts: []model.UsageRequestFact{fact},
		UsageAggregates:   aggregates,
	}

	first, err := DBImportIncremental(ctx, dump)
	if err != nil {
		t.Fatalf("first snapshot import failed: %v", err)
	}
	if first.RowsAffected["usage_request_facts"] != 1 ||
		first.RowsAffected["usage_aggregates"] != 2 {
		t.Fatalf("snapshot rows were not imported: %+v", first.RowsAffected)
	}
	if processed, err := UsageAggregatePending(ctx, 100); err != nil || processed != 0 {
		t.Fatalf("snapshot-backed fact was queued again: processed=%d err=%v", processed, err)
	}

	second, err := DBImportIncremental(ctx, dump)
	if err != nil {
		t.Fatalf("second snapshot import failed: %v", err)
	}
	if second.RowsAffected["usage_request_facts"] != 0 ||
		second.RowsAffected["usage_aggregates"] != 0 {
		t.Fatalf("snapshot import was not idempotent: %+v", second.RowsAffected)
	}
}

func TestDBImportRejectsConflictingUsageAggregateSnapshots(t *testing.T) {
	for _, format := range []string{"json", "zip"} {
		t.Run(format, func(t *testing.T) {
			ctx := setupBackupTestDB(t)
			conn := dbpkg.GetDB().WithContext(ctx)
			aggregatedAt := time.Now().UTC()
			fact := model.UsageRequestFact{
				RelayLogID:   601,
				Time:         aggregatedAt.Unix(),
				RequestModel: "snapshot-conflict",
				Outcome:      model.RequestOutcomeSuccess,
				InputTokens:  13,
				AggregatedAt: &aggregatedAt,
			}
			aggregates := usageAggregateSnapshotsForRequestFact(fact)
			conflictingTarget := aggregates[0]
			conflictingTarget.InputTokens++
			mustCreateBackupRow(t, conn, &conflictingTarget)

			var err error
			switch format {
			case "json":
				_, err = DBImportIncremental(ctx, &model.DBDump{
					Version:           dbDumpVersion,
					IncludeStats:      true,
					UsageRequestFacts: []model.UsageRequestFact{fact},
					UsageAggregates:   aggregates,
				})
			case "zip":
				payload := buildBackupImportZip(t, backupZipManifest{
					Version:      dbDumpVersion,
					ExportedAt:   "2026-07-29T00:00:00Z",
					IncludeStats: true,
					Format:       "zip-v1",
				}, nil, map[string]string{
					"usage_request_facts.ndjson": encodeBackupNDJSON(t, []model.UsageRequestFact{fact}),
					"usage_aggregates.ndjson":    encodeBackupNDJSON(t, aggregates),
				})
				_, err = DBImportZip(ctx, bytes.NewReader(payload), int64(len(payload)))
			}
			if err == nil || !strings.Contains(err.Error(), "conflicts with different destination metrics") {
				t.Fatalf("conflicting aggregate snapshot was accepted: %v", err)
			}
			var factCount int64
			if countErr := conn.Model(&model.UsageRequestFact{}).Count(&factCount).Error; countErr != nil {
				t.Fatalf("count request facts: %v", countErr)
			}
			if factCount != 0 {
				t.Fatalf("failed aggregate import left facts behind: %d", factCount)
			}
			var reloaded model.UsageAggregate
			if reloadErr := conn.First(&reloaded, "aggregate_key = ?", conflictingTarget.AggregateKey).Error; reloadErr != nil {
				t.Fatalf("reload conflicting target: %v", reloadErr)
			}
			if reloaded.InputTokens != conflictingTarget.InputTokens {
				t.Fatalf("conflicting target was overwritten: %+v", reloaded)
			}
		})
	}
}

func TestDBImportRejectsPartialAggregateCoverageForProcessedFact(t *testing.T) {
	ctx := setupBackupTestDB(t)
	aggregatedAt := time.Now().UTC()
	fact := model.UsageRequestFact{
		RelayLogID:   701,
		Time:         aggregatedAt.Unix(),
		RequestModel: "partial-snapshot",
		Outcome:      model.RequestOutcomeSuccess,
		AggregatedAt: &aggregatedAt,
	}
	aggregates := usageAggregateSnapshotsForRequestFact(fact)
	if _, err := DBImportIncremental(ctx, &model.DBDump{
		Version:           dbDumpVersion,
		IncludeStats:      true,
		UsageRequestFacts: []model.UsageRequestFact{fact},
		UsageAggregates:   aggregates[:1],
	}); err == nil || !strings.Contains(err.Error(), "snapshot must contain both hourly and daily") {
		t.Fatalf("partial aggregate coverage was accepted: %v", err)
	}
}

func TestDBImportRejectsGlobalClashControllerGroup(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, err := DBImportIncremental(ctx, &model.DBDump{
		Version: dbDumpVersion,
		ClashControllers: []model.ClashControllerBackup{{
			ID:        1,
			Name:      "unsafe-global",
			APIURL:    "http://127.0.0.1:9090",
			ProxyURL:  "http://127.0.0.1:7890",
			GroupName: "GLOBAL",
			Enabled:   true,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "not GLOBAL") {
		t.Fatalf("GLOBAL Clash group import should be rejected, got %v", err)
	}
}

func TestUsageAggregateImportFingerprintUsesPostgresStoragePrecision(t *testing.T) {
	row := model.UsageAggregate{
		AggregateKey: "postgres-real",
		CostUSD:      0.123456789,
	}
	rounded := row
	rounded.CostUSD = float64(float32(row.CostUSD))

	sourceFingerprint, err := usageAggregateImportFingerprint(row, "postgres")
	if err != nil {
		t.Fatalf("fingerprint source aggregate: %v", err)
	}
	storedFingerprint, err := usageAggregateImportFingerprint(rounded, "postgres")
	if err != nil {
		t.Fatalf("fingerprint stored aggregate: %v", err)
	}
	if !bytes.Equal(sourceFingerprint, storedFingerprint) {
		t.Fatal("PostgreSQL REAL rounding changed aggregate snapshot identity")
	}

	different := rounded
	different.CostUSD += 0.01
	differentFingerprint, err := usageAggregateImportFingerprint(different, "postgres")
	if err != nil {
		t.Fatalf("fingerprint different aggregate: %v", err)
	}
	if bytes.Equal(sourceFingerprint, differentFingerprint) {
		t.Fatal("materially different PostgreSQL aggregate metrics were treated as identical")
	}
}

func usageAggregateSnapshotsForRequestFact(
	fact model.UsageRequestFact,
) []model.UsageAggregate {
	value := usageAggregateFactFromRequest(fact)
	rows := make([]model.UsageAggregate, 0, 2)
	for _, granularity := range []model.UsageAggregateGranularity{
		model.UsageAggregateHourly,
		model.UsageAggregateDaily,
	} {
		bucketStart := usageAggregateBucketStart(value.time, granularity)
		row := newUsageAggregate(
			usageAggregateKey(value, granularity, bucketStart),
			value,
			granularity,
			bucketStart,
		)
		addUsageFactToAggregate(row, value)
		rows = append(rows, *row)
	}
	return rows
}

func TestDBImportUsageFactsSkipOccupiedDeterministicIDs(t *testing.T) {
	ctx := setupBackupTestDB(t)
	conn := dbpkg.GetDB().WithContext(ctx)
	sourceRequest := model.UsageRequestFact{
		RelayLogID: 1, Time: 2, RequestModel: "source-deterministic-collision",
		Outcome: model.RequestOutcomeSuccess,
	}
	fingerprint, err := usageRequestFactImportFingerprint(sourceRequest)
	if err != nil {
		t.Fatalf("fingerprint source request: %v", err)
	}
	occupiedID := deterministicUsageFactImportID(sourceRequest.RelayLogID, fingerprint, 0)
	mustCreateBackupRow(t, conn, &model.UsageRequestFact{
		RelayLogID: 1, Time: 1, RequestModel: "occupied-source-id",
		Outcome: model.RequestOutcomeFailed,
	})
	mustCreateBackupRow(t, conn, &model.UsageRequestFact{
		RelayLogID: occupiedID, Time: 1, RequestModel: "occupied-deterministic-id",
		Outcome: model.RequestOutcomeFailed,
	})
	mustCreateBackupRow(t, conn, &model.UsageAttemptFact{
		RelayLogID: occupiedID, AttemptNumber: 1, Time: 1,
		RequestModel: "occupied-deterministic-id", Status: model.AttemptFailed,
		Outcome: model.RequestOutcomeFailed,
	})
	dump := &model.DBDump{
		Version:      dbDumpVersion,
		IncludeStats: true,
		UsageRequestFacts: []model.UsageRequestFact{
			sourceRequest,
		},
		UsageAttemptFacts: []model.UsageAttemptFact{{
			RelayLogID: 1, AttemptNumber: 1, Time: 2,
			RequestModel: "source-deterministic-collision", Status: model.AttemptSuccess,
			Outcome: model.RequestOutcomeSuccess,
		}},
	}

	first, err := DBImportIncremental(ctx, dump)
	if err != nil {
		t.Fatalf("first DBImportIncremental failed: %v", err)
	}
	if first.RowsAffected["usage_request_facts"] != 1 ||
		first.RowsAffected["usage_attempt_facts"] != 1 {
		t.Fatalf("source facts were dropped on deterministic collision: %+v", first.RowsAffected)
	}
	var importedRequest model.UsageRequestFact
	mustFindBackupRow(t, conn.Where("request_model = ?", "source-deterministic-collision"), &importedRequest)
	if importedRequest.RelayLogID == occupiedID {
		t.Fatal("source request reused an occupied deterministic ID")
	}
	var importedAttempt model.UsageAttemptFact
	mustFindBackupRow(t, conn.Where(
		"relay_log_id = ? AND attempt_number = ?",
		importedRequest.RelayLogID,
		1,
	), &importedAttempt)
	if importedAttempt.RequestModel != importedRequest.RequestModel {
		t.Fatalf("source attempt was attached to an unrelated request: %+v", importedAttempt)
	}
	second, err := DBImportIncremental(ctx, dump)
	if err != nil {
		t.Fatalf("second DBImportIncremental failed: %v", err)
	}
	if second.RowsAffected["usage_request_facts"] != 0 ||
		second.RowsAffected["usage_attempt_facts"] != 0 {
		t.Fatalf("collision remap was not idempotent: %+v", second.RowsAffected)
	}
}

func TestDBImportUsageFactsRejectMismatchedAttemptAtMappedID(t *testing.T) {
	ctx := setupBackupTestDB(t)
	conn := dbpkg.GetDB().WithContext(ctx)
	request := model.UsageRequestFact{
		RelayLogID: 1, Time: 1, RequestModel: "shared-request",
		Outcome: model.RequestOutcomeSuccess,
	}
	mustCreateBackupRow(t, conn, &request)
	mustCreateBackupRow(t, conn, &model.UsageAttemptFact{
		RelayLogID: 1, AttemptNumber: 1, Time: 1,
		RequestModel: "target-attempt", Status: model.AttemptFailed,
		Outcome: model.RequestOutcomeFailed,
	})
	dump := &model.DBDump{
		Version:           dbDumpVersion,
		IncludeStats:      true,
		UsageRequestFacts: []model.UsageRequestFact{request},
		UsageAttemptFacts: []model.UsageAttemptFact{{
			RelayLogID: 1, AttemptNumber: 1, Time: 1,
			RequestModel: "source-attempt", Status: model.AttemptSuccess,
			Outcome: model.RequestOutcomeSuccess,
		}},
	}

	if _, err := DBImportIncremental(ctx, dump); err == nil ||
		!strings.Contains(err.Error(), "target identity collision") {
		t.Fatalf("mismatched attempt at mapped ID was not rejected: %v", err)
	}
	var sourceAttemptCount int64
	if err := conn.Model(&model.UsageAttemptFact{}).
		Where("request_model = ?", "source-attempt").
		Count(&sourceAttemptCount).Error; err != nil {
		t.Fatalf("count source attempts failed: %v", err)
	}
	if sourceAttemptCount != 0 {
		t.Fatalf("mismatched attempt was partially imported: %d", sourceAttemptCount)
	}
}

func TestDBImportZipRejectsDuplicateRelayLogSourceIDsAcrossBatches(t *testing.T) {
	ctx := setupBackupTestDB(t)
	logs := make([]model.RelayLog, 0, dbImportBatchSize+1)
	for index := 0; index < dbImportBatchSize; index++ {
		logs = append(logs, model.RelayLog{
			ID: int64(index + 1), Time: int64(index + 1),
			RequestModelName: fmt.Sprintf("unique-source-%d", index),
			ChannelId:        100,
			Outcome:          model.RequestOutcomeSuccess,
		})
	}
	logs = append(logs, model.RelayLog{
		ID: 1, Time: 999, RequestModelName: "duplicate-source",
		ChannelId: 100, Outcome: model.RequestOutcomeSuccess,
	})
	payload := buildBackupImportZip(t, backupZipManifest{
		Version:     dbDumpVersion,
		ExportedAt:  "2026-07-28T00:00:00Z",
		IncludeLogs: true,
		Format:      "zip-v1",
	}, map[string]any{
		"channels.json": []model.Channel{{
			ID: 100, Name: "duplicate-source-channel", Enabled: true,
		}},
	}, map[string]string{"relay_logs.ndjson": encodeBackupNDJSON(t, logs)})

	if _, err := DBImportZip(ctx, bytes.NewReader(payload), int64(len(payload))); err == nil {
		t.Fatal("duplicate relay log source id should fail the import")
	}
	conn := dbpkg.GetDB().WithContext(ctx)
	var count int64
	if err := conn.Model(&model.Channel{}).
		Where("name = ?", "duplicate-source-channel").
		Count(&count).Error; err != nil {
		t.Fatalf("count channels failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("duplicate source id failure partially committed: %d", count)
	}
}

func TestDBImportZipRejectsExcessiveNestedRecordComplexity(t *testing.T) {
	ctx := setupBackupTestDB(t)
	var raw strings.Builder
	raw.WriteString(`{"id":1,"attempts":[`)
	for index := 0; index < maxBackupZipRecordTokens/2+10; index++ {
		if index > 0 {
			raw.WriteByte(',')
		}
		raw.WriteString(`{}`)
	}
	raw.WriteString("]}\n")
	payload := buildBackupImportZip(t, backupZipManifest{
		Version:     dbDumpVersion,
		ExportedAt:  "2026-07-28T00:00:00Z",
		IncludeLogs: true,
		Format:      "zip-v1",
	}, nil, map[string]string{"relay_logs.ndjson": raw.String()})

	if _, err := DBImportZip(ctx, bytes.NewReader(payload), int64(len(payload))); err == nil ||
		!strings.Contains(err.Error(), "too many JSON tokens") {
		t.Fatalf("complex record was not rejected before typed import: %v", err)
	}
}

func TestBackupZipRecordBudgetRejectsOversizedInput(t *testing.T) {
	budget := &backupZipRecordBudget{total: maxBackupZipRecords}
	if err := budget.consume("rows.ndjson", 1); err == nil {
		t.Fatal("record count over budget should be rejected")
	}
	budget = &backupZipRecordBudget{}
	if err := budget.consume("rows.ndjson", maxBackupZipRecordBytes+1); err == nil {
		t.Fatal("oversized record should be rejected")
	}
}

func TestOpenBackupZipRejectsDirectoryEntryFlood(t *testing.T) {
	var payload bytes.Buffer
	writer := zip.NewWriter(&payload)
	for index := 0; index <= len(backupZipAllowedFiles()); index++ {
		entry, err := writer.Create("manifest.json")
		if err != nil {
			t.Fatalf("create ZIP entry: %v", err)
		}
		if _, err := entry.Write([]byte(`{}`)); err != nil {
			t.Fatalf("write ZIP entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close ZIP: %v", err)
	}
	if _, err := openBackupZip(
		context.Background(),
		bytes.NewReader(payload.Bytes()),
		int64(payload.Len()),
	); err == nil || !strings.Contains(err.Error(), "too many entries") {
		t.Fatalf("directory entry flood was not rejected early: %v", err)
	}
}

func TestOpenBackupZipRejectsSpoofedDirectoryEntryCount(t *testing.T) {
	var payload bytes.Buffer
	writer := zip.NewWriter(&payload)
	for index := 0; index <= len(backupZipAllowedFiles()); index++ {
		entry, err := writer.Create("manifest.json")
		if err != nil {
			t.Fatalf("create ZIP entry: %v", err)
		}
		if _, err := entry.Write([]byte(`{}`)); err != nil {
			t.Fatalf("write ZIP entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close ZIP: %v", err)
	}
	archive := payload.Bytes()
	eocd := bytes.LastIndex(archive, []byte{'P', 'K', 0x05, 0x06})
	if eocd < 0 {
		t.Fatal("ZIP end-of-directory record not found")
	}
	archive[eocd+8], archive[eocd+9] = 1, 0
	archive[eocd+10], archive[eocd+11] = 1, 0

	if _, err := openBackupZip(
		context.Background(),
		bytes.NewReader(archive),
		int64(len(archive)),
	); err == nil || !strings.Contains(err.Error(), "central directory contains too many entries") {
		t.Fatalf("spoofed directory count bypassed preflight: %v", err)
	}
}

func TestDBImportZipRejectsDuplicateAndUnexpectedEntries(t *testing.T) {
	for _, test := range []struct {
		name    string
		entries []string
	}{
		{name: "duplicate manifest", entries: []string{"manifest.json", "manifest.json"}},
		{name: "unexpected entry", entries: []string{"manifest.json", "../secret.json"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var payload bytes.Buffer
			writer := zip.NewWriter(&payload)
			for _, name := range test.entries {
				entry, err := writer.Create(name)
				if err != nil {
					t.Fatalf("create ZIP entry: %v", err)
				}
				if _, err := entry.Write([]byte(
					`{"version":2,"exported_at":"2026-07-28T00:00:00Z","format":"zip-v1"}`,
				)); err != nil {
					t.Fatalf("write ZIP entry: %v", err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatalf("close ZIP: %v", err)
			}
			if _, err := openBackupZip(
				context.Background(),
				bytes.NewReader(payload.Bytes()),
				int64(payload.Len()),
			); err == nil {
				t.Fatal("unsafe ZIP was accepted")
			}
		})
	}
}

func TestDBImportZipRejectsManifestMissingRequiredEntry(t *testing.T) {
	tests := []struct {
		name     string
		manifest backupZipManifest
		missing  string
	}{
		{
			name: "logs",
			manifest: backupZipManifest{
				Version: dbDumpVersion, ExportedAt: "2026-07-28T00:00:00Z",
				Format: "zip-v1", IncludeLogs: true,
			},
			missing: "relay_logs.ndjson",
		},
		{
			name: "stats",
			manifest: backupZipManifest{
				Version: dbDumpVersion, ExportedAt: "2026-07-28T00:00:00Z",
				Format: "zip-v1", IncludeStats: true,
			},
			missing: "usage_aggregates.ndjson",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := buildBackupImportZip(t, test.manifest, nil, nil)
			payload = removeBackupZipEntry(t, payload, test.missing)
			ctx := setupBackupTestDB(t)
			if _, err := DBImportZip(
				ctx,
				bytes.NewReader(payload),
				int64(len(payload)),
			); err == nil || !strings.Contains(err.Error(), "missing required entry") {
				t.Fatalf("manifest/file mismatch was accepted: %v", err)
			}
		})
	}
}

func buildBackupImportZip(
	t *testing.T,
	manifest backupZipManifest,
	jsonEntries map[string]any,
	rawEntries map[string]string,
) []byte {
	t.Helper()
	var payload bytes.Buffer
	writer := zip.NewWriter(&payload)
	written := make(map[string]struct{})
	writeJSON := func(name string, value any) {
		t.Helper()
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create ZIP entry %s: %v", name, err)
		}
		if err := json.NewEncoder(entry).Encode(value); err != nil {
			t.Fatalf("encode ZIP entry %s: %v", name, err)
		}
		written[name] = struct{}{}
	}
	writeJSON("manifest.json", manifest)
	for name, value := range jsonEntries {
		writeJSON(name, value)
	}
	for name, value := range rawEntries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create ZIP entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(value)); err != nil {
			t.Fatalf("write ZIP entry %s: %v", name, err)
		}
		written[name] = struct{}{}
	}
	required := make([]string, 0)
	if manifest.IncludeLogs {
		required = append(required,
			"relay_logs.ndjson",
			"relay_log_repair_audits.json",
			"site_operation_attempts.ndjson",
		)
	}
	if manifest.IncludeStats {
		required = append(required,
			"stats_total.json", "stats_daily.json", "stats_hourly.json",
			"stats_model.json", "stats_channel.json", "stats_api_key.json",
			"stats_site_model_hourly.json", "usage_request_facts.ndjson",
			"usage_attempt_facts.ndjson", "usage_aggregates.ndjson",
		)
	}
	for _, name := range required {
		if _, ok := written[name]; ok {
			continue
		}
		if strings.HasSuffix(name, ".json") {
			writeJSON(name, []any{})
			continue
		}
		_, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create empty ZIP entry %s: %v", name, err)
		}
		written[name] = struct{}{}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close ZIP: %v", err)
	}
	return payload.Bytes()
}

func removeBackupZipEntry(t *testing.T, payload []byte, name string) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("open source ZIP: %v", err)
	}
	var result bytes.Buffer
	writer := zip.NewWriter(&result)
	removed := false
	for _, file := range reader.File {
		if file.Name == name {
			removed = true
			continue
		}
		source, err := file.Open()
		if err != nil {
			t.Fatalf("open source entry %s: %v", file.Name, err)
		}
		target, err := writer.CreateHeader(&file.FileHeader)
		if err != nil {
			source.Close()
			t.Fatalf("create target entry %s: %v", file.Name, err)
		}
		if _, err := io.Copy(target, source); err != nil {
			source.Close()
			t.Fatalf("copy ZIP entry %s: %v", file.Name, err)
		}
		source.Close()
	}
	if !removed {
		t.Fatalf("ZIP entry %s was not present", name)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close rewritten ZIP: %v", err)
	}
	return result.Bytes()
}

func encodeBackupNDJSON[T any](t *testing.T, rows []T) string {
	t.Helper()
	var payload strings.Builder
	encoder := json.NewEncoder(&payload)
	for index := range rows {
		if err := encoder.Encode(&rows[index]); err != nil {
			t.Fatalf("encode NDJSON row: %v", err)
		}
	}
	return payload.String()
}

func seedExtendedBackupData(t *testing.T, ctx context.Context) extendedBackupSeed {
	t.Helper()
	conn := dbpkg.GetDB().WithContext(ctx)
	now := time.Date(2026, time.July, 27, 8, 0, 0, 0, time.UTC)
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("refresh source settings: %v", err)
	}
	if err := SettingSetString(model.SettingKeyJWTSecret, "source-backup-jwt-secret"); err != nil {
		t.Fatalf("set source jwt secret: %v", err)
	}
	encryptedControllerSecret, err := EncryptSecret("controller-secret")
	if err != nil {
		t.Fatalf("encrypt controller secret: %v", err)
	}

	controller := model.ClashController{
		Name:            "backup-controller",
		APIURL:          "http://127.0.0.1:9090",
		ProxyURL:        "http://127.0.0.1:7890",
		GroupName:       "Octopus-Recovery",
		SecretEncrypted: encryptedControllerSecret,
		Enabled:         true,
	}
	mustCreateBackupRow(t, conn, &controller)
	proxy := model.ProxyConfiguration{
		Name:              "backup-proxy",
		URL:               "http://127.0.0.1:7890",
		ClashControllerID: &controller.ID,
		Enabled:           true,
	}
	mustCreateBackupRow(t, conn, &proxy)
	channel := model.Channel{
		Name:          "backup-channel",
		Enabled:       true,
		ProxyMode:     model.ProxyUsageModePool,
		ProxyConfigID: &proxy.ID,
	}
	mustCreateBackupRow(t, conn, &channel)
	mustCreateBackupRow(t, conn, &model.ChannelKey{
		ChannelID: channel.ID, ChannelKey: "backup-channel-key", Enabled: true,
	})
	site := model.Site{
		Name:                   "backup-site",
		Platform:               model.SitePlatformNewAPI,
		BaseURL:                "https://backup.example.com",
		Enabled:                true,
		ProxyMode:              model.ProxyUsageModePool,
		ProxyConfigID:          &proxy.ID,
		PreferredProxyConfigID: &proxy.ID,
	}
	mustCreateBackupRow(t, conn, &site)
	account := model.SiteAccount{
		SiteID:                      site.ID,
		Name:                        "backup-account",
		CredentialType:              model.SiteCredentialTypeAPIKey,
		APIKey:                      "backup-site-key",
		ProxyMode:                   model.ProxyUsageModePool,
		ProxyConfigID:               &proxy.ID,
		PreferredProxyConfigID:      &proxy.ID,
		VerificationCookieEncrypted: "verification-cookie",
		VerificationUserAgent:       "verification-agent",
		VerificationProxyConfigID:   &proxy.ID,
		VerificationExpiresAt:       timePointer(now.Add(time.Hour)),
		Enabled:                     true,
		AutoSync:                    true,
	}
	mustCreateBackupRow(t, conn, &account)
	mustCreateBackupRow(t, conn, &model.SiteProxyPreference{
		IdentityKey: (SiteProxyPathDescriptor{
			SiteID:            site.ID,
			SiteAccountID:     account.ID,
			ProxyMode:         model.ProxyUsageModePool,
			ProxyConfigID:     proxy.ID,
			ClashControllerID: controller.ID,
			ClashNode:         "backup-node",
		}).IdentityKey(),
		SiteID:            site.ID,
		SiteAccountID:     account.ID,
		ProxyMode:         model.ProxyUsageModePool,
		ProxyConfigID:     proxy.ID,
		ClashControllerID: controller.ID,
		ClashNode:         "backup-node",
		Status:            model.SiteProxyPreferenceHealthy,
		SuccessCount:      3,
		LastSuccessAt:     timePointer(now),
		ExpiresAt:         timePointer(now.Add(24 * time.Hour)),
	})
	apiKey := model.APIKey{Name: "backup-api-key", APIKey: "client-backup-key", Enabled: true}
	mustCreateBackupRow(t, conn, &apiKey)
	canonical := model.CanonicalModel{
		Name:            "Backup Model",
		NormalizedName:  "backup-model",
		RoutingStrategy: model.RoutingStrategyReliability,
		ProtocolPolicy:  model.ProtocolPolicyTransformAllowed,
		Enabled:         true,
		Manual:          true,
	}
	mustCreateBackupRow(t, conn, &canonical)
	mustCreateBackupRow(t, conn, &model.ModelAlias{
		CanonicalModelID: canonical.ID,
		Alias:            "Backup Alias",
		NormalizedAlias:  "backup-alias",
		Manual:           true,
	})
	candidate := model.RouteCandidate{
		CanonicalModelID:  canonical.ID,
		ChannelID:         channel.ID,
		UpstreamModelName: "provider/backup-model-v1",
		SiteID:            &site.ID,
		SiteAccountID:     &account.ID,
		SiteGroupKey:      "premium",
		Status:            model.RouteCandidateActive,
		Weight:            2,
		Manual:            true,
		LastSeenAt:        now,
	}
	mustCreateBackupRow(t, conn, &candidate)
	mustCreateBackupRow(t, conn, &model.HeaderPolicy{
		Name:    "Backup site policy",
		Version: 4,
		Scope:   model.HeaderPolicyScopeSite,
		ScopeID: site.ID,
		Enabled: true,
	})
	mustCreateBackupRow(t, conn, &model.HeaderPolicy{
		Name:    "Backup canonical policy",
		Version: 5,
		Scope:   model.HeaderPolicyScopeCanonicalModel,
		ScopeID: canonical.ID,
		Enabled: true,
	})
	mustCreateBackupRow(t, conn, &model.HeaderPolicy{
		Name:    "Backup candidate policy",
		Version: 6,
		Scope:   model.HeaderPolicyScopeRouteCandidate,
		ScopeID: candidate.ID,
		Enabled: true,
	})
	mustCreateBackupRow(t, conn, &model.UserAgentProfile{
		Name: "Backup Agent", Value: "backup-agent/1.0",
	})
	quote := model.SiteModelPriceQuote{
		RouteCandidateID:  &candidate.ID,
		SiteID:            site.ID,
		SiteAccountID:     &account.ID,
		GroupKey:          "premium",
		ModelName:         candidate.UpstreamModelName,
		Source:            model.PriceQuoteSourceSiteExact,
		Unit:              model.PriceUnitPerMillionTokens,
		Currency:          "EUR",
		Input:             1.25,
		Output:            2.5,
		GroupMultiplier:   1,
		ExchangeRateToUSD: 1.1,
		ObservedAt:        now,
	}
	quote.RefreshIdentityKey()
	mustCreateBackupRow(t, conn, &quote)
	mustCreateBackupRow(t, conn, &model.CurrencyRate{
		Currency: "EUR", RateToUSD: 1.1, UpdatedAt: now,
	})

	relayLog := model.RelayLog{
		ID:                 7001,
		Time:               now.Unix(),
		RequestModelName:   canonical.Name,
		RequestAPIKeyID:    apiKey.ID,
		RequestAPIKeyName:  apiKey.Name,
		ChannelId:          channel.ID,
		ChannelName:        channel.Name,
		ActualModelName:    candidate.UpstreamModelName,
		CanonicalModelName: canonical.Name,
		RouteCandidateID:   candidate.ID,
		PriceQuoteID:       quote.ID,
		Success:            true,
		Outcome:            model.RequestOutcomeSuccess,
		Attempts: []model.ChannelAttempt{{
			ChannelID: channel.ID, ChannelName: channel.Name,
			RouteCandidateID: candidate.ID, AttemptNum: 1, Status: model.AttemptSuccess,
			Usage: &model.AttemptUsageSnapshot{
				InputTokens: 10, OutputTokens: 20,
				PriceQuoteID: quote.ID,
			},
		}},
	}
	mustCreateBackupRow(t, conn, &relayLog)
	mustCreateBackupRow(t, conn, &model.RelayLogRepairAudit{
		BatchID: "backup-audit", RuleVersion: "v1", Matched: 1, Updated: 1,
		RequestedAt: now, CompletedAt: now,
	})
	mustCreateBackupRow(t, conn, &model.SiteOperationAttempt{
		SiteID:            site.ID,
		SiteAccountID:     account.ID,
		Operation:         model.SiteOperationSync,
		AttemptNumber:     1,
		ProxyMode:         model.ProxyUsageModePool,
		ProxyConfigID:     &proxy.ID,
		ClashControllerID: &controller.ID,
		StartedAt:         now,
		Success:           true,
	})
	aggregatedAt := now.Add(time.Minute)
	mustCreateBackupRow(t, conn, &model.UsageRequestFact{
		RelayLogID:       relayLog.ID,
		Time:             now.Unix(),
		SiteID:           site.ID,
		SiteName:         site.Name,
		SiteAccountID:    account.ID,
		SiteAccountName:  account.Name,
		ChannelID:        channel.ID,
		ChannelName:      channel.Name,
		APIKeyID:         apiKey.ID,
		APIKeyName:       apiKey.Name,
		RouteCandidateID: candidate.ID,
		PriceQuoteID:     quote.ID,
		RequestModel:     canonical.Name,
		ActualModel:      candidate.UpstreamModelName,
		CanonicalModel:   canonical.Name,
		Outcome:          model.RequestOutcomeSuccess,
		InputTokens:      10,
		OutputTokens:     20,
		AggregatedAt:     &aggregatedAt,
	})
	mustCreateBackupRow(t, conn, &model.UsageAttemptFact{
		RelayLogID:       relayLog.ID,
		AttemptNumber:    1,
		Time:             now.Unix(),
		SiteID:           site.ID,
		SiteAccountID:    account.ID,
		ChannelID:        channel.ID,
		APIKeyID:         apiKey.ID,
		RouteCandidateID: candidate.ID,
		PriceQuoteID:     quote.ID,
		RequestModel:     canonical.Name,
		ActualModel:      candidate.UpstreamModelName,
		CanonicalModel:   canonical.Name,
		Status:           model.AttemptSuccess,
		Outcome:          model.RequestOutcomeSuccess,
		AggregatedAt:     &aggregatedAt,
	})
	mustCreateBackupRow(t, conn, &model.UsageAggregate{
		AggregateKey:   "source-key",
		Granularity:    model.UsageAggregateDaily,
		MetricScope:    string(UsageMetricScopeRequest),
		BucketStart:    now.Unix(),
		SiteID:         site.ID,
		SiteName:       site.Name,
		SiteAccountID:  account.ID,
		ChannelID:      channel.ID,
		APIKeyID:       apiKey.ID,
		RequestModel:   canonical.Name,
		ActualModel:    candidate.UpstreamModelName,
		CanonicalModel: canonical.Name,
		MetricCount:    1,
		SuccessCount:   1,
		InputTokens:    10,
		OutputTokens:   20,
	})
	mustCreateBackupRow(t, conn, &model.VerificationSession{
		SiteID: site.ID, SiteAccountID: account.ID,
		Status:    model.VerificationSessionPending,
		ExpiresAt: now.Add(time.Hour), CookieEncrypted: "verification-session-cookie",
	})

	return extendedBackupSeed{
		controller: controller,
		proxy:      proxy,
		channel:    channel,
		site:       site,
		account:    account,
		apiKey:     apiKey,
		canonical:  canonical,
		candidate:  candidate,
		quote:      quote,
	}
}

func seedDestinationIDCollisions(t *testing.T) {
	t.Helper()
	conn := dbpkg.GetDB()
	controller := model.ClashController{
		Name: "existing-controller", APIURL: "http://127.0.0.1:9191",
		ProxyURL: "http://127.0.0.1:7999", GroupName: "EXISTING", Enabled: true,
	}
	mustCreateBackupRow(t, conn, &controller)
	proxy := model.ProxyConfiguration{Name: "existing-proxy", URL: "http://127.0.0.1:7999", Enabled: true}
	mustCreateBackupRow(t, conn, &proxy)
	channel := model.Channel{Name: "existing-channel", Enabled: true}
	mustCreateBackupRow(t, conn, &channel)
	site := model.Site{
		Name: "existing-site", Platform: model.SitePlatformNewAPI,
		BaseURL: "https://existing.example.com", Enabled: true,
	}
	mustCreateBackupRow(t, conn, &site)
	account := model.SiteAccount{
		SiteID: site.ID, Name: "existing-account",
		CredentialType: model.SiteCredentialTypeAPIKey, APIKey: "existing-key", Enabled: true,
	}
	mustCreateBackupRow(t, conn, &account)
	mustCreateBackupRow(t, conn, &model.APIKey{Name: "existing-api-key", APIKey: "existing-client-key", Enabled: true})
	mustCreateBackupRow(t, conn, &model.CanonicalModel{
		Name: "Existing Model", NormalizedName: "existing-model", Enabled: true,
	})
	priceCollision := model.SiteModelPriceQuote{
		SiteID: site.ID, SiteAccountID: &account.ID,
		GroupKey: model.SiteDefaultGroupKey, ModelName: "existing-price",
		Source: model.PriceQuoteSourceManualOverride,
		Unit:   model.PriceUnitPerMillionTokens, Currency: "USD",
		Input: 1, Output: 2, GroupMultiplier: 1, ExchangeRateToUSD: 1,
		ObservedAt: time.Now(), ManualOverride: true,
	}
	priceCollision.RefreshIdentityKey()
	mustCreateBackupRow(t, conn, &priceCollision)
	mustCreateBackupRow(t, conn, &model.RelayLog{
		ID: 7001, Time: 1, RequestModelName: "destination-log",
		ChannelId: channel.ID, Outcome: model.RequestOutcomeFailed,
	})
}

func assertExtendedBackupRelations(t *testing.T, seed extendedBackupSeed) {
	t.Helper()
	conn := dbpkg.GetDB()

	var controller model.ClashController
	mustFindBackupRow(t, conn.Where("name = ?", seed.controller.Name), &controller)
	if controller.ID == seed.controller.ID || controller.SecretEncrypted != "" || controller.Enabled {
		t.Fatalf("controller collision/remap failed: %+v", controller)
	}
	var proxy model.ProxyConfiguration
	mustFindBackupRow(t, conn.Where("url = ?", seed.proxy.URL), &proxy)
	if proxy.ClashControllerID == nil || *proxy.ClashControllerID != controller.ID {
		t.Fatalf("proxy clash controller was not remapped: %+v", proxy)
	}
	var channel model.Channel
	mustFindBackupRow(t, conn.Where("name = ?", seed.channel.Name), &channel)
	var site model.Site
	mustFindBackupRow(t, conn.Where("base_url = ?", seed.site.BaseURL), &site)
	if site.ProxyConfigID == nil || *site.ProxyConfigID != proxy.ID ||
		site.PreferredProxyConfigID == nil || *site.PreferredProxyConfigID != proxy.ID {
		t.Fatalf("site proxy references were not remapped: %+v", site)
	}
	var account model.SiteAccount
	mustFindBackupRow(t, conn.Where("site_id = ? AND name = ?", site.ID, seed.account.Name), &account)
	if account.ProxyConfigID == nil || *account.ProxyConfigID != proxy.ID ||
		account.PreferredProxyConfigID == nil || *account.PreferredProxyConfigID != proxy.ID {
		t.Fatalf("account proxy references were not remapped: %+v", account)
	}
	if account.VerificationCookieEncrypted != "" || account.VerificationExpiresAt != nil {
		t.Fatalf("verification state was restored unexpectedly: %+v", account)
	}
	var preference model.SiteProxyPreference
	mustFindBackupRow(t, conn.Where(
		"site_id = ? AND site_account_id = ? AND clash_node = ?",
		site.ID,
		account.ID,
		"backup-node",
	), &preference)
	expectedPreferenceKey := (SiteProxyPathDescriptor{
		SiteID:            site.ID,
		SiteAccountID:     account.ID,
		ProxyMode:         model.ProxyUsageModePool,
		ProxyConfigID:     proxy.ID,
		ClashControllerID: controller.ID,
		ClashNode:         "backup-node",
	}).IdentityKey()
	if preference.ProxyConfigID != proxy.ID ||
		preference.ClashControllerID != controller.ID ||
		preference.IdentityKey != expectedPreferenceKey {
		t.Fatalf("site proxy preference references were not remapped: %+v", preference)
	}
	var apiKey model.APIKey
	mustFindBackupRow(t, conn.Where("api_key = ?", seed.apiKey.APIKey), &apiKey)
	var canonical model.CanonicalModel
	mustFindBackupRow(t, conn.Where("normalized_name = ?", seed.canonical.NormalizedName), &canonical)
	var candidate model.RouteCandidate
	mustFindBackupRow(t, conn.Where(
		"canonical_model_id = ? AND channel_id = ? AND upstream_model_name = ?",
		canonical.ID, channel.ID, seed.candidate.UpstreamModelName,
	), &candidate)
	if candidate.SiteID == nil || *candidate.SiteID != site.ID ||
		candidate.SiteAccountID == nil || *candidate.SiteAccountID != account.ID {
		t.Fatalf("route candidate site references were not remapped: %+v", candidate)
	}

	expectedPolicies := map[model.HeaderPolicyScope]struct {
		scopeID int
		name    string
		version int
	}{
		model.HeaderPolicyScopeSite: {
			scopeID: site.ID, name: "Backup site policy", version: 4,
		},
		model.HeaderPolicyScopeCanonicalModel: {
			scopeID: canonical.ID, name: "Backup canonical policy", version: 5,
		},
		model.HeaderPolicyScopeRouteCandidate: {
			scopeID: candidate.ID, name: "Backup candidate policy", version: 6,
		},
	}
	var policies []model.HeaderPolicy
	if err := conn.Find(&policies).Error; err != nil {
		t.Fatalf("query header policies failed: %v", err)
	}
	for _, policy := range policies {
		if expected, ok := expectedPolicies[policy.Scope]; ok &&
			(policy.ScopeID != expected.scopeID ||
				policy.Name != expected.name ||
				policy.Version != expected.version) {
			t.Fatalf("header policy metadata/relation did not round-trip: got %+v want %+v", policy, expected)
		}
	}

	var quote model.SiteModelPriceQuote
	mustFindBackupRow(t, conn.Where("model_name = ?", seed.candidate.UpstreamModelName), &quote)
	if quote.RouteCandidateID == nil || *quote.RouteCandidateID != candidate.ID ||
		quote.SiteID != site.ID || quote.SiteAccountID == nil || *quote.SiteAccountID != account.ID {
		t.Fatalf("price quote references were not remapped: %+v", quote)
	}
	if quote.ID == seed.quote.ID {
		t.Fatalf("price quote ID collision was not remapped: %+v", quote)
	}
	var relayLog model.RelayLog
	mustFindBackupRow(t, conn.Where("request_model_name = ?", seed.canonical.Name), &relayLog)
	if relayLog.ID == 7001 {
		t.Fatalf("relay log ID collision was not remapped: %+v", relayLog)
	}
	if relayLog.ChannelId != channel.ID || relayLog.RequestAPIKeyID != apiKey.ID ||
		relayLog.RouteCandidateID != candidate.ID || relayLog.PriceQuoteID != quote.ID ||
		len(relayLog.Attempts) != 1 || relayLog.Attempts[0].ChannelID != channel.ID ||
		relayLog.Attempts[0].RouteCandidateID != candidate.ID ||
		relayLog.Attempts[0].Usage == nil ||
		relayLog.Attempts[0].Usage.PriceQuoteID != quote.ID {
		t.Fatalf("relay log references were not remapped: %+v", relayLog)
	}
	var requestFact model.UsageRequestFact
	mustFindBackupRow(t, conn.Where("relay_log_id = ?", relayLog.ID), &requestFact)
	if requestFact.SiteID != site.ID || requestFact.SiteAccountID != account.ID ||
		requestFact.ChannelID != channel.ID || requestFact.APIKeyID != apiKey.ID ||
		requestFact.RouteCandidateID != candidate.ID || requestFact.PriceQuoteID != quote.ID {
		t.Fatalf("request fact references were not remapped: %+v", requestFact)
	}
	var attemptFact model.UsageAttemptFact
	mustFindBackupRow(t, conn.Where(
		"relay_log_id = ? AND attempt_number = ?",
		relayLog.ID,
		1,
	), &attemptFact)
	if attemptFact.RouteCandidateID != candidate.ID || attemptFact.PriceQuoteID != quote.ID {
		t.Fatalf("attempt fact references were not remapped: %+v", attemptFact)
	}
	var aggregate model.UsageAggregate
	mustFindBackupRow(t, conn.Where("metric_scope = ?", string(UsageMetricScopeRequest)), &aggregate)
	if aggregate.AggregateKey == "source-key" ||
		aggregate.SiteID != site.ID || aggregate.SiteAccountID != account.ID ||
		aggregate.ChannelID != channel.ID || aggregate.APIKeyID != apiKey.ID {
		t.Fatalf("usage aggregate identity was not remapped: %+v", aggregate)
	}
	var attempt model.SiteOperationAttempt
	mustFindBackupRow(t, conn.Where("site_account_id = ?", account.ID), &attempt)
	if attempt.SiteID != site.ID || attempt.ProxyConfigID == nil || *attempt.ProxyConfigID != proxy.ID ||
		attempt.ClashControllerID == nil || *attempt.ClashControllerID != controller.ID {
		t.Fatalf("site operation attempt references were not remapped: %+v", attempt)
	}
	var verificationCount int64
	if err := conn.Model(&model.VerificationSession{}).Count(&verificationCount).Error; err != nil {
		t.Fatalf("count verification sessions failed: %v", err)
	}
	if verificationCount != 0 {
		t.Fatalf("verification sessions should not be restored, got %d", verificationCount)
	}
}

func mustCreateBackupRow(t *testing.T, conn *gorm.DB, value any) {
	t.Helper()
	if err := conn.Create(value).Error; err != nil {
		t.Fatalf("create %T failed: %v", value, err)
	}
}

func mustFindBackupRow(t *testing.T, conn *gorm.DB, value any) {
	t.Helper()
	if err := conn.First(value).Error; err != nil {
		t.Fatalf("find %T failed: %v", value, err)
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}
