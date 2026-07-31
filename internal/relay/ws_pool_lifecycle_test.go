package relay

import (
	"testing"
	"time"
)

func newTestWSPool() *wsPool {
	return &wsPool{
		conns:       make(map[wsPoolKey]*wsPoolEntry),
		inFlight:    make(map[wsPoolKey]int),
		unsupported: make(map[int]time.Time),
		health:      make(map[int]*wsChannelHealth),
		stopCh:      make(chan struct{}),
	}
}

func TestWSPoolCleanupRetiresBusyExpiredConnectionAfterUse(t *testing.T) {
	pool := newTestWSPool()
	key := wsPoolKey{channelID: 1, keyID: 2}
	pc := &pooledConn{
		id:        "busy-expired",
		createdAt: time.Now().Add(-wsConnMaxAge - time.Minute),
		lastUsed:  time.Now(),
		busy:      true,
		queue:     1,
		poolKey:   key,
	}
	pool.conns[key] = &wsPoolEntry{conns: []*pooledConn{pc}}

	pool.cleanup()

	if pc.closed {
		t.Fatal("cleanup closed a busy connection")
	}
	if !pc.retireAfterUse {
		t.Fatal("expected expired busy connection to be retired after use")
	}
	if pool.pooledConnCount(key) != 1 {
		t.Fatal("expected busy connection to remain managed until it is returned")
	}

	pool.Put(pc)
	if !pc.closed {
		t.Fatal("expected retired connection to close when returned")
	}
	if pool.pooledConnCount(key) != 0 {
		t.Fatal("expected retired connection to be removed from the pool")
	}
}

func TestWSPoolPutDoesNotReviveRemovedConnection(t *testing.T) {
	pool := newTestWSPool()
	key := wsPoolKey{channelID: 1, keyID: 2}
	pc := &pooledConn{
		id:        "removed",
		createdAt: time.Now(),
		lastUsed:  time.Now(),
		busy:      true,
		queue:     1,
		poolKey:   key,
	}

	pool.Put(pc)

	if !pc.closed {
		t.Fatal("expected an unmanaged connection to be discarded")
	}
	if pool.pooledConnCount(key) != 0 {
		t.Fatal("removed connection was revived in the pool")
	}
}

func TestWSUpstreamReaderCloseWithErrorRemovesPreviouslyReturnedConnection(t *testing.T) {
	pool := newTestWSPool()
	previousPool := wsUpstreamPool
	wsUpstreamPool = pool
	t.Cleanup(func() { wsUpstreamPool = previousPool })

	key := wsPoolKey{channelID: 1, keyID: 2}
	pc := &pooledConn{
		id:        "stream-error",
		createdAt: time.Now(),
		lastUsed:  time.Now(),
		busy:      true,
		queue:     1,
		poolKey:   key,
	}
	pool.conns[key] = &wsPoolEntry{conns: []*pooledConn{pc}}

	reader := newWSUpstreamReader(pc, key.channelID, key.keyID)
	if err := reader.Close(); err != nil {
		t.Fatalf("return connection: %v", err)
	}
	reader.CloseWithError()

	if pool.pooledConnCount(key) != 0 {
		t.Fatal("stream error left the returned connection in the pool")
	}
	if !pc.closed {
		t.Fatal("expected errored connection to be marked closed")
	}
}
