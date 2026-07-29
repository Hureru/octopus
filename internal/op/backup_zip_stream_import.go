package op

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

type backupZipRecordBudget struct {
	total int
}

func (budget *backupZipRecordBudget) consume(fileName string, recordBytes int64) error {
	if recordBytes > maxBackupZipRecordBytes {
		return fmt.Errorf(
			"ZIP entry %q contains a record larger than %d bytes",
			fileName,
			maxBackupZipRecordBytes,
		)
	}
	budget.total++
	if budget.total > maxBackupZipRecords {
		return fmt.Errorf(
			"ZIP backup exceeds record limit (%d)",
			maxBackupZipRecords,
		)
	}
	return nil
}

func importBackupZipEntries(
	ctx context.Context,
	archive *validatedBackupZip,
	tx *gorm.DB,
	state *dbImportState,
	result *model.DBImportResult,
) error {
	budget := &backupZipRecordBudget{}
	importDump := func(dump *model.DBDump) error {
		dump.Version = archive.manifest.Version
		dump.ExportedAt = archive.exportedAt
		return importDBDump(tx, dump, state, result)
	}

	sourceJWT := ""
	if err := streamBackupZipJSONArray[model.Setting](
		ctx,
		archive.files["settings.json"],
		budget,
		func(rows []model.Setting) error {
			for _, row := range rows {
				if row.Key == model.SettingKeyJWTSecret {
					sourceJWT = row.Value
				}
			}
			return importDump(&model.DBDump{Settings: rows})
		},
	); err != nil {
		return err
	}
	if err := streamBackupZipJSONArray[model.ClashControllerBackup](
		ctx,
		archive.files["clash_controllers.json"],
		budget,
		func(rows []model.ClashControllerBackup) error {
			dump := &model.DBDump{ClashControllers: rows}
			if sourceJWT != "" {
				dump.Settings = []model.Setting{{
					Key: model.SettingKeyJWTSecret, Value: sourceJWT,
				}}
			}
			return importDump(dump)
		},
	); err != nil {
		return err
	}

	if err := importBackupZipCoreJSON(ctx, archive, budget, importDump); err != nil {
		return err
	}
	if err := importBackupZipStatsJSON(ctx, archive, budget, importDump); err != nil {
		return err
	}
	if err := importBackupZipLogAndUsageEntries(ctx, archive, budget, importDump); err != nil {
		return err
	}
	return nil
}

func importBackupZipCoreJSON(
	ctx context.Context,
	archive *validatedBackupZip,
	budget *backupZipRecordBudget,
	importDump func(*model.DBDump) error,
) error {
	if err := importBackupZipJSONTable(ctx, archive, "proxy_configurations.json", budget, importDump,
		func(dump *model.DBDump, rows []model.ProxyConfiguration) {
			dump.ProxyConfigurations = rows
		}); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "channels.json", budget, importDump,
		func(dump *model.DBDump, rows []model.Channel) { dump.Channels = rows }); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "channel_keys.json", budget, importDump,
		func(dump *model.DBDump, rows []model.ChannelKey) { dump.ChannelKeys = rows }); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "sites.json", budget, importDump,
		func(dump *model.DBDump, rows []model.Site) { dump.Sites = rows }); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "site_accounts.json", budget, importDump,
		func(dump *model.DBDump, rows []model.SiteAccount) { dump.SiteAccounts = rows }); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "site_proxy_preferences.json", budget, importDump,
		func(dump *model.DBDump, rows []model.SiteProxyPreference) {
			dump.SiteProxyPreferences = rows
		}); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "site_tokens.json", budget, importDump,
		func(dump *model.DBDump, rows []model.SiteToken) { dump.SiteTokens = rows }); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "site_user_groups.json", budget, importDump,
		func(dump *model.DBDump, rows []model.SiteUserGroup) {
			dump.SiteUserGroups = rows
		}); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "site_models.json", budget, importDump,
		func(dump *model.DBDump, rows []model.SiteModel) { dump.SiteModels = rows }); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "site_channel_bindings.json", budget, importDump,
		func(dump *model.DBDump, rows []model.SiteChannelBinding) {
			dump.SiteChannelBindings = rows
		}); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "canonical_models.json", budget, importDump,
		func(dump *model.DBDump, rows []model.CanonicalModel) {
			dump.CanonicalModels = rows
		}); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "groups.json", budget, importDump,
		func(dump *model.DBDump, rows []model.Group) { dump.Groups = rows }); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "group_items.json", budget, importDump,
		func(dump *model.DBDump, rows []model.GroupItem) { dump.GroupItems = rows }); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "llm_infos.json", budget, importDump,
		func(dump *model.DBDump, rows []model.LLMInfo) { dump.LLMInfos = rows }); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "api_keys.json", budget, importDump,
		func(dump *model.DBDump, rows []model.APIKey) { dump.APIKeys = rows }); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "model_aliases.json", budget, importDump,
		func(dump *model.DBDump, rows []model.ModelAlias) { dump.ModelAliases = rows }); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "route_candidates.json", budget, importDump,
		func(dump *model.DBDump, rows []model.RouteCandidate) {
			dump.RouteCandidates = rows
		}); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "header_policies.json", budget, importDump,
		func(dump *model.DBDump, rows []model.HeaderPolicy) { dump.HeaderPolicies = rows }); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "user_agent_profiles.json", budget, importDump,
		func(dump *model.DBDump, rows []model.UserAgentProfile) {
			dump.UserAgentProfiles = rows
		}); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "currency_rates.json", budget, importDump,
		func(dump *model.DBDump, rows []model.CurrencyRate) { dump.CurrencyRates = rows }); err != nil {
		return err
	}
	return importBackupZipJSONTable(ctx, archive, "site_model_price_quotes.json", budget, importDump,
		func(dump *model.DBDump, rows []model.SiteModelPriceQuote) {
			dump.SiteModelPriceQuotes = rows
		})
}

func importBackupZipStatsJSON(
	ctx context.Context,
	archive *validatedBackupZip,
	budget *backupZipRecordBudget,
	importDump func(*model.DBDump) error,
) error {
	if !archive.manifest.IncludeStats {
		return nil
	}
	if err := importBackupZipJSONTable(ctx, archive, "stats_total.json", budget, importDump,
		func(dump *model.DBDump, rows []model.StatsTotal) {
			dump.IncludeStats = true
			dump.StatsTotal = rows
		}); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "stats_daily.json", budget, importDump,
		func(dump *model.DBDump, rows []model.StatsDaily) {
			dump.IncludeStats = true
			dump.StatsDaily = rows
		}); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "stats_hourly.json", budget, importDump,
		func(dump *model.DBDump, rows []model.StatsHourly) {
			dump.IncludeStats = true
			dump.StatsHourly = rows
		}); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "stats_model.json", budget, importDump,
		func(dump *model.DBDump, rows []model.StatsModel) {
			dump.IncludeStats = true
			dump.StatsModel = rows
		}); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "stats_channel.json", budget, importDump,
		func(dump *model.DBDump, rows []model.StatsChannel) {
			dump.IncludeStats = true
			dump.StatsChannel = rows
		}); err != nil {
		return err
	}
	if err := importBackupZipJSONTable(ctx, archive, "stats_api_key.json", budget, importDump,
		func(dump *model.DBDump, rows []model.StatsAPIKey) {
			dump.IncludeStats = true
			dump.StatsAPIKey = rows
		}); err != nil {
		return err
	}
	return importBackupZipJSONTable(ctx, archive, "stats_site_model_hourly.json", budget, importDump,
		func(dump *model.DBDump, rows []model.StatsSiteModelHourly) {
			dump.IncludeStats = true
			dump.StatsSiteModelHourly = rows
		})
}

func importBackupZipLogAndUsageEntries(
	ctx context.Context,
	archive *validatedBackupZip,
	budget *backupZipRecordBudget,
	importDump func(*model.DBDump) error,
) error {
	if archive.manifest.IncludeLogs {
		if err := importBackupZipNDJSONTable(ctx, archive, "relay_logs.ndjson", budget, importDump,
			func(dump *model.DBDump, rows []model.RelayLog) {
				dump.IncludeLogs = true
				dump.RelayLogs = rows
			}); err != nil {
			return err
		}
		if err := importBackupZipJSONTable(ctx, archive, "relay_log_repair_audits.json", budget, importDump,
			func(dump *model.DBDump, rows []model.RelayLogRepairAudit) {
				dump.IncludeLogs = true
				dump.RelayLogRepairAudits = rows
			}); err != nil {
			return err
		}
		if err := importBackupZipNDJSONTable(ctx, archive, "site_operation_attempts.ndjson", budget, importDump,
			func(dump *model.DBDump, rows []model.SiteOperationAttempt) {
				dump.IncludeLogs = true
				dump.SiteOperationAttempts = rows
			}); err != nil {
			return err
		}
	}
	if !archive.manifest.IncludeStats {
		return nil
	}
	if err := importBackupZipNDJSONTable(ctx, archive, "usage_aggregates.ndjson", budget, importDump,
		func(dump *model.DBDump, rows []model.UsageAggregate) {
			dump.IncludeStats = true
			dump.UsageAggregates = rows
		}); err != nil {
		return err
	}
	if err := importBackupZipNDJSONTable(ctx, archive, "usage_request_facts.ndjson", budget, importDump,
		func(dump *model.DBDump, rows []model.UsageRequestFact) {
			dump.IncludeStats = true
			dump.UsageRequestFacts = rows
		}); err != nil {
		return err
	}
	return importBackupZipNDJSONTable(ctx, archive, "usage_attempt_facts.ndjson", budget, importDump,
		func(dump *model.DBDump, rows []model.UsageAttemptFact) {
			dump.IncludeStats = true
			dump.UsageAttemptFacts = rows
		})
}

func importBackupZipJSONTable[T any](
	ctx context.Context,
	archive *validatedBackupZip,
	name string,
	budget *backupZipRecordBudget,
	importDump func(*model.DBDump) error,
	assign func(*model.DBDump, []T),
) error {
	return streamBackupZipJSONArray(ctx, archive.files[name], budget, func(rows []T) error {
		dump := &model.DBDump{}
		assign(dump, rows)
		return importDump(dump)
	})
}

func importBackupZipNDJSONTable[T any](
	ctx context.Context,
	archive *validatedBackupZip,
	name string,
	budget *backupZipRecordBudget,
	importDump func(*model.DBDump) error,
	assign func(*model.DBDump, []T),
) error {
	return streamBackupZipNDJSON(ctx, archive.files[name], budget, func(rows []T) error {
		dump := &model.DBDump{}
		assign(dump, rows)
		return importDump(dump)
	})
}

func streamBackupZipJSONArray[T any](
	ctx context.Context,
	file *zip.File,
	budget *backupZipRecordBudget,
	importBatch func([]T) error,
) error {
	if file == nil {
		return nil
	}
	reader, closeEntry, err := newBackupZipRecordReader(ctx, file)
	if err != nil {
		return err
	}
	defer closeEntry()

	marker, err := readBackupJSONMarker(reader)
	if err != nil {
		return fmt.Errorf("decode ZIP entry %q: %w", file.Name, err)
	}
	if marker == 'n' {
		if err := consumeBackupJSONNull(reader); err != nil {
			return fmt.Errorf("decode ZIP entry %q: %w", file.Name, err)
		}
		return ensureBackupJSONWhitespaceEOF(reader, file.Name)
	}
	if marker != '[' {
		return fmt.Errorf("decode ZIP entry %q: expected JSON array", file.Name)
	}

	batch := make([]T, 0, dbImportBatchSize)
	afterComma := false
	for {
		marker, err = readBackupJSONMarker(reader)
		if err != nil {
			return fmt.Errorf("decode ZIP entry %q: %w", file.Name, err)
		}
		if marker == ']' {
			if afterComma {
				return fmt.Errorf("decode ZIP entry %q: trailing array delimiter", file.Name)
			}
			break
		}
		afterComma = false
		if err := reader.UnreadByte(); err != nil {
			return fmt.Errorf("decode ZIP entry %q: %w", file.Name, err)
		}
		raw, nextReader, err := readLimitedBackupJSONValue(reader, file.Name)
		if err != nil {
			return err
		}
		reader = nextReader
		if err := validateBackupJSONRecord(raw, file.Name, budget); err != nil {
			return err
		}
		var item T
		if err := json.Unmarshal(raw, &item); err != nil {
			return fmt.Errorf("decode ZIP entry %q: %w", file.Name, err)
		}
		batch = append(batch, item)
		if len(batch) == dbImportBatchSize {
			if err := importBatch(batch); err != nil {
				return err
			}
			clear(batch)
			batch = batch[:0]
		}
		marker, err = readBackupJSONMarker(reader)
		if err != nil {
			return fmt.Errorf("decode ZIP entry %q: %w", file.Name, err)
		}
		if marker == ']' {
			break
		}
		if marker != ',' {
			return fmt.Errorf("decode ZIP entry %q: expected array delimiter", file.Name)
		}
		afterComma = true
	}
	if len(batch) > 0 {
		if err := importBatch(batch); err != nil {
			return err
		}
	}
	return ensureBackupJSONWhitespaceEOF(reader, file.Name)
}

func streamBackupZipNDJSON[T any](
	ctx context.Context,
	file *zip.File,
	budget *backupZipRecordBudget,
	importBatch func([]T) error,
) error {
	if file == nil {
		return nil
	}
	reader, err := file.Open()
	if err != nil {
		return fmt.Errorf("open ZIP entry %q: %w", file.Name, err)
	}
	defer reader.Close()

	scanner := bufio.NewScanner(&contextLimitReader{
		ctx:       ctx,
		reader:    reader,
		remaining: int64(file.UncompressedSize64) + 1,
	})
	scanner.Buffer(make([]byte, 64<<10), maxBackupZipRecordBytes+1)
	batch := make([]T, 0, dbImportBatchSize)
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		if err := validateBackupJSONRecord(raw, file.Name, budget); err != nil {
			return err
		}
		var item T
		if err := json.Unmarshal(raw, &item); err != nil {
			return fmt.Errorf("decode ZIP entry %q: %w", file.Name, err)
		}
		batch = append(batch, item)
		if len(batch) == dbImportBatchSize {
			if err := importBatch(batch); err != nil {
				return err
			}
			clear(batch)
			batch = batch[:0]
		}
	}
	if err := scanner.Err(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("decode ZIP entry %q: record exceeds size limit: %w", file.Name, err)
	}
	if len(batch) == 0 {
		return nil
	}
	return importBatch(batch)
}

func newBackupZipRecordReader(
	ctx context.Context,
	file *zip.File,
) (*bufio.Reader, func(), error) {
	reader, err := file.Open()
	if err != nil {
		return nil, nil, fmt.Errorf("open ZIP entry %q: %w", file.Name, err)
	}
	buffered := bufio.NewReader(&contextLimitReader{
		ctx:       ctx,
		reader:    reader,
		remaining: int64(file.UncompressedSize64) + 1,
	})
	return buffered, func() { _ = reader.Close() }, nil
}

func readLimitedBackupJSONValue(
	reader *bufio.Reader,
	fileName string,
) (json.RawMessage, *bufio.Reader, error) {
	limited := &io.LimitedReader{
		R: reader,
		N: maxBackupZipRecordBytes + 1,
	}
	decoder := json.NewDecoder(limited)
	var raw json.RawMessage
	err := decoder.Decode(&raw)
	buffered, bufferErr := io.ReadAll(decoder.Buffered())
	if bufferErr != nil {
		return nil, reader, fmt.Errorf("decode ZIP entry %q: %w", fileName, bufferErr)
	}
	nextReader := bufio.NewReader(io.MultiReader(bytes.NewReader(buffered), reader))
	if err != nil {
		if limited.N == 0 {
			return nil, nextReader, fmt.Errorf(
				"ZIP entry %q contains a record larger than %d bytes",
				fileName,
				maxBackupZipRecordBytes,
			)
		}
		return nil, nextReader, fmt.Errorf("decode ZIP entry %q: %w", fileName, err)
	}
	if len(raw) > maxBackupZipRecordBytes {
		return nil, nextReader, fmt.Errorf(
			"ZIP entry %q contains a record larger than %d bytes",
			fileName,
			maxBackupZipRecordBytes,
		)
	}
	return raw, nextReader, nil
}

func validateBackupJSONRecord(
	raw []byte,
	fileName string,
	budget *backupZipRecordBudget,
) error {
	if err := budget.consume(fileName, int64(len(raw))); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	for tokens := 1; ; tokens++ {
		if tokens > maxBackupZipRecordTokens {
			return fmt.Errorf(
				"ZIP entry %q contains a record with too many JSON tokens",
				fileName,
			)
		}
		if _, err := decoder.Token(); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return fmt.Errorf("decode ZIP entry %q: %w", fileName, err)
		}
	}
}

func readBackupJSONMarker(reader *bufio.Reader) (byte, error) {
	for {
		value, err := reader.ReadByte()
		if err != nil {
			return 0, err
		}
		if !isBackupJSONSpace(value) {
			return value, nil
		}
	}
}

func consumeBackupJSONNull(reader *bufio.Reader) error {
	suffix := make([]byte, 3)
	if _, err := io.ReadFull(reader, suffix); err != nil {
		return err
	}
	if string(suffix) != "ull" {
		return fmt.Errorf("invalid JSON null")
	}
	return nil
}

func ensureBackupJSONWhitespaceEOF(reader *bufio.Reader, fileName string) error {
	for {
		value, err := reader.ReadByte()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("decode ZIP entry %q: %w", fileName, err)
		}
		if !isBackupJSONSpace(value) {
			return fmt.Errorf("decode ZIP entry %q: trailing JSON value", fileName)
		}
	}
}

func isBackupJSONSpace(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}
