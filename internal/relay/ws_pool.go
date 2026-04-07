package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/coder/websocket"
)

const (
	wsConnMaxAge       = 55 * time.Minute // slightly less than 60-min limit
	wsConnIdleTimeout  = 5 * time.Minute
	wsPoolCleanupEvery = 1 * time.Minute
)

// wsUpstreamPool manages persistent WebSocket connections to upstream providers.
var wsUpstreamPool = newWSPool()

type wsPoolKey struct {
	channelID int
	keyID     int
	headerSig string
}

type pooledConn struct {
	conn      *websocket.Conn
	createdAt time.Time
	lastUsed  time.Time
	busy      bool
	poolKey   wsPoolKey
}

type wsPool struct {
	mu    sync.Mutex
	conns map[wsPoolKey]*pooledConn

	// Track channels that don't support WS to avoid repeated attempts
	unsupported   map[int]time.Time
	unsupportedMu sync.RWMutex

	stopCh chan struct{}
	once   sync.Once
}

func newWSPool() *wsPool {
	p := &wsPool{
		conns:       make(map[wsPoolKey]*pooledConn),
		unsupported: make(map[int]time.Time),
		stopCh:      make(chan struct{}),
	}
	go p.cleanupLoop()
	return p
}

// Get returns an existing idle connection or nil.
func (p *wsPool) Get(key wsPoolKey) *pooledConn {
	p.mu.Lock()
	defer p.mu.Unlock()

	pc, ok := p.conns[key]
	if !ok || pc.busy {
		return nil
	}

	// Check expiration
	if time.Since(pc.createdAt) > wsConnMaxAge {
		pc.conn.Close(websocket.StatusGoingAway, "connection expired")
		delete(p.conns, key)
		return nil
	}

	pc.busy = true
	pc.lastUsed = time.Now()
	return pc
}

// Put returns a connection to the pool after use.
func (p *wsPool) Put(pc *pooledConn) {
	if pc == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	pc.busy = false
	pc.lastUsed = time.Now()
	p.conns[pc.poolKey] = pc
}

// Remove removes and closes a connection.
func (p *wsPool) Remove(key wsPoolKey) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if pc, ok := p.conns[key]; ok {
		pc.conn.Close(websocket.StatusNormalClosure, "")
		delete(p.conns, key)
	}
}

// IsUnsupported checks if a channel is known to not support WS.
func (p *wsPool) IsUnsupported(channelID int) bool {
	p.unsupportedMu.RLock()
	defer p.unsupportedMu.RUnlock()

	t, ok := p.unsupported[channelID]
	if !ok {
		return false
	}
	// Re-check every 30 minutes
	return time.Since(t) < 30*time.Minute
}

// MarkUnsupported marks a channel as not supporting WS.
func (p *wsPool) MarkUnsupported(channelID int) {
	p.unsupportedMu.Lock()
	defer p.unsupportedMu.Unlock()
	p.unsupported[channelID] = time.Now()
}

// Dial creates a new WebSocket connection to the upstream.
func (p *wsPool) Dial(ctx context.Context, key wsPoolKey, channel *dbmodel.Channel, baseUrl string, headers http.Header) (*pooledConn, bool, error) {
	// Build WS URL
	wsURL, err := buildWSURL(baseUrl)
	if err != nil {
		return nil, false, fmt.Errorf("invalid base url for ws: %w", err)
	}

	// Get HTTP client for proxy settings
	httpClient, err := helper.ChannelHttpClient(channel)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get http client: %w", err)
	}
	httpClient = cloneHTTPClientForWSDial(httpClient)

	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	opts := &websocket.DialOptions{
		HTTPClient: httpClient,
		HTTPHeader: headers,
	}

	conn, response, err := websocket.Dial(dialCtx, wsURL, opts)
	if err != nil {
		return nil, shouldMarkWSUnsupported(response, err), err
	}

	// Set read limit high for large responses (e.g., image generation)
	conn.SetReadLimit(int64(maxSSEEventSize))

	pc := &pooledConn{
		conn:      conn,
		createdAt: time.Now(),
		lastUsed:  time.Now(),
		busy:      true,
		poolKey:   key,
	}

	return pc, false, nil
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
		for _, header := range channel.CustomHeader {
			if strings.TrimSpace(header.HeaderKey) == "" {
				continue
			}
			headers.Set(header.HeaderKey, header.HeaderValue)
		}
	}
	headers.Set("Authorization", "Bearer "+key)
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

func newWSPoolKey(channelID, keyID int, headers http.Header) wsPoolKey {
	return wsPoolKey{channelID: channelID, keyID: keyID, headerSig: wsHeaderSignature(headers)}
}

func wsHeaderSignature(headers http.Header) string {
	if len(headers) == 0 {
		return ""
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, strings.ToLower(key))
	}
	sort.Strings(keys)

	var builder strings.Builder
	for _, key := range keys {
		values := append([]string(nil), headers.Values(key)...)
		sort.Strings(values)
		builder.WriteString(key)
		builder.WriteByte('=')
		for i, value := range values {
			if i > 0 {
				builder.WriteByte(',')
			}
			builder.WriteString(value)
		}
		builder.WriteByte('\n')
	}
	return builder.String()
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

// SendResponseCreate sends a response.create message on a WS connection.
func (p *wsPool) SendResponseCreate(ctx context.Context, pc *pooledConn, requestBody json.RawMessage) error {
	merged, err := buildWSResponseCreateMessage(requestBody)
	if err != nil {
		return err
	}

	return pc.conn.Write(ctx, websocket.MessageText, merged)
}

func buildWSResponseCreateMessage(requestBody json.RawMessage) ([]byte, error) {
	// Merge type field into the request body
	var bodyMap map[string]json.RawMessage
	if err := json.Unmarshal(requestBody, &bodyMap); err != nil {
		return nil, fmt.Errorf("failed to parse request body: %w", err)
	}
	bodyMap["type"] = json.RawMessage(`"response.create"`)

	// Remove stream and background fields (not used in WS mode)
	delete(bodyMap, "stream")
	delete(bodyMap, "background")

	merged, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ws message: %w", err)
	}

	return merged, nil
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

	parsed.Path = parsed.Path + "/responses"
	return parsed.String(), nil
}

func shouldMarkWSUnsupported(response *http.Response, err error) bool {
	statusCode := 0
	if response != nil {
		statusCode = response.StatusCode
	}
	switch statusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUpgradeRequired, http.StatusNotImplemented:
		return true
	}

	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "status code 404") ||
		strings.Contains(message, "status code 405") ||
		strings.Contains(message, "status code 426") ||
		strings.Contains(message, "status code 501") ||
		strings.Contains(message, " got 404") ||
		strings.Contains(message, " got 405") ||
		strings.Contains(message, " got 426") ||
		strings.Contains(message, " got 501")
}

func (p *wsPool) cleanupLoop() {
	ticker := time.NewTicker(wsPoolCleanupEvery)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.cleanup()
		}
	}
}

func (p *wsPool) cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for key, pc := range p.conns {
		if pc.busy {
			continue
		}
		if now.Sub(pc.createdAt) > wsConnMaxAge || now.Sub(pc.lastUsed) > wsConnIdleTimeout {
			pc.conn.Close(websocket.StatusGoingAway, "cleanup")
			delete(p.conns, key)
		}
	}

	// Clean up old unsupported entries
	p.unsupportedMu.Lock()
	for id, t := range p.unsupported {
		if now.Sub(t) > 30*time.Minute {
			delete(p.unsupported, id)
		}
	}
	p.unsupportedMu.Unlock()
}

// Close shuts down the pool and all connections.
func (p *wsPool) Close() {
	p.once.Do(func() {
		close(p.stopCh)

		p.mu.Lock()
		defer p.mu.Unlock()

		for key, pc := range p.conns {
			pc.conn.Close(websocket.StatusGoingAway, "shutdown")
			delete(p.conns, key)
		}
	})
}

// TryUpstreamWS attempts to get or create a WS connection for an upstream channel.
// Returns nil if the channel doesn't support WS or connection fails.
func TryUpstreamWS(ctx context.Context, channel *dbmodel.Channel, baseUrl, key string, keyID int, clientHeaders http.Header, forceRedial ...bool) *pooledConn {
	if wsUpstreamPool.IsUnsupported(channel.ID) {
		return nil
	}

	headers := buildUpstreamWSHeaders(clientHeaders, channel, key)
	poolKey := newWSPoolKey(channel.ID, keyID, headers)
	redial := len(forceRedial) > 0 && forceRedial[0]

	// Try existing connection first
	if !redial {
		if pc := wsUpstreamPool.Get(poolKey); pc != nil {
			return pc
		}
	} else {
		wsUpstreamPool.Remove(poolKey)
	}

	// Try to dial new connection
	pc, unsupported, err := wsUpstreamPool.Dial(ctx, poolKey, channel, baseUrl, headers)
	if err != nil {
		if unsupported {
			log.Infof("upstream WS dial failed for channel %d, marking unsupported: %v", channel.ID, err)
			wsUpstreamPool.MarkUnsupported(channel.ID)
		} else {
			log.Infof("upstream WS dial failed for channel %d: %v", channel.ID, err)
		}
		return nil
	}

	return pc
}

// CloseUpstreamWSPool gracefully shuts down the upstream WS pool.
func CloseUpstreamWSPool() {
	wsUpstreamPool.Close()
}
