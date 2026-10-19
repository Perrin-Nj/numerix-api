// Package contentdb is the database side of the question bank: save and version questions, move them
// through the review lifecycle, pick the next one for a child, and queue the Mongo sync events.
// The pure rules (validation, answer checking, hashing) live in package content.
//
// Go for Spring people: Repo is a @Repository + @Service in one. Every method that writes takes the
// caller's *sql.Tx (Spring: it joins the caller's @Transactional transaction), so a change, its version
// snapshot, its audit row and its outbox row commit together or not at all.
package contentdb

import (
	"container/list"
	"strconv"
	"strings"
	"sync"

	"github.com/Perrin-Nj/numerix-api/internal/content"
)

// Cache is a small in-memory LRU of parsed questions keyed by "id@version". A question version never
// changes once saved, so an entry is always correct; InvalidateID only frees memory early and makes the
// "invalidate on edit" rule explicit.
//
// Go for Spring people: container/list is a doubly linked list. The map finds an element in O(1); moving
// the element to the front on each hit keeps the most recently used entries, and the back is evicted.
type Cache struct {
	mu    sync.Mutex
	max   int
	order *list.List               // front = most recently used
	items map[string]*list.Element // key -> element whose Value is *entry
	gen   uint64                   // bumped on every invalidation, lets other caches notice a change
	hits  uint64
	miss  uint64
}

// entry is one cached question with its key.
type entry struct {
	key string
	q   *content.Question
}

// NewCache builds an LRU holding at most size questions (min 16).
func NewCache(size int) *Cache {
	return &Cache{max: max(size, 16), order: list.New(), items: map[string]*list.Element{}}
}

// key builds the cache key.
func key(id string, version int) string { return id + "@" + strconv.Itoa(version) }

// Get returns a cached question. The returned pointer is shared: callers must not modify it.
func (c *Cache) Get(id string, version int) (*content.Question, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key(id, version)]; ok {
		c.order.MoveToFront(el)
		c.hits++
		return el.Value.(*entry).q, true
	}
	c.miss++
	return nil, false
}

// Put stores a question, evicting the least recently used one when full.
func (c *Cache) Put(id string, version int, q *content.Question) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	k := key(id, version)
	if el, ok := c.items[k]; ok {
		el.Value.(*entry).q = q
		c.order.MoveToFront(el)
		return
	}
	c.items[k] = c.order.PushFront(&entry{key: k, q: q})
	for c.order.Len() > c.max {
		back := c.order.Back()
		c.order.Remove(back)
		delete(c.items, back.Value.(*entry).key)
	}
}

// InvalidateID drops every cached version of a question (called after an edit, publish, retire or seed).
func (c *Cache) InvalidateID(ids ...string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range ids {
		for k, el := range c.items {
			if strings.HasPrefix(k, id+"@") {
				c.order.Remove(el)
				delete(c.items, k)
			}
		}
	}
	c.gen++
}

// Generation changes every time content was invalidated; watchers refresh their derived data when it moves.
func (c *Cache) Generation() uint64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

// Len, Hits and Misses are for tests and the health output.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// Stats returns hit and miss counters.
func (c *Cache) Stats() (hits, misses uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.miss
}
