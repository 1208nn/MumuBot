package utils

import (
	"container/list"
	"sync"
	"time"
)

type ttlCacheEntry[K comparable, V any] struct {
	key       K
	value     V
	expiresAt time.Time
}

// TTLCache 是一个带容量上限的并发安全进程内缓存。
type TTLCache[K comparable, V any] struct {
	capacity int
	ttl      time.Duration

	mu      sync.Mutex
	items   map[K]*list.Element
	evictor *list.List
}

func NewTTLCache[K comparable, V any](capacity int, ttl time.Duration) *TTLCache[K, V] {
	if capacity <= 0 {
		capacity = 1
	}
	if ttl <= 0 {
		ttl = time.Minute
	}

	return &TTLCache[K, V]{
		capacity: capacity,
		ttl:      ttl,
		items:    make(map[K]*list.Element, capacity),
		evictor:  list.New(),
	}
}

func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var zero V
	elem, ok := c.items[key]
	if !ok {
		return zero, false
	}

	entry := elem.Value.(*ttlCacheEntry[K, V])
	if time.Now().After(entry.expiresAt) {
		c.removeElement(elem)
		return zero, false
	}

	return entry.value, true
}

func (c *TTLCache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		entry := elem.Value.(*ttlCacheEntry[K, V])
		entry.value = value
		entry.expiresAt = time.Now().Add(c.ttl)
		c.evictor.MoveToBack(elem)
		return
	}

	entry := &ttlCacheEntry[K, V]{
		key:       key,
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}
	elem := c.evictor.PushBack(entry)
	c.items[key] = elem
	c.trimExpiredLocked()
	for len(c.items) > c.capacity {
		c.removeElement(c.evictor.Front())
	}
}

func (c *TTLCache[K, V]) trimExpiredLocked() {
	now := time.Now()
	for elem := c.evictor.Front(); elem != nil; {
		next := elem.Next()
		entry := elem.Value.(*ttlCacheEntry[K, V])
		if now.After(entry.expiresAt) {
			c.removeElement(elem)
		}
		elem = next
	}
}

func (c *TTLCache[K, V]) removeElement(elem *list.Element) {
	if elem == nil {
		return
	}
	entry := elem.Value.(*ttlCacheEntry[K, V])
	delete(c.items, entry.key)
	c.evictor.Remove(elem)
}
