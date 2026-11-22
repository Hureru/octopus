package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/coder/websocket"
)

const (
	wsUpstreamDialTimeout = 15 * time.Second
	wsUnsupportedRecheck  = 30 * time.Minute // 握手明确说不支持的渠道，过这么久再试
	wsHealthBackoffBase   = 1 * time.Minute  // 首次失败退避
	wsHealthBackoffMax    = 5 * time.Minute  // 退避上限
)

// wsUpstreamIdleTimeout：上游这么久不发任何东西就算超时，和 Codex 等回复的时限一样。是变量，测试里要改短。
var wsUpstreamIdleTimeout = 300 * time.Second

// wsUpstream 是一条客户端连接独占的上游 WS，客户端连接关闭时一起关掉。
// 一条客户端连接上同一时刻只有一个请求，所以不用加锁。
// shortcut: 两个回合之间不读这条连接，所以上游的 ping 得不到回应，也发现不了连接已经断了。
// 上游关掉空闲连接时，下一个回合会换新连接并发完整请求；连接被中间设备悄悄丢掉时，要等到首字超时才发现，然后同样换新连接重发。
// 原生续传因此老是用不上、或者回合因此卡住时，再加空闲时的读循环。
type wsUpstream struct {
	conn           *websocket.Conn // nil：还没有连接
	channelID      int
	keyID          int
	lastResponseID string // 这条连接上刚完成的回复：只有接着它的请求才能发原生续传
}

func (u *wsUpstream) close() {
	if u.conn != nil {
		// 不等上游回关闭帧：走到这里时不是连接坏了，就是客户端已经走了
		_ = u.conn.CloseNow()
	}
	*u = wsUpstream{}
}

// wsUpstreamHealth 记每个渠道的上游 WS 现在值不值得试，免得每个请求都去撞一次握手。
// shortcut: 按渠道 ID 记，改了渠道的地址也要等到期才重试；条目数不超过渠道数，所以不清理。
var wsUpstreamHealth = struct {
	sync.Mutex
	channels map[int]wsUpstreamChannelHealth
}{channels: map[int]wsUpstreamChannelHealth{}}

type wsUpstreamChannelHealth struct {
	failures  int // 连续失败次数
	skipUntil time.Time
}

func wsUpstreamAvailable(channelID int) bool {
	wsUpstreamHealth.Lock()
	defer wsUpstreamHealth.Unlock()
	return !time.Now().Before(wsUpstreamHealth.channels[channelID].skipUntil)
}

// recordWSUpstreamFailure 让这个渠道的上游 WS 歇一会儿，这期间走 HTTP。
// unsupported 表示上游明确不支持 WS；其他失败按连续次数退避。
func recordWSUpstreamFailure(channelID int, unsupported bool) {
	wsUpstreamHealth.Lock()
	defer wsUpstreamHealth.Unlock()
	health := wsUpstreamHealth.channels[channelID]
	backoff := wsUnsupportedRecheck
	if !unsupported {
		health.failures++
		backoff = wsFailureBackoff(health.failures)
	}
	health.skipUntil = time.Now().Add(backoff)
	wsUpstreamHealth.channels[channelID] = health
	log.Debugf("upstream ws for channel %d paused for %s (unsupported=%t)", channelID, backoff, unsupported)
}

func recordWSUpstreamSuccess(channelID int) {
	wsUpstreamHealth.Lock()
	defer wsUpstreamHealth.Unlock()
	delete(wsUpstreamHealth.channels, channelID)
}

func resetWSUpstreamHealth() {
	wsUpstreamHealth.Lock()
	defer wsUpstreamHealth.Unlock()
	clear(wsUpstreamHealth.channels)
}

// wsFailureBackoff returns the backoff duration based on consecutive failure count.
func wsFailureBackoff(failures int) time.Duration {
	switch {
	case failures <= 1:
		return wsHealthBackoffBase // 1min
	case failures == 2:
		return 2 * wsHealthBackoffBase // 2min
	default:
		return wsHealthBackoffMax // 5min cap
	}
}

// dialWSUpstream 给渠道建一条上游 WS。unsupported 为真表示上游明确不支持 WS。
func dialWSUpstream(ctx context.Context, channel *dbmodel.Channel, baseUrl string, headers http.Header) (conn *websocket.Conn, unsupported bool, err error) {
	wsURL, err := buildWSURL(baseUrl)
	if err != nil {
		return nil, false, fmt.Errorf("invalid base url for ws: %w", err)
	}
	// 用渠道自己的 HTTP 客户端握手，代理设置才生效
	httpClient, err := helper.ChannelHTTPClientWithContext(ctx, channel)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get http client: %w", err)
	}
	dialCtx, cancel := context.WithTimeout(ctx, wsUpstreamDialTimeout)
	defer cancel()
	conn, response, err := websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{
		HTTPClient: cloneHTTPClientForWSDial(httpClient),
		HTTPHeader: headers,
	})
	if err != nil {
		return nil, shouldMarkWSUnsupported(response, err), err
	}
	// 单个事件可能很大（比如图片生成）
	conn.SetReadLimit(int64(maxSSEEventSize))
	return conn, false, nil
}

func buildUpstreamWSHeaders(clientHeaders http.Header, channel *dbmodel.Channel, key string) http.Header {
	headers := http.Header{}
	for name, values := range clientHeaders {
		if !shouldProxyUpstreamWSHeader(name) {
			continue
		}
		for _, value := range values {
			headers.Add(name, value)
		}
	}
	if values, ok := headers["User-Agent"]; !ok || len(values) == 0 {
		headers.Set("User-Agent", "")
	} else {
		headers["User-Agent"] = values[:1]
	}
	if channel != nil {
		clientHeader := clientHeaderLookup(headers)
		for _, header := range channel.CustomHeader {
			if strings.TrimSpace(header.HeaderKey) == "" {
				continue
			}
			if value, ok := header.ResolveValue(clientHeader); ok {
				headers.Set(header.HeaderKey, value)
			}
		}
	}
	headers.Set("Authorization", "Bearer "+key)
	headers.Set("OpenAI-Beta", "responses_websockets=2026-02-06")
	return headers
}

func shouldProxyUpstreamWSHeader(name string) bool {
	lowerName := strings.ToLower(strings.TrimSpace(name))
	if lowerName == "" {
		return false
	}
	if hopByHopHeaders[lowerName] {
		return false
	}
	if strings.HasPrefix(lowerName, "sec-websocket-") {
		return false
	}
	return true
}

func cloneHTTPClientForWSDial(httpClient *http.Client) *http.Client {
	if httpClient == nil {
		return nil
	}
	clonedClient := *httpClient
	if transport, ok := httpClient.Transport.(*http.Transport); ok && transport != nil {
		clonedTransport := transport.Clone()
		clonedTransport.DisableCompression = true
		clonedClient.Transport = clonedTransport
		return &clonedClient
	}
	if httpClient.Transport == nil {
		if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
			clonedTransport := defaultTransport.Clone()
			clonedTransport.DisableCompression = true
			clonedClient.Transport = clonedTransport
		}
	}
	return &clonedClient
}

func buildWSURL(baseUrl string) (string, error) {
	parsed, err := url.Parse(strings.TrimSuffix(baseUrl, "/"))
	if err != nil {
		return "", err
	}

	// Convert http(s) to ws(s)
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	case "wss", "ws":
		// already WS
	default:
		parsed.Scheme = "wss"
	}

	// shortcut: the dial reuses the prefix an HTTP request learned and never probes itself, so a
	// site-root channel's first dial fails and it is served over HTTP until the next dial is due;
	// add a dial fallback if such channels must open on WS at once.
	parsed.Path = parsed.Path + helper.LearnedAPIPrefix(baseUrl, "/responses") + "/responses"
	return parsed.String(), nil
}

func shouldMarkWSUnsupported(response *http.Response, err error) bool {
	statusCode := 0
	if response != nil {
		statusCode = response.StatusCode
	}
	switch statusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	}

	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "status code 404") ||
		strings.Contains(message, "status code 405") ||
		strings.Contains(message, "status code 501") ||
		strings.Contains(message, " got 404") ||
		strings.Contains(message, " got 405") ||
		strings.Contains(message, " got 501")
}

// forwardViaUpstreamWS 在这条客户端连接独占的上游 WS 上跑这个请求。
// handled 为假表示这个渠道现在用不了 WS，调用方在同一次尝试里改走 HTTP。
func (ra *relayAttempt) forwardViaUpstreamWS(ctx context.Context) (statusCode int, err error, handled bool) {
	u := ra.upstream
	if u.conn != nil && u.channelID == ra.channel.ID && u.keyID == ra.usedKey.ID {
		statusCode, err, stale := ra.upstreamWSTurn(ctx, true)
		if !stale {
			return statusCode, err, true
		}
		// 复用的连接在第一个事件之前就不行了（空闲时被上游关掉、上游不认这个续传……），客户端还什么都没收到：
		// 换一条新连接，用完整请求重试一次。除此之外同一个请求绝不重发。
		log.Debugf("upstream ws for channel %s failed before the first event, retrying on a new connection: %v", ra.channel.Name, err)
		ra.metrics.SetWSRecovery(dbmodel.RelayLogWSRecoveryReconnect)
	}
	u.close()
	if !ra.dialUpstreamWS(ctx) {
		return 0, nil, false
	}
	statusCode, err, _ = ra.upstreamWSTurn(ctx, false)
	return statusCode, err, true
}

// dialUpstreamWS 给这条客户端连接建上游 WS。这个渠道现在用不了 WS 时返回 false。
func (ra *relayAttempt) dialUpstreamWS(ctx context.Context) bool {
	if !wsUpstreamAvailable(ra.channel.ID) {
		return false
	}
	headers := buildUpstreamWSHeaders(ra.clientRequestHeaders(), ra.channel, ra.usedKey.ChannelKey)
	conn, unsupported, err := dialWSUpstream(ctx, ra.channel, ra.channel.GetBaseUrl(), headers)
	if err != nil {
		// 客户端自己走了造成的失败不算渠道的
		if ctx.Err() == nil {
			log.Debugf("upstream ws dial failed for channel %s, using HTTP: %v", ra.channel.Name, err)
			recordWSUpstreamFailure(ra.channel.ID, unsupported)
			ra.metrics.SetWSRecovery(dbmodel.RelayLogWSRecoveryDowngrade)
		}
		return false
	}
	*ra.upstream = wsUpstream{conn: conn, channelID: ra.channel.ID, keyID: ra.usedKey.ID}
	return true
}

// upstreamWSTurn 在上游 WS 上跑一个回合：发请求，把事件原样转给客户端，读到结束事件为止。
// 回合没有正常完成时关掉这条连接。
// stale 为真表示复用的连接在第一个事件之前就失败了（断了，或者一直没有回音），换一条新连接可能就好。
func (ra *relayAttempt) upstreamWSTurn(ctx context.Context, reused bool) (statusCode int, err error, stale bool) {
	u := ra.upstream
	// 原生续传（客户端的原始请求）：只有这条连接刚完成的回复正是请求要接的那个，上游才接得上。
	// 其余情况发自包含的完整请求。
	body, mode := ra.rawBody, dbmodel.RelayLogWSModeFresh
	switch {
	case reused && u.lastResponseID != "" && u.lastResponseID == ra.previousResponseID:
		body, mode = ra.continuationBody, dbmodel.RelayLogWSModeContinuation
	case ra.internalRequest.IsOpenAIExactReplayRequest():
		mode = dbmodel.RelayLogWSModeReplay
	case ra.previousResponseID != "":
		mode = dbmodel.RelayLogWSModeContinuation // 本地没有存档，原样转发的续传
	}
	payload, err := ra.buildUpstreamWSPayload(body)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err), false
	}
	ra.metrics.SetTransportRequestPayload(payload, ra.internalRequest.Model)

	writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	err = u.conn.Write(writeCtx, websocket.MessageText, payload)
	cancel()
	if err != nil {
		return ra.upstreamWSBroken(ctx, reused, fmt.Errorf("upstream ws send failed: %v", err))
	}

	writer := ra.streamWriter.(*WSStreamWriter)
	var collector responsesOutputCollector
	first := true
	for {
		timeout := wsUpstreamIdleTimeout
		firstTokenTimeout := time.Duration(ra.firstTokenTimeOutSec) * time.Second
		awaitingFirstToken := first && firstTokenTimeout > 0 && firstTokenTimeout < timeout
		if awaitingFirstToken {
			timeout = firstTokenTimeout
		}
		readCtx, cancel := context.WithTimeout(ctx, timeout)
		msgType, data, err := u.conn.Read(readCtx)
		timedOut := readCtx.Err() != nil
		cancel()
		if err != nil {
			// 错误都不带原来的 context 错误：那是上游的问题，不能被当成客户端取消
			switch {
			case ctx.Err() != nil || !timedOut:
				// 结束事件之前连接断了：不管关闭码是什么都算失败
				return ra.upstreamWSBroken(ctx, reused && first, fmt.Errorf("upstream ws closed before the terminal event: %v", err))
			case awaitingFirstToken:
				// 复用的连接什么都等不到，多半是空闲时被中间设备悄悄丢掉了（请求发得出去，没有回音）：
				// 和读失败一样，换一条新连接再试一次
				u.close()
				return 0, ra.firstTokenTimeoutError(), reused
			default:
				return ra.upstreamWSBroken(ctx, reused && first, fmt.Errorf("upstream ws sent nothing for %s", timeout))
			}
		}
		if msgType != websocket.MessageText {
			continue
		}

		event := parseWSUpstreamEvent(data)
		if first && event.Error != nil {
			// 客户端还什么都没收到：不转发这个错误，交给重试和换渠道。连接上的状态说不准了，不再用它
			u.close()
			statusCode, err := wsUpstreamFailure(event)
			return statusCode, err, reused
		}
		if first {
			first = false
			ra.metrics.SetFirstTokenTime(time.Now())
			ra.metrics.UsedWS = true
			ra.metrics.SetWSMode(mode)
			ra.metrics.SetWSExecMode(dbmodel.RelayLogWSExecModePassthrough)
		}
		collector.observe(data)
		ra.collectPassthroughEvent(ctx, data)
		if err := writer.WriteFrame(ra.rewriteWSPassthroughDownstreamModel(data)); err != nil {
			// 客户端收不到了，等同于它取消了请求：不换渠道，也不算这个渠道的失败
			u.close()
			return 0, fmt.Errorf("write to the client failed: %v: %w", err, context.Canceled), false
		}
		ra.streamPayloadWritten.Store(true)

		if _, terminal := responsesPassthroughTerminalEvents[event.Type]; !terminal {
			continue
		}
		ra.rawOutputItems = collector.items()
		if event.Type == "response.completed" || event.Type == "response.incomplete" {
			u.lastResponseID = event.ResponseID
			recordWSUpstreamSuccess(ra.channel.ID)
		} else {
			// 上游自己报告了失败，客户端已经收到（attempt 会把这一次记成失败）。连接上的状态说不准了，不再用它
			u.close()
		}
		return http.StatusOK, nil, false
	}
}

// wsUpstreamFailure 把上游在事件里报告的失败变成这次尝试的状态码和错误。
func wsUpstreamFailure(event wsUpstreamEvent) (int, error) {
	if event.Error == nil {
		return http.StatusBadGateway, fmt.Errorf("upstream reported %s", event.Type)
	}
	status := event.Error.Status
	if status == 0 {
		status = http.StatusBadGateway
	}
	return normalizeUpstreamStatusCode(status, event.Error.Error()), event.Error
}

// upstreamWSBroken 处理上游连接本身的失败：关掉它。
// stale 表示它是复用的、而且还没收到这个回合的第一个事件——多半只是这条连接放坏了，不算渠道的失败。
func (ra *relayAttempt) upstreamWSBroken(ctx context.Context, stale bool, err error) (int, error, bool) {
	ra.upstream.close()
	if ctx.Err() != nil {
		// 客户端走了，或者它的连接到了时限
		return 0, ctx.Err(), false
	}
	if !stale {
		recordWSUpstreamFailure(ra.channel.ID, false)
	}
	return 0, err, stale
}

// buildUpstreamWSPayload 把请求体变成上游 WS 的 response.create 消息：
// 只改 model 和 WS 协议要求的几个字段，其余原样。渠道的参数覆盖最后应用，和走 HTTP 时一样。
func (ra *relayAttempt) buildUpstreamWSPayload(body []byte) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	payload["model"], _ = json.Marshal(ra.internalRequest.Model)
	payload["type"] = json.RawMessage(`"response.create"`)
	payload["stream"] = json.RawMessage(`true`)
	delete(payload, "background") // WS 上没有后台模式
	message, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return helper.MergeParamOverride(message, ra.channel.ParamOverride)
}

// wsUpstreamEvent 是转发上游 WS 事件时要看的几样东西。
type wsUpstreamEvent struct {
	Type       string
	ResponseID string
	Error      *wsUpstreamEventError // error 事件，或者 response.failed 里带的 error
}

type wsUpstreamEventError struct {
	Status  int
	Code    string
	Type    string
	Message string
}

func (e *wsUpstreamEventError) Error() string {
	if e == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if e.Code != "" {
		parts = append(parts, "code="+e.Code)
	}
	if e.Type != "" {
		parts = append(parts, "type="+e.Type)
	}
	if e.Status > 0 {
		parts = append(parts, fmt.Sprintf("status=%d", e.Status))
	}
	if len(parts) == 0 {
		return "upstream ws error"
	}
	return strings.Join(parts, ", ")
}

func parseWSUpstreamEvent(data []byte) wsUpstreamEvent {
	type errorBody struct {
		Code    any    `json:"code"`
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	var raw struct {
		Type     string     `json:"type"`
		Status   int        `json:"status"`
		Error    *errorBody `json:"error"`
		Response struct {
			ID    string     `json:"id"`
			Error *errorBody `json:"error"`
		} `json:"response"`
	}
	// 解析不了的字段留空，照样转发
	_ = json.Unmarshal(data, &raw)
	event := wsUpstreamEvent{Type: raw.Type, ResponseID: raw.Response.ID}
	switch {
	case raw.Error != nil:
		event.Error = &wsUpstreamEventError{Status: raw.Status, Code: normalizeWSUpstreamErrorCode(raw.Error.Code), Type: raw.Error.Type, Message: raw.Error.Message}
	case raw.Response.Error != nil:
		// response.failed 不带状态码
		event.Error = &wsUpstreamEventError{Status: http.StatusBadGateway, Code: normalizeWSUpstreamErrorCode(raw.Response.Error.Code), Type: raw.Response.Error.Type, Message: raw.Response.Error.Message}
	}
	return event
}

func normalizeWSUpstreamErrorCode(code any) string {
	switch v := code.(type) {
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

// rewriteWSPassthroughDownstreamModel 把上游事件里的模型名（response.model）换回客户端用的那个。
// 别的值都不解析，所以调用方自己的数据（比如 metadata 里的 model）不会被改，数字也不会丢精度。
func (ra *relayAttempt) rewriteWSPassthroughDownstreamModel(data []byte) []byte {
	if ra == nil || ra.internalRequest == nil || strings.TrimSpace(ra.requestModel) == "" || strings.TrimSpace(ra.internalRequest.Model) == strings.TrimSpace(ra.requestModel) {
		return data
	}
	var event, response map[string]json.RawMessage
	var model string
	if json.Unmarshal(data, &event) != nil || json.Unmarshal(event["response"], &response) != nil ||
		json.Unmarshal(response["model"], &model) != nil || model != ra.internalRequest.Model {
		return data
	}
	response["model"], _ = json.Marshal(ra.requestModel)
	event["response"], _ = json.Marshal(response)
	if rewritten, err := json.Marshal(event); err == nil {
		return rewritten
	}
	return data
}
