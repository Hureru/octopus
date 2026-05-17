package relay

import (
	"strings"
	"sync"
	"time"
)

const wsResponseConnAffinityTTL = time.Hour

type wsResponseConnBinding struct {
	connID    string
	expiresAt time.Time
}

var wsResponseConnState = struct {
	mu       sync.RWMutex
	bindings map[string]wsResponseConnBinding
}{bindings: make(map[string]wsResponseConnBinding)}

func bindWSResponseConn(responseID, connID string, ttl time.Duration) {
	responseID = strings.TrimSpace(responseID)
	connID = strings.TrimSpace(connID)
	if responseID == "" || connID == "" {
		return
	}
	if ttl <= 0 {
		ttl = wsResponseConnAffinityTTL
	}
	wsResponseConnState.mu.Lock()
	wsResponseConnState.bindings[responseID] = wsResponseConnBinding{connID: connID, expiresAt: time.Now().Add(ttl)}
	wsResponseConnState.mu.Unlock()
}

func getWSResponseConn(responseID string) (string, bool) {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return "", false
	}
	now := time.Now()
	wsResponseConnState.mu.RLock()
	binding, ok := wsResponseConnState.bindings[responseID]
	wsResponseConnState.mu.RUnlock()
	if !ok || strings.TrimSpace(binding.connID) == "" {
		return "", false
	}
	if !binding.expiresAt.IsZero() && now.After(binding.expiresAt) {
		wsResponseConnState.mu.Lock()
		delete(wsResponseConnState.bindings, responseID)
		wsResponseConnState.mu.Unlock()
		return "", false
	}
	return binding.connID, true
}

func deleteWSResponseConn(responseID string) {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return
	}
	wsResponseConnState.mu.Lock()
	delete(wsResponseConnState.bindings, responseID)
	wsResponseConnState.mu.Unlock()
}

func resetWSResponseConnStateForTest() {
	wsResponseConnState.mu.Lock()
	wsResponseConnState.bindings = make(map[string]wsResponseConnBinding)
	wsResponseConnState.mu.Unlock()
}
