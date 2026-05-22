package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
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
	if err := op.SettingSetString(setting.Key, setting.Value); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	switch setting.Key {
	case model.SettingKeyModelInfoUpdateInterval:
		hours, err := strconv.Atoi(setting.Value)
		if err != nil {
			resp.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		task.Update(string(setting.Key), time.Duration(hours)*time.Hour)
	case model.SettingKeySyncLLMInterval:
		hours, err := strconv.Atoi(setting.Value)
		if err != nil {
			resp.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		task.Update(string(setting.Key), time.Duration(hours)*time.Hour)
	case model.SettingKeySiteSyncInterval, model.SettingKeySiteCheckinInterval:
		hours, err := strconv.Atoi(setting.Value)
		if err != nil {
			resp.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		task.Update(string(setting.Key), time.Duration(hours)*time.Hour)
	case model.SettingKeyProjectedChannelAutoGroupEnabled:
		if setting.Value == "true" && projectedAutoGroupQueued.CompareAndSwap(false, true) {
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
	resp.Success(c, setting)
}

func exportDB(c *gin.Context) {
	includeLogs, _ := strconv.ParseBool(c.DefaultQuery("include_logs", "false"))
	includeStats, _ := strconv.ParseBool(c.DefaultQuery("include_stats", "false"))
	format := strings.ToLower(strings.TrimSpace(c.DefaultQuery("format", "json")))
	if format != "json" && format != "zip" {
		resp.Error(c, http.StatusBadRequest, "invalid format")
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

func importDB(c *gin.Context) {
	var dump model.DBDump

	contentType := c.GetHeader("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		fh, err := c.FormFile("file")
		if err != nil {
			resp.Error(c, http.StatusBadRequest, "missing upload file field 'file'")
			return
		}
		f, err := fh.Open()
		if err != nil {
			resp.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		defer f.Close()
		body, err := io.ReadAll(f)
		if err != nil {
			resp.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		if err := decodeDBDump(body, &dump); err != nil {
			resp.Error(c, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			resp.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		if err := decodeDBDump(body, &dump); err != nil {
			resp.Error(c, http.StatusBadRequest, err.Error())
			return
		}
	}

	result, err := op.DBImportIncremental(c.Request.Context(), &dump)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := op.InitCache(); err != nil {
		log.Warnf("cache refresh after import failed: %v", err)
	}

	resp.Success(c, result)
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
