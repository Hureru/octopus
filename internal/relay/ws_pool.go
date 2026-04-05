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
	wsConnMaxAge       = 55 * time.Minute // slightly less than 60-min limit
	wsConnIdleTimeout  = 5 * time.Minute
	wsPoolCleanupEvery = 1 * time.Minute
)

// wsUpstreamPool manages persistent WebSocket connections to upstream providers.
var wsUpstreamPool = newWSPool()

type wsPoolKey struct {
	channelID int
	keyID     int
}

type pooledConn struct {
	conn       *websocket.Conn
	createdAt  time.Time
	lastUsed   time.Time
	busy       bool
	lastRespID string // for previous_response_id chaining
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
func (p *wsPool) Get(channelID, keyID int) *pooledConn {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := wsPoolKey{channelID, keyID}
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
func (p *wsPool) Put(channelID, keyID int, pc *pooledConn) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := wsPoolKey{channelID, keyID}
	pc.busy = false
	pc.lastUsed = time.Now()
	p.conns[key] = pc
}

// Remove removes and closes a connection.
func (p *wsPool) Remove(channelID, keyID int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := wsPoolKey{channelID, keyID}
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
func (p *wsPool) Dial(ctx context.Context, channel *dbmodel.Channel, baseUrl, key string) (*pooledConn, error) {
	// Build WS URL
	wsURL, err := buildWSURL(baseUrl)
	if err != nil {
		return nil, fmt.Errorf("invalid base url for ws: %w", err)
	}

	// Get HTTP client for proxy settings
	httpClient, err := helper.ChannelHttpClient(channel)
	if err != nil {
		return nil, fmt.Errorf("failed to get http client: %w", err)
	}

	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	opts := &websocket.DialOptions{
		HTTPClient: httpClient,
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer " + key},
		},
	}

	conn, _, err := websocket.Dial(dialCtx, wsURL, opts)
	if err != nil {
		return nil, err
	}

	// Set read limit high for large responses (e.g., image generation)
	conn.SetReadLimit(int64(maxSSEEventSize))

	pc := &pooledConn{
		conn:      conn,
		createdAt: time.Now(),
		lastUsed:  time.Now(),
		busy:      true,
	}

	return pc, nil
}

// SendResponseCreate sends a response.create message on a WS connection.
func (p *wsPool) SendResponseCreate(ctx context.Context, pc *pooledConn, requestBody json.RawMessage) error {
	// Merge type field into the request body
	var bodyMap map[string]json.RawMessage
	if err := json.Unmarshal(requestBody, &bodyMap); err != nil {
		return fmt.Errorf("failed to parse request body: %w", err)
	}
	bodyMap["type"] = json.RawMessage(`"response.create"`)

	// Remove stream and background fields (not used in WS mode)
	delete(bodyMap, "stream")
	delete(bodyMap, "background")

	// Add previous_response_id if available
	if pc.lastRespID != "" {
		if _, exists := bodyMap["previous_response_id"]; !exists {
			bodyMap["previous_response_id"] = json.RawMessage(fmt.Sprintf("%q", pc.lastRespID))
		}
	}

	merged, err := json.Marshal(bodyMap)
	if err != nil {
		return fmt.Errorf("failed to marshal ws message: %w", err)
	}

	return pc.conn.Write(ctx, websocket.MessageText, merged)
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
func TryUpstreamWS(ctx context.Context, channel *dbmodel.Channel, baseUrl, key string, keyID int) *pooledConn {
	if wsUpstreamPool.IsUnsupported(channel.ID) {
		return nil
	}

	// Try existing connection first
	if pc := wsUpstreamPool.Get(channel.ID, keyID); pc != nil {
		return pc
	}

	// Try to dial new connection
	pc, err := wsUpstreamPool.Dial(ctx, channel, baseUrl, key)
	if err != nil {
		log.Infof("upstream WS dial failed for channel %d, marking unsupported: %v", channel.ID, err)
		wsUpstreamPool.MarkUnsupported(channel.ID)
		return nil
	}

	return pc
}

// CloseUpstreamWSPool gracefully shuts down the upstream WS pool.
func CloseUpstreamWSPool() {
	wsUpstreamPool.Close()
}
