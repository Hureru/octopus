package op

import (
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/utils/cache"
)

type rateLimitEntry struct {
	mu     sync.Mutex
	minute int64
	count  int64
}

var rateLimitCache = cache.New[int, *rateLimitEntry](16)

func RateLimitCheck(apiKeyID int, maxRPM int) (bool, int) {
	if maxRPM <= 0 {
		return true, 0
	}

	now := time.Now()
	currentMinute := now.Unix() / 60

	entry, _ := rateLimitCache.GetOrSet(apiKeyID, &rateLimitEntry{})

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.minute != currentMinute {
		entry.minute = currentMinute
		entry.count = 0
	}

	entry.count++
	if entry.count > int64(maxRPM) {
		retryAfter := 60 - int(now.Unix()%60)
		if retryAfter <= 0 {
			retryAfter = 1
		}
		return false, retryAfter
	}

	return true, 0
}

func RateLimitDel(apiKeyID int) {
	rateLimitCache.Del(apiKeyID)
}
