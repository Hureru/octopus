package op

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

const (
	maxBackupZipCompressedBytes   = 128 << 20
	maxBackupZipUncompressedBytes = 256 << 20
	maxBackupZipEntryBytes        = 128 << 20
	maxBackupZipRecordBytes       = 4 << 20
	maxBackupZipRecordTokens      = 100_000
	maxBackupZipRecords           = 2_000_000
	maxBackupZipManifestBytes     = 64 << 10
)

type backupZipManifest struct {
	Version      int    `json:"version"`
	ExportedAt   string `json:"exported_at"`
	IncludeLogs  bool   `json:"include_logs"`
	IncludeStats bool   `json:"include_stats"`
	Format       string `json:"format"`
}

type validatedBackupZip struct {
	manifest   backupZipManifest
	exportedAt time.Time
	files      map[string]*zip.File
}

func DBImportZip(
	ctx context.Context,
	reader io.ReaderAt,
	size int64,
) (*model.DBImportResult, error) {
	archive, err := openBackupZip(ctx, reader, size)
	if err != nil {
		return nil, err
	}
	return runDBImportTransaction(ctx, func(
		tx *gorm.DB,
		state *dbImportState,
		result *model.DBImportResult,
	) error {
		return importBackupZipEntries(ctx, archive, tx, state, result)
	})
}

func openBackupZip(
	ctx context.Context,
	reader io.ReaderAt,
	size int64,
) (*validatedBackupZip, error) {
	if reader == nil || size <= 0 {
		return nil, fmt.Errorf("empty ZIP backup")
	}
	if size > maxBackupZipCompressedBytes {
		return nil, fmt.Errorf(
			"ZIP backup exceeds compressed size limit (%d bytes)",
			maxBackupZipCompressedBytes,
		)
	}
	if err := validateBackupZipDirectoryCount(reader, size); err != nil {
		return nil, err
	}
	archive, err := zip.NewReader(reader, size)
	if err != nil {
		return nil, fmt.Errorf("open ZIP backup: %w", err)
	}
	files, err := validateBackupZipEntries(archive.File)
	if err != nil {
		return nil, err
	}

	var manifest backupZipManifest
	if files["manifest.json"].UncompressedSize64 > maxBackupZipManifestBytes {
		return nil, fmt.Errorf("ZIP manifest exceeds size limit")
	}
	if err := decodeBackupZipJSON(ctx, files["manifest.json"], &manifest); err != nil {
		return nil, err
	}
	if manifest.Format != "zip-v1" {
		return nil, fmt.Errorf("unsupported ZIP backup format %q", manifest.Format)
	}
	if manifest.Version < 0 || manifest.Version > dbDumpVersion {
		return nil, fmt.Errorf("unsupported dump version: %d", manifest.Version)
	}
	exportedAt, err := time.Parse(time.RFC3339, manifest.ExportedAt)
	if err != nil {
		return nil, fmt.Errorf("invalid ZIP manifest exported_at: %w", err)
	}
	if err := validateBackupZipManifestFiles(manifest, files); err != nil {
		return nil, err
	}
	return &validatedBackupZip{
		manifest:   manifest,
		exportedAt: exportedAt,
		files:      files,
	}, nil
}

func validateBackupZipDirectoryCount(reader io.ReaderAt, size int64) error {
	const (
		endHeaderSize  = 22
		maxCommentSize = 1<<16 - 1
	)
	readSize := size
	if limit := int64(endHeaderSize + maxCommentSize); readSize > limit {
		readSize = limit
	}
	tail := make([]byte, int(readSize))
	if _, err := reader.ReadAt(tail, size-readSize); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read ZIP directory: %w", err)
	}
	signature := []byte{'P', 'K', 0x05, 0x06}
	for offset := len(tail) - endHeaderSize; offset >= 0; offset-- {
		if !bytes.Equal(tail[offset:offset+len(signature)], signature) {
			continue
		}
		commentSize := int(binary.LittleEndian.Uint16(tail[offset+20 : offset+22]))
		if offset+endHeaderSize+commentSize != len(tail) {
			continue
		}
		if binary.LittleEndian.Uint16(tail[offset+4:offset+6]) != 0 ||
			binary.LittleEndian.Uint16(tail[offset+6:offset+8]) != 0 {
			return fmt.Errorf("multi-disk ZIP backups are not supported")
		}
		entriesOnDisk := binary.LittleEndian.Uint16(tail[offset+8 : offset+10])
		entryCount := binary.LittleEndian.Uint16(tail[offset+10 : offset+12])
		if entryCount == 0xffff {
			return fmt.Errorf("ZIP64 backup directories are not supported")
		}
		if entriesOnDisk != entryCount {
			return fmt.Errorf("ZIP central directory entry count mismatch")
		}
		if int(entryCount) > len(backupZipAllowedFiles()) {
			return fmt.Errorf("ZIP backup contains too many entries: %d", entryCount)
		}
		directorySize := int64(binary.LittleEndian.Uint32(tail[offset+12 : offset+16]))
		directoryOffset := int64(binary.LittleEndian.Uint32(tail[offset+16 : offset+20]))
		endOffset := size - readSize + int64(offset)
		if directoryOffset < 0 ||
			directorySize < 0 ||
			directoryOffset > endOffset ||
			directorySize > endOffset-directoryOffset {
			return fmt.Errorf("ZIP central directory bounds are invalid")
		}
		actualCount, err := scanBackupZipCentralDirectory(
			reader,
			directoryOffset,
			directorySize,
		)
		if err != nil {
			return err
		}
		if actualCount != int(entryCount) {
			return fmt.Errorf(
				"ZIP central directory entry count mismatch: header=%d actual=%d",
				entryCount,
				actualCount,
			)
		}
		return nil
	}
	return fmt.Errorf("open ZIP backup: end-of-directory record not found")
}

func scanBackupZipCentralDirectory(
	reader io.ReaderAt,
	offset int64,
	size int64,
) (int, error) {
	const centralHeaderSize = 46
	end := offset + size
	count := 0
	for offset < end {
		if end-offset < centralHeaderSize {
			return 0, fmt.Errorf("ZIP central directory is truncated")
		}
		header := make([]byte, centralHeaderSize)
		if _, err := reader.ReadAt(header, offset); err != nil {
			return 0, fmt.Errorf("read ZIP central directory: %w", err)
		}
		if binary.LittleEndian.Uint32(header[:4]) != 0x02014b50 {
			return 0, fmt.Errorf("ZIP central directory header is invalid")
		}
		count++
		if count > len(backupZipAllowedFiles()) {
			return 0, fmt.Errorf("ZIP central directory contains too many entries")
		}
		nameSize := int64(binary.LittleEndian.Uint16(header[28:30]))
		extraSize := int64(binary.LittleEndian.Uint16(header[30:32]))
		commentSize := int64(binary.LittleEndian.Uint16(header[32:34]))
		recordSize := int64(centralHeaderSize) + nameSize + extraSize + commentSize
		if recordSize > end-offset {
			return 0, fmt.Errorf("ZIP central directory entry is truncated")
		}
		offset += recordSize
	}
	if offset != end {
		return 0, fmt.Errorf("ZIP central directory size mismatch")
	}
	return count, nil
}

func validateBackupZipEntries(files []*zip.File) (map[string]*zip.File, error) {
	allowed := backupZipAllowedFiles()
	result := make(map[string]*zip.File, len(files))
	var total uint64
	for _, file := range files {
		name := strings.TrimSpace(file.Name)
		if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") ||
			strings.Contains(name, "..") {
			return nil, fmt.Errorf("invalid ZIP entry %q", file.Name)
		}
		if _, ok := allowed[name]; !ok {
			return nil, fmt.Errorf("unexpected ZIP entry %q", name)
		}
		if _, duplicate := result[name]; duplicate {
			return nil, fmt.Errorf("duplicate ZIP entry %q", name)
		}
		if file.UncompressedSize64 > maxBackupZipEntryBytes {
			return nil, fmt.Errorf("ZIP entry %q exceeds size limit", name)
		}
		if total > ^uint64(0)-file.UncompressedSize64 {
			return nil, fmt.Errorf("ZIP uncompressed size overflow")
		}
		total += file.UncompressedSize64
		if total > maxBackupZipUncompressedBytes {
			return nil, fmt.Errorf(
				"ZIP backup exceeds uncompressed size limit (%d bytes)",
				maxBackupZipUncompressedBytes,
			)
		}
		result[name] = file
	}
	if result["manifest.json"] == nil {
		return nil, fmt.Errorf("ZIP backup is missing manifest.json")
	}
	return result, nil
}

func backupZipAllowedFiles() map[string]struct{} {
	names := []string{
		"manifest.json",
		"channels.json", "channel_keys.json", "proxy_configurations.json",
		"sites.json", "site_accounts.json", "site_tokens.json",
		"site_user_groups.json", "site_models.json", "site_channel_bindings.json",
		"groups.json", "group_items.json", "llm_infos.json", "api_keys.json",
		"settings.json", "canonical_models.json", "model_aliases.json",
		"route_candidates.json", "header_policies.json", "user_agent_profiles.json",
		"site_model_price_quotes.json", "currency_rates.json",
		"clash_controllers.json", "site_proxy_preferences.json",
		"stats_total.json", "stats_daily.json", "stats_hourly.json",
		"stats_model.json", "stats_channel.json", "stats_api_key.json",
		"stats_site_model_hourly.json", "relay_log_repair_audits.json",
		"relay_logs.ndjson", "usage_request_facts.ndjson",
		"usage_attempt_facts.ndjson", "usage_aggregates.ndjson",
		"site_operation_attempts.ndjson",
	}
	result := make(map[string]struct{}, len(names))
	for _, name := range names {
		result[name] = struct{}{}
	}
	return result
}

func validateBackupZipManifestFiles(
	manifest backupZipManifest,
	files map[string]*zip.File,
) error {
	logFiles := []string{
		"relay_logs.ndjson",
		"relay_log_repair_audits.json",
		"site_operation_attempts.ndjson",
	}
	statsFiles := []string{
		"stats_total.json", "stats_daily.json", "stats_hourly.json",
		"stats_model.json", "stats_channel.json", "stats_api_key.json",
		"stats_site_model_hourly.json", "usage_request_facts.ndjson",
		"usage_attempt_facts.ndjson", "usage_aggregates.ndjson",
	}
	for _, name := range logFiles {
		if manifest.IncludeLogs && files[name] == nil {
			return fmt.Errorf("ZIP manifest includes logs but is missing required entry %q", name)
		}
		if !manifest.IncludeLogs && files[name] != nil {
			return fmt.Errorf("ZIP manifest excludes logs but contains %q", name)
		}
	}
	for _, name := range statsFiles {
		if manifest.IncludeStats && files[name] == nil {
			return fmt.Errorf("ZIP manifest includes stats but is missing required entry %q", name)
		}
		if !manifest.IncludeStats && files[name] != nil {
			return fmt.Errorf("ZIP manifest excludes stats but contains %q", name)
		}
	}
	return nil
}

func decodeBackupZipJSON(ctx context.Context, file *zip.File, target any) error {
	if file == nil {
		return nil
	}
	reader, err := file.Open()
	if err != nil {
		return fmt.Errorf("open ZIP entry %q: %w", file.Name, err)
	}
	defer reader.Close()
	decoder := json.NewDecoder(&contextLimitReader{
		ctx:       ctx,
		reader:    reader,
		remaining: int64(file.UncompressedSize64) + 1,
	})
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode ZIP entry %q: %w", file.Name, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode ZIP entry %q: trailing JSON value", file.Name)
		}
		return fmt.Errorf("decode ZIP entry %q: %w", file.Name, err)
	}
	return nil
}

type contextLimitReader struct {
	ctx       context.Context
	reader    io.Reader
	remaining int64
}

func (reader *contextLimitReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	if reader.remaining <= 0 {
		return 0, fmt.Errorf("ZIP entry exceeded declared size")
	}
	if int64(len(buffer)) > reader.remaining {
		buffer = buffer[:reader.remaining]
	}
	n, err := reader.reader.Read(buffer)
	reader.remaining -= int64(n)
	return n, err
}
