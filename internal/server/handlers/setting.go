package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/bestruirui/octopus/internal/task"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/gin-gonic/gin"
)

var projectedAutoGroupQueued atomic.Bool

const maxDBImportUploadBytes = 128 << 20
const maxDBImportMultipartOverhead = 1 << 20

func init() {
	router.NewGroupRouter("/api/v1/setting").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(getSettingList),
		).
		AddRoute(
			router.NewRoute("/set", http.MethodPost).
				Use(middleware.RequireJSON()).
				Handle(setSetting),
		).
		AddRoute(
			router.NewRoute("/export", http.MethodGet).
				Handle(exportDB),
		).
		AddRoute(
			router.NewRoute("/import", http.MethodPost).
				Handle(importDB),
		)
}

func getSettingList(c *gin.Context) {
	settings, err := op.SettingList(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, settings)
}

func setSetting(c *gin.Context) {
	var setting model.Setting
	if err := c.ShouldBindJSON(&setting); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := setting.Validate(); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if setting.Key == model.SettingKeyJWTSecret {
		resp.Error(c, http.StatusBadRequest, "setting cannot be changed through this endpoint")
		return
	}
	if err := op.SettingSetString(setting.Key, setting.Value); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	switch setting.Key {
	case model.SettingKeyModelInfoUpdateInterval,
		model.SettingKeySyncLLMInterval,
		model.SettingKeySiteSyncInterval,
		model.SettingKeySiteCheckinInterval,
		model.SettingKeyStatsSaveInterval,
		model.SettingKeyOutlierRetireInterval,
		model.SettingKeyWebDAVBackupInterval:
		if err := task.UpdateSettingInterval(setting.Key, setting.Value); err != nil {
			resp.Error(c, http.StatusInternalServerError, err.Error())
			return
		}
	case model.SettingKeyProjectedChannelAutoGroupEnabled:
		mode, _ := model.ParseAutoGroupSettingValue(setting.Value)
		if mode != model.AutoGroupTypeNone && projectedAutoGroupQueued.CompareAndSwap(false, true) {
			safe.Go("projected-channel-auto-group-all", func() {
				defer projectedAutoGroupQueued.Store(false)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				if err := op.AutoGroupAllProjectedChannels(ctx); err != nil {
					log.Warnf("failed to auto group all projected channels: %v", err)
				}
			})
		}
	}
	if responseSetting, ok := op.SettingForClient(setting); ok {
		resp.Success(c, responseSetting)
		return
	}
	resp.Success(c, nil)
}

func exportDB(c *gin.Context) {
	includeLogs, _ := strconv.ParseBool(c.DefaultQuery("include_logs", "false"))
	includeStats, _ := strconv.ParseBool(c.DefaultQuery("include_stats", "false"))
	format := strings.ToLower(strings.TrimSpace(c.DefaultQuery("format", "json")))
	if format != "json" && format != "zip" {
		resp.Error(c, http.StatusBadRequest, "invalid format")
		return
	}
	if err := validateDBExportOptions(format, includeLogs, includeStats); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if format == "zip" {
		filename := "octopus-export-" + time.Now().Format("20060102150405") + ".zip"
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
		wrapper := &countingResponseWriter{ResponseWriter: c.Writer}
		if err := op.DBExportZip(c.Request.Context(), wrapper, includeLogs, includeStats); err != nil {
			if wrapper.bytesWritten == 0 {
				c.Header("Content-Type", "application/json")
				c.Header("Content-Disposition", "")
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": err.Error()})
				return
			}
			// Headers already sent; we can't switch to a JSON error. Log it and
			// let the client surface the truncated download.
			log.Warnf("zip export failed mid-stream: %v", err)
		}
		return
	}

	dump, err := op.DBExportAll(c.Request.Context(), includeLogs, includeStats)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.Header("Content-Type", "application/json")
	c.Header("Content-Disposition", "attachment; filename=\"octopus-export-"+time.Now().Format("20060102150405")+".json\"")
	c.JSON(http.StatusOK, dump)
}

func validateDBExportOptions(format string, includeLogs, includeStats bool) error {
	if format == "json" && includeLogs {
		return fmt.Errorf("JSON exports cannot include logs; use ZIP format")
	}
	if format == "json" && includeStats {
		return fmt.Errorf("JSON exports cannot include stats; use ZIP format")
	}
	return nil
}

func importDB(c *gin.Context) {
	var dump model.DBDump
	var result *model.DBImportResult
	var importErr error

	contentType := c.GetHeader("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		if c.Request.ContentLength > maxDBImportUploadBytes+maxDBImportMultipartOverhead {
			resp.Error(c, http.StatusRequestEntityTooLarge, "backup file exceeds upload limit")
			return
		}
		c.Request.Body = http.MaxBytesReader(
			c.Writer,
			c.Request.Body,
			maxDBImportUploadBytes+maxDBImportMultipartOverhead,
		)
		fh, err := c.FormFile("file")
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				resp.Error(c, http.StatusRequestEntityTooLarge, "backup file exceeds upload limit")
				return
			}
			resp.Error(c, http.StatusBadRequest, "missing upload file field 'file'")
			return
		}
		f, err := fh.Open()
		if err != nil {
			resp.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		defer f.Close()
		if fh.Size > maxDBImportUploadBytes {
			resp.Error(c, http.StatusRequestEntityTooLarge, "backup file exceeds upload limit")
			return
		}
		if isZipBackup(
			fh.Filename,
			fh.Header.Get("Content-Type"),
			f,
		) {
			result, importErr = op.DBImportZip(c.Request.Context(), f, fh.Size)
		} else {
			body, err := readLimitedBackup(f, maxDBImportUploadBytes)
			if err != nil {
				resp.Error(c, http.StatusBadRequest, err.Error())
				return
			}
			importErr = decodeDBDump(body, &dump)
		}
	} else {
		if strings.Contains(contentType, "application/zip") {
			temp, err := os.CreateTemp("", "octopus-import-*.zip")
			if err != nil {
				resp.Error(c, http.StatusBadRequest, err.Error())
				return
			}
			tempName := temp.Name()
			defer func() {
				_ = temp.Close()
				_ = os.Remove(tempName)
			}()
			written, err := io.Copy(
				temp,
				io.LimitReader(c.Request.Body, maxDBImportUploadBytes+1),
			)
			if err != nil {
				resp.Error(c, http.StatusBadRequest, err.Error())
				return
			}
			if written > maxDBImportUploadBytes {
				resp.Error(c, http.StatusRequestEntityTooLarge, "backup file exceeds upload limit")
				return
			}
			result, importErr = op.DBImportZip(c.Request.Context(), temp, written)
		} else {
			body, err := readLimitedBackup(c.Request.Body, maxDBImportUploadBytes)
			if err != nil {
				resp.Error(c, http.StatusBadRequest, err.Error())
				return
			}
			importErr = decodeDBDump(body, &dump)
		}
	}

	if importErr != nil {
		status := http.StatusBadRequest
		if errors.Is(importErr, errBackupUploadTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		resp.Error(c, status, importErr.Error())
		return
	}
	if result == nil {
		result, importErr = op.DBImportIncremental(c.Request.Context(), &dump)
		if importErr != nil {
			resp.Error(c, http.StatusBadRequest, importErr.Error())
			return
		}
	}

	if err := op.InitCache(); err != nil {
		log.Warnf("cache refresh after import failed: %v", err)
	}
	if err := task.ReloadSettingIntervals(); err != nil {
		log.Warnf("scheduler interval refresh after import failed: %v", err)
	}

	resp.Success(c, result)
}

func readLimitedBackup(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errBackupUploadTooLarge
	}
	return body, nil
}

var errBackupUploadTooLarge = errors.New("backup file exceeds upload limit")

func isZipBackup(filename, contentType string, reader io.ReaderAt) bool {
	if strings.EqualFold(filepath.Ext(filename), ".zip") ||
		strings.Contains(strings.ToLower(contentType), "application/zip") {
		return true
	}
	var signature [4]byte
	n, err := reader.ReadAt(signature[:], 0)
	return err == nil && n == len(signature) &&
		string(signature[:]) == "PK\x03\x04"
}

func decodeDBDump(body []byte, dump *model.DBDump) error {
	if dump == nil {
		return json.Unmarshal(body, &struct{}{})
	}

	if err := json.Unmarshal(body, dump); err != nil {
		return err
	}

	if dump.Version == 0 &&
		len(dump.Channels) == 0 &&
		len(dump.Sites) == 0 &&
		len(dump.SiteAccounts) == 0 &&
		len(dump.SiteTokens) == 0 &&
		len(dump.SiteUserGroups) == 0 &&
		len(dump.SiteModels) == 0 &&
		len(dump.SiteChannelBindings) == 0 &&
		len(dump.Groups) == 0 &&
		len(dump.GroupItems) == 0 &&
		len(dump.Settings) == 0 &&
		len(dump.APIKeys) == 0 &&
		len(dump.LLMInfos) == 0 &&
		len(dump.RelayLogs) == 0 &&
		len(dump.StatsDaily) == 0 &&
		len(dump.StatsHourly) == 0 &&
		len(dump.StatsTotal) == 0 &&
		len(dump.StatsChannel) == 0 &&
		len(dump.StatsModel) == 0 &&
		len(dump.StatsAPIKey) == 0 {
		var wrapper struct {
			Code    int             `json:"code"`
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &wrapper); err == nil && len(wrapper.Data) > 0 {
			return json.Unmarshal(wrapper.Data, dump)
		}
	}

	return nil
}

// countingResponseWriter wraps gin.ResponseWriter to track whether the body
// has started, so callers can choose to emit a JSON error if the underlying
// stream failed before any bytes were committed.
type countingResponseWriter struct {
	gin.ResponseWriter
	bytesWritten int64
}

func (w *countingResponseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += int64(n)
	return n, err
}

func (w *countingResponseWriter) WriteString(s string) (int, error) {
	n, err := w.ResponseWriter.WriteString(s)
	w.bytesWritten += int64(n)
	return n, err
}
