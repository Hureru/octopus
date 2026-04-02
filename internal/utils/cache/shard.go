package cache

import (
	"sync"
)

type shard[K comparable, V any] struct {
	hashmap map[K]V
	lock    sync.RWMutex
}

func (c *shard[K, V]) set(k K, v V) {
	c.lock.Lock()
	c.hashmap[k] = v
	c.lock.Unlock()
}

func (c *shard[K, V]) get(k K) (V, bool) {
	c.lock.RLock()
	item, exist := c.hashmap[k]
	c.lock.RUnlock()
	if !exist {
		var zero V
		return zero, false
	}
	return item, true
}

func (c *shard[K, V]) del(k K) int {
	c.lock.Lock()
	defer c.lock.Unlock()
	if _, found := c.hashmap[k]; found {
		delete(c.hashmap, k)
		return 1
	}
	return 0
}

func (c *shard[K, V]) clear() {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.hashmap = map[K]V{}
}

func (c *shard[K, V]) getAll() map[K]V {
	c.lock.RLock()
	defer c.lock.RUnlock()
	result := make(map[K]V, len(c.hashmap))
	for k, v := range c.hashmap {
		result[k] = v
	}
	return result
}

func (c *shard[K, V]) len() int {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return len(c.hashmap)
}
