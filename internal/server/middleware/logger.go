package middleware

import (
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
)

type LoggerConfig struct {
	Enabled       bool
	SlowThreshold time.Duration
}

func Logger(cfg LoggerConfig) gin.HandlerFunc {
	if cfg.SlowThreshold <= 0 {
		cfg.SlowThreshold = 3 * time.Second
	}
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery
		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		shouldLog := cfg.Enabled || status >= 500 || latency >= cfg.SlowThreshold || len(c.Errors) > 0
		if !shouldLog {
			return
		}

		fields := []interface{}{
			"method", c.Request.Method,
			"path", path,
			"status", status,
			"latency", latency.String(),
			"latency_ms", latency.Milliseconds(),
			"ip", c.ClientIP(),
		}
		if query != "" && log.IsDebugEnabled() {
			fields = append(fields, "query", query)
		}
		if len(c.Errors) > 0 {
			fields = append(fields, "error", c.Errors.String())
		}

		switch {
		case status >= 500 || len(c.Errors) > 0:
			log.Warnw("http.request", fields...)
		case latency >= cfg.SlowThreshold:
			log.Warnw("http.slow", fields...)
		default:
			if cfg.Enabled {
				log.Infow("http.request", fields...)
			} else {
				log.Debugw("http.request", fields...)
			}
		}
	}
}
