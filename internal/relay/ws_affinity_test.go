package relay

import (
	"fmt"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/utils/cache"
)

func TestWSAffinityHotCacheRemovesExpiredEntriesAndCapsSize(t *testing.T) {
	now := time.Now()
	store := &dbWSAffinityStore{
		hot: cache.New[string, wsAffinityEntry](wsAffinityCacheShards),
	}
	store.hot.Set("expired", wsAffinityEntry{ExpiresAt: now.Add(-time.Second)})
	for i := 0; i < wsAffinityHotMaxItems+100; i++ {
		store.hot.Set(fmt.Sprintf("key-%d", i), wsAffinityEntry{
			ExpiresAt: now.Add(time.Duration(i+1) * time.Second),
		})
	}

	store.maintainHotCache(now)

	if _, ok := store.hot.Get("expired"); ok {
		t.Fatal("expected expired affinity entry to be removed")
	}
	if got := store.hot.Len(); got > wsAffinityHotMaxItems {
		t.Fatalf("hot affinity cache length = %d, want <= %d", got, wsAffinityHotMaxItems)
	}
}
