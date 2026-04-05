package relay

import (
	"context"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/conf"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/gin-gonic/gin"
)

// maxSSEEventSize 定义 SSE 事件的最大大小。
// 对于图像生成模型（如 gemini-3-pro-image-preview），返回的 base64 编码图像数据
// 可能非常大（高分辨率图像可能超过 10MB），因此需要设置足够大的缓冲区。
// 默认 32MB，可通过环境变量 OCTOPUS_RELAY_MAX_SSE_EVENT_SIZE 覆盖。
var maxSSEEventSize = 32 * 1024 * 1024

func init() {
	if raw := strings.TrimSpace(os.Getenv(strings.ToUpper(conf.APP_NAME) + "_RELAY_MAX_SSE_EVENT_SIZE")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			maxSSEEventSize = v
		}
	}
}

// hopByHopHeaders 定义不应转发的 HTTP 头
var hopByHopHeaders = map[string]bool{
	"authorization":       true,
	"x-api-key":           true,
	"connection":          true,
	"keep-alive":          true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"te":                  true,
	"trailer":             true,
	"transfer-encoding":   true,
	"upgrade":             true,
	"content-length":      true,
	"host":                true,
	"accept-encoding":     true,
	"x-forwarded-for":     true,
	"x-forwarded-host":    true,
	"x-forwarded-proto":   true,
	"x-forwarded-port":    true,
	"x-real-ip":           true,
	"forwarded":           true,
	"cf-connecting-ip":    true,
	"true-client-ip":      true,
	"x-client-ip":         true,
	"x-cluster-client-ip": true,
}

// StreamWriter abstracts writing responses to the client (HTTP SSE or WebSocket).
type StreamWriter interface {
	Write(data []byte) (int, error)
	Flush()
	Written() bool
	Header() http.Header
	WriteHeader(code int)
}

// UpstreamReader abstracts reading events from upstream (SSE or WebSocket).
type UpstreamReader interface {
	// ReadEvent reads the next event data. Returns io.EOF at end of stream.
	ReadEvent(ctx context.Context) ([]byte, error)
	// StatusCode returns the HTTP status code (for error handling).
	StatusCode() int
	// Headers returns the response headers.
	Headers() http.Header
	// Body returns the raw response body for non-stream scenarios.
	Body() io.ReadCloser
	Close() error
}

type relayRequest struct {
	c               *gin.Context
	ctx             context.Context // used when c is nil (WebSocket mode)
	inAdapter       model.Inbound
	internalRequest *model.InternalLLMRequest
	metrics         *RelayMetrics
	apiKeyID        int
	requestModel    string
	iter            *balancer.Iterator

	// streamWriter allows overriding the response writer (nil = use c.Writer)
	streamWriter StreamWriter
}

// requestContext returns the request context from gin or the standalone context.
func (r *relayRequest) requestContext() context.Context {
	if r.c != nil {
		return r.c.Request.Context()
	}
	return r.ctx
}

// relayAttempt 尝试级上下文
type relayAttempt struct {
	*relayRequest // 嵌入请求级上下文

	outAdapter           model.Outbound
	channel              *dbmodel.Channel
	usedKey              dbmodel.ChannelKey
	firstTokenTimeOutSec int
	retryAfter           time.Duration // forward() 提取后暂存
}

// attemptResult 封装单次尝试的结果
type attemptResult struct {
	Success    bool          // 是否成功
	Written    bool          // 流式响应是否已开始写入（不可重试）
	Canceled   bool          // 是否由下游请求取消或超时触发
	Err        error         // 失败时的错误
	StatusCode int           // 上游 HTTP 状态码（0 = 连接错误）
	RetryAfter time.Duration // 解析的 Retry-After 值
}
