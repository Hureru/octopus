package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func init() {
	router.NewGroupRouter("/api/v1/log").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listLog),
		).
		AddRoute(
			router.NewRoute("/site-action-targets", http.MethodGet).
				Handle(getLogSiteActionTargets),
		).
		AddRoute(
			router.NewRoute("/:id", http.MethodGet).
				Handle(getLog),
		).
		AddRoute(
			router.NewRoute("/clear", http.MethodDelete).
				Handle(clearLog),
		).
		AddRoute(
			router.NewRoute("/stream-token", http.MethodGet).
				Handle(getStreamToken),
		)

	router.NewGroupRouter("/api/v1/log").
		AddRoute(
			router.NewRoute("/stream", http.MethodGet).
				Handle(streamLog),
		)
}

func listLog(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	channelIDsStr := c.Query("channel_ids")
	status := op.RelayLogStatusFilter(strings.TrimSpace(c.Query("status")))
	keyword := c.Query("keyword")
	keywordScope := op.RelayLogKeywordScope(strings.TrimSpace(c.Query("keyword_scope")))
	keywordMode := op.RelayLogKeywordMode(strings.TrimSpace(c.Query("keyword_mode")))
	pagination := strings.TrimSpace(c.Query("pagination"))
	includeContent, err := parseBoolQuery(c, "include_content", false)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	withTotal, err := parseBoolQuery(c, "with_total", true)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	var beforeTime, beforeID *int64
	if raw := strings.TrimSpace(c.Query("before_time")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			resp.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		beforeTime = &value
	}
	if raw := strings.TrimSpace(c.Query("before_id")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			resp.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		beforeID = &value
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var startTime, endTime *int
	if startTimeStr != "" {
		st, err := strconv.Atoi(startTimeStr)
		if err != nil {
			resp.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		startTime = &st
	}
	if endTimeStr != "" {
		et, err := strconv.Atoi(endTimeStr)
		if err != nil {
			resp.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		endTime = &et
	}

	if status != op.RelayLogStatusAll && status != op.RelayLogStatusSuccess && status != op.RelayLogStatusError {
		resp.Error(c, http.StatusBadRequest, "invalid status")
		return
	}
	if keywordScope != op.RelayLogKeywordScopeDefault && keywordScope != op.RelayLogKeywordScopeContent {
		resp.Error(c, http.StatusBadRequest, "invalid keyword_scope")
		return
	}
	switch keywordMode {
	case op.RelayLogKeywordModeDefault, op.RelayLogKeywordModePrefix, op.RelayLogKeywordModeExact, op.RelayLogKeywordModeContains:
	default:
		resp.Error(c, http.StatusBadRequest, "invalid keyword_mode")
		return
	}
	switch pagination {
	case "", "cursor", "page":
	default:
		resp.Error(c, http.StatusBadRequest, "invalid pagination")
		return
	}

	var channelIDs []int
	if channelIDsStr != "" {
		for _, item := range strings.Split(channelIDsStr, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			id, err := strconv.Atoi(item)
			if err != nil {
				resp.Error(c, http.StatusBadRequest, err.Error())
				return
			}
			channelIDs = append(channelIDs, id)
		}
	}

	result, err := op.RelayLogListWithFilter(c.Request.Context(), op.RelayLogListFilter{
		StartTime:      startTime,
		EndTime:        endTime,
		ChannelIDs:     channelIDs,
		Status:         status,
		Keyword:        keyword,
		KeywordScope:   keywordScope,
		KeywordMode:    keywordMode,
		Page:           page,
		PageSize:       pageSize,
		IncludeContent: includeContent,
		WithTotal:      withTotal,
		Limit:          limit,
		BeforeTime:     beforeTime,
		BeforeID:       beforeID,
		Pagination:     pagination,
	})
	if err != nil {
		var fe *op.RelayLogFilterError
		if errors.As(err, &fe) {
			resp.Error(c, http.StatusBadRequest, fe.Message)
			return
		}
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	resp.Success(c, gin.H{
		"logs":        result.Logs,
		"total":       result.Total,
		"has_more":    result.HasMore,
		"next_cursor": result.NextCursor,
		"search_mode": result.SearchMode,
		"warning":     result.Warning,
	})
}

func parseBoolQuery(c *gin.Context, key string, defaultValue bool) (bool, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return defaultValue, fmt.Errorf("invalid boolean %q for %s", raw, key)
	}
	return value, nil
}

func getLogSiteActionTargets(c *gin.Context) {
	rawIDs := strings.TrimSpace(c.Query("ids"))
	if rawIDs == "" {
		resp.Success(c, gin.H{})
		return
	}
	parts := strings.Split(rawIDs, ",")
	if len(parts) > 100 {
		resp.Error(c, http.StatusBadRequest, "too many ids")
		return
	}
	ids := make([]int64, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			resp.InvalidParam(c)
			return
		}
		if id <= 0 {
			resp.InvalidParam(c)
			return
		}
		ids = append(ids, id)
	}
	data, err := op.RelayLogSiteActionTargets(c.Request.Context(), ids)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, data)
}

func getLog(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		resp.InvalidParam(c)
		return
	}
	logItem, err := op.RelayLogGet(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			resp.NotFound(c)
			return
		}
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, logItem)
}

func clearLog(c *gin.Context) {
	if err := op.RelayLogClear(c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, nil)
}

func getStreamToken(c *gin.Context) {
	token, err := op.RelayLogStreamTokenCreate()
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, gin.H{"token": token})
}

func streamLog(c *gin.Context) {
	token := c.Query("token")
	if token == "" || !op.RelayLogStreamTokenVerify(token) {
		resp.Error(c, http.StatusUnauthorized, "invalid stream token")
		return
	}

	op.RelayLogStreamTokenRevoke(token)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	logChan := op.RelayLogSubscribe()
	defer op.RelayLogUnsubscribe(logChan)

	ctx := c.Request.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case log, ok := <-logChan:
			if !ok {
				return
			}
			data, err := json.Marshal(log)
			if err != nil {
				continue
			}
			c.Writer.Write([]byte(fmt.Sprintf("data: %s\n\n", data)))
			c.Writer.Flush()
		}
	}
}
