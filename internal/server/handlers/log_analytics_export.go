package handlers

import (
	"encoding/csv"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
)

var usageAnalyticsMetricCSVColumns = []string{
	"metric_count",
	"request_count",
	"attempt_count",
	"success_count",
	"failed_count",
	"canceled_count",
	"indeterminate_count",
	"success_rate",
	"input_tokens",
	"output_tokens",
	"cache_read_tokens",
	"cache_write_tokens",
	"total_tokens",
	"cost_usd",
	"average_duration_ms",
	"p95_duration_ms",
	"average_ftut_ms",
	"p95_ftut_ms",
	"ftut_samples",
}

func getUsageAnalyticsExport(c *gin.Context) {
	filter, ok := parseUsageAnalyticsFilter(c)
	if !ok {
		return
	}
	options, ok := parseUsageBreakdownOptions(c)
	if !ok {
		return
	}
	firstPage, err := op.UsageAnalyticsBreakdownExportPageGet(
		c.Request.Context(),
		filter,
		options.dimension,
		options.search,
		1,
		options.sort,
		options.descending,
	)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	filename := "octopus-usage-" + time.Now().UTC().Format("20060102T150405Z") + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	writer := &countingResponseWriter{ResponseWriter: c.Writer}
	if err := writeUsageAnalyticsCSVPages(
		writer,
		firstPage,
		func(page int) (op.UsageBreakdown, error) {
			return op.UsageAnalyticsBreakdownExportPageGet(
				c.Request.Context(),
				filter,
				options.dimension,
				options.search,
				page,
				options.sort,
				options.descending,
			)
		},
	); err != nil {
		if writer.bytesWritten == 0 {
			c.Header("Content-Type", "application/json")
			c.Header("Content-Disposition", "")
			resp.Error(c, http.StatusInternalServerError, err.Error())
			return
		}
		log.Warnf("usage analytics CSV export failed mid-stream: %v", err)
	}
}

type usageAnalyticsCSVPageLoader func(page int) (op.UsageBreakdown, error)

func writeUsageAnalyticsCSVPages(
	output io.Writer,
	firstPage op.UsageBreakdown,
	loadPage usageAnalyticsCSVPageLoader,
) error {
	writer := csv.NewWriter(output)
	if err := writer.Write(usageAnalyticsCSVHeader()); err != nil {
		return err
	}
	page := firstPage
	written := int64(0)
	for {
		for _, item := range page.Items {
			if err := writer.Write(usageAnalyticsCSVRecord(page.Scope, page.Dimension, item)); err != nil {
				return err
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return err
		}
		written += int64(len(page.Items))
		if written >= firstPage.Total || len(page.Items) == 0 {
			return nil
		}
		if loadPage == nil {
			return io.ErrUnexpectedEOF
		}
		next, err := loadPage(page.Page + 1)
		if err != nil {
			return err
		}
		page = next
	}
}

func writeUsageAnalyticsCSV(
	output io.Writer,
	scope op.UsageMetricScope,
	dimension string,
	items []op.UsageBreakdownItem,
) error {
	writer := csv.NewWriter(output)
	if err := writer.Write(usageAnalyticsCSVHeader()); err != nil {
		return err
	}
	for _, item := range items {
		if err := writer.Write(usageAnalyticsCSVRecord(scope, dimension, item)); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func usageAnalyticsCSVHeader() []string {
	return append([]string{"metric_scope", "dimension", "id", "name"}, usageAnalyticsMetricCSVColumns...)
}

func usageAnalyticsCSVRecord(
	scope op.UsageMetricScope,
	dimension string,
	item op.UsageBreakdownItem,
) []string {
	metrics := item.UsageAnalyticsMetrics
	return []string{
		string(scope),
		dimension,
		strconv.Itoa(item.ID),
		sanitizeUsageCSVCell(item.Name),
		strconv.FormatInt(metrics.MetricCount, 10),
		strconv.FormatInt(metrics.RequestCount, 10),
		strconv.FormatInt(metrics.AttemptCount, 10),
		strconv.FormatInt(metrics.SuccessCount, 10),
		strconv.FormatInt(metrics.FailedCount, 10),
		strconv.FormatInt(metrics.CanceledCount, 10),
		strconv.FormatInt(metrics.IndeterminateCount, 10),
		strconv.FormatFloat(metrics.SuccessRate, 'g', -1, 64),
		strconv.FormatInt(metrics.InputTokens, 10),
		strconv.FormatInt(metrics.OutputTokens, 10),
		strconv.FormatInt(metrics.CacheReadTokens, 10),
		strconv.FormatInt(metrics.CacheWriteTokens, 10),
		strconv.FormatInt(metrics.TotalTokens, 10),
		strconv.FormatFloat(metrics.CostUSD, 'g', -1, 64),
		strconv.FormatFloat(metrics.AverageDurationMS, 'g', -1, 64),
		strconv.FormatInt(metrics.P95DurationMS, 10),
		strconv.FormatFloat(metrics.AverageFTUTMS, 'g', -1, 64),
		strconv.FormatInt(metrics.P95FTUTMS, 10),
		strconv.FormatInt(metrics.FTUTSamples, 10),
	}
}

func sanitizeUsageCSVCell(value string) string {
	trimmed := strings.TrimLeftFunc(value, unicode.IsSpace)
	if trimmed == "" {
		return value
	}
	first, _ := utf8.DecodeRuneInString(trimmed)
	if strings.ContainsRune("=+-@", first) {
		return "'" + value
	}
	return value
}
