// This implementation is based on and modified from https://github.com/fanjindong/go-cache
package cache

import (
	"fmt"

	"github.com/cespare/xxhash/v2"
)

func keyToString[K comparable](key K) string {
	return fmt.Sprintf("%v", key)
}

type Cache[K comparable, V any] interface {
	Set(k K, v V)
	Get(k K) (V, bool)
	GetAll() map[K]V
	Del(keys ...K) int
	Exists(keys ...K) bool
	Len() int
	Clear()
}

func New[K comparable, V any](shards int) Cache[K, V] {
	if shards <= 0 {
		shards = 1024
	}

	c := &cache[K, V]{
		shards:    make([]*shard[K, V], shards),
		shardMask: uint64(shards - 1),
	}
	for i := 0; i < shards; i++ {
		c.shards[i] = &shard[K, V]{hashmap: map[K]V{}}
	}

	return c
}

type cache[K comparable, V any] struct {
	shards    []*shard[K, V]
	shardMask uint64
}

func (c *cache[K, V]) Set(k K, v V) {
	hashedKey := xxhash.Sum64String(keyToString(k))
	shard := c.getShard(hashedKey)
	shard.set(k, v)
}

func (c *cache[K, V]) Get(k K) (V, bool) {
	hashedKey := xxhash.Sum64String(keyToString(k))
	shard := c.getShard(hashedKey)
	return shard.get(k)
}

func (c *cache[K, V]) GetAll() map[K]V {
	result := make(map[K]V)
	for _, shard := range c.shards {
		shardData := shard.getAll()
		for k, v := range shardData {
			result[k] = v
		}
	}
	return result
}

func (c *cache[K, V]) Del(ks ...K) int {
	var count int
	for _, k := range ks {
		hashedKey := xxhash.Sum64String(keyToString(k))
		shard := c.getShard(hashedKey)
		count += shard.del(k)
	}
	return count
}

func (c *cache[K, V]) Exists(ks ...K) bool {
	for _, k := range ks {
		if _, found := c.Get(k); !found {
			return false
		}
	}
	return true
}

func (c *cache[K, V]) Len() int {
	var count int
	for _, shard := range c.shards {
		count += shard.len()
	}
	return count
}

func (c *cache[K, V]) getShard(hashedKey uint64) (shard *shard[K, V]) {
	return c.shards[hashedKey&c.shardMask]
}

func (c *cache[K, V]) Clear() {
	for _, s := range c.shards {
		s.clear()
	}
}
