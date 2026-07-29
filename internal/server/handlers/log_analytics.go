package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/log/analytics").
		Use(middleware.Auth()).
		AddRoute(router.NewRoute("/summary", http.MethodGet).Handle(getUsageAnalyticsSummary)).
		AddRoute(router.NewRoute("/timeseries", http.MethodGet).Handle(getUsageAnalyticsTimeseries)).
		AddRoute(router.NewRoute("/breakdown", http.MethodGet).Handle(getUsageAnalyticsBreakdown)).
		AddRoute(router.NewRoute("/export", http.MethodGet).Handle(getUsageAnalyticsExport)).
		AddRoute(router.NewRoute("/dimensions", http.MethodGet).Handle(getUsageAnalyticsDimensions))
}

func getUsageAnalyticsSummary(c *gin.Context) {
	filter, ok := parseUsageAnalyticsFilter(c)
	if !ok {
		return
	}
	result, err := op.UsageAnalyticsSummaryGet(c.Request.Context(), filter)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, result)
}

func getUsageAnalyticsTimeseries(c *gin.Context) {
	filter, ok := parseUsageAnalyticsFilter(c)
	if !ok {
		return
	}
	result, err := op.UsageAnalyticsTimeseriesGet(c.Request.Context(), filter)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, result)
}

func getUsageAnalyticsBreakdown(c *gin.Context) {
	filter, ok := parseUsageAnalyticsFilter(c)
	if !ok {
		return
	}
	options, ok := parseUsageBreakdownOptions(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	result, err := op.UsageAnalyticsBreakdownSearchGet(
		c.Request.Context(),
		filter,
		options.dimension,
		options.search,
		page,
		pageSize,
		options.sort,
		options.descending,
	)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, result)
}

type usageBreakdownOptions struct {
	dimension  string
	search     string
	sort       string
	descending bool
}

func parseUsageBreakdownOptions(c *gin.Context) (usageBreakdownOptions, bool) {
	dimension := strings.TrimSpace(c.DefaultQuery("dimension", "site"))
	if !validUsageDimension(dimension) {
		resp.Error(c, http.StatusBadRequest, "invalid dimension")
		return usageBreakdownOptions{}, false
	}
	sortBy := strings.TrimSpace(c.DefaultQuery("sort", "request_count"))
	if !validUsageSort(sortBy) {
		resp.Error(c, http.StatusBadRequest, "invalid sort")
		return usageBreakdownOptions{}, false
	}
	descending, err := parseBoolQuery(c, "descending", true)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return usageBreakdownOptions{}, false
	}
	return usageBreakdownOptions{
		dimension:  dimension,
		search:     strings.TrimSpace(c.Query("search")),
		sort:       sortBy,
		descending: descending,
	}, true
}

func getUsageAnalyticsDimensions(c *gin.Context) {
	filter, ok := parseUsageAnalyticsFilter(c)
	if !ok {
		return
	}
	dimension := strings.TrimSpace(c.DefaultQuery("dimension", "site"))
	if !validUsageDimension(dimension) {
		resp.Error(c, http.StatusBadRequest, "invalid dimension")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	result, err := op.UsageAnalyticsDimensionsGet(
		c.Request.Context(),
		filter,
		dimension,
		c.Query("search"),
		page,
		pageSize,
	)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, result)
}

func parseUsageAnalyticsFilter(c *gin.Context) (op.UsageAnalyticsFilter, bool) {
	startTime, err := parseOptionalInt64(c.Query("start_time"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid start_time")
		return op.UsageAnalyticsFilter{}, false
	}
	endTime, err := parseOptionalInt64(c.Query("end_time"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid end_time")
		return op.UsageAnalyticsFilter{}, false
	}
	timezone := strings.TrimSpace(c.DefaultQuery("timezone", "UTC"))
	if _, err := time.LoadLocation(timezone); err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid timezone")
		return op.UsageAnalyticsFilter{}, false
	}
	scope := op.UsageMetricScope(strings.TrimSpace(c.DefaultQuery("metric_scope", string(op.UsageMetricScopeRequest))))
	if scope != op.UsageMetricScopeRequest && scope != op.UsageMetricScopeAttempt {
		resp.Error(c, http.StatusBadRequest, "invalid metric_scope")
		return op.UsageAnalyticsFilter{}, false
	}

	siteIDs, err := parseUsageIntList(c.Query("site_ids"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return op.UsageAnalyticsFilter{}, false
	}
	accountIDs, err := parseUsageIntList(c.Query("site_account_ids"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return op.UsageAnalyticsFilter{}, false
	}
	channelIDs, err := parseUsageIntList(c.Query("channel_ids"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return op.UsageAnalyticsFilter{}, false
	}
	apiKeyIDs, err := parseUsageIntList(c.Query("api_key_ids"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return op.UsageAnalyticsFilter{}, false
	}
	return op.UsageAnalyticsFilter{
		StartTime:       startTime,
		EndTime:         endTime,
		Timezone:        timezone,
		Scope:           scope,
		SiteIDs:         siteIDs,
		SiteAccountIDs:  accountIDs,
		ChannelIDs:      channelIDs,
		APIKeyIDs:       apiKeyIDs,
		RequestModels:   parseUsageStringList(c.Query("request_models")),
		ActualModels:    parseUsageStringList(c.Query("actual_models")),
		CanonicalModels: parseUsageStringList(c.Query("canonical_models")),
	}, true
}

func parseOptionalInt64(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

func parseUsageIntList(value string) ([]int, error) {
	items := parseUsageStringList(value)
	result := make([]int, 0, len(items))
	for _, item := range items {
		id, err := strconv.Atoi(item)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid id list")
		}
		result = append(result, id)
	}
	return result, nil
}

func parseUsageStringList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func validUsageDimension(value string) bool {
	switch value {
	case "site", "site_account", "channel", "api_key", "request_model", "actual_model", "canonical_model":
		return true
	default:
		return false
	}
}

func validUsageSort(value string) bool {
	switch value {
	case "request_count", "attempt_count", "total_tokens", "cost", "success_rate", "duration":
		return true
	default:
		return false
	}
}
