package catalog

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Reader is where a cache's value comes from: one read of Orrery, or a fake
// in tests.
type Reader[T any] func(ctx context.Context) (T, error)

// Snapshot is what a page gets: the value, when it was read, and whether it
// is the last good one because the catalog cannot be reached now.
type Snapshot[T any] struct {
	Value   T
	Fetched time.Time
	Stale   bool
	Err     error
}

// Cache reads its source at most once per TTL and keeps the last good
// answer through failures. One read at a time: callers queue behind it.
type Cache[T any] struct {
	name   string
	read   Reader[T]
	size   func(T) int
	ttl    time.Duration
	now    func() time.Time
	log    *slog.Logger
	onRead func(ctx context.Context, outcome string)

	mu      sync.Mutex
	value   T
	good    time.Time // when value was last read with success
	tried   time.Time // when the source was last asked, either way
	lastErr error
}

// NewCache wraps a reader. name starts its log lines ("catalog read",
// "landscape read failed"); size counts what a read returned, for the log.
// onRead is told the outcome of each read, "ok" or "failed", for a metric.
func NewCache[T any](name string, read Reader[T], size func(T) int, ttl time.Duration, log *slog.Logger, onRead func(ctx context.Context, outcome string)) *Cache[T] {
	if onRead == nil {
		onRead = func(context.Context, string) {}
	}
	return &Cache[T]{name: name, read: read, size: size, ttl: ttl, now: time.Now, log: log, onRead: onRead}
}

// Get answers from the cache, reading the source when the cache is older
// than the TTL. A failed read keeps the last good value and marks it stale.
func (c *Cache[T]) Get(ctx context.Context) Snapshot[T] {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.tried.IsZero() || now.Sub(c.tried) >= c.ttl {
		c.tried = now
		value, err := c.read(ctx)
		if err != nil {
			c.lastErr = err
			c.onRead(ctx, "failed")
			c.log.ErrorContext(ctx, c.name+" read failed", "error", err, "last_good", c.good, "kept", c.size(c.value))
		} else {
			c.value, c.good, c.lastErr = value, now, nil
			c.onRead(ctx, "ok")
			c.log.InfoContext(ctx, c.name+" read", "items", c.size(value))
		}
	}
	return Snapshot[T]{Value: c.value, Fetched: c.good, Stale: c.lastErr != nil, Err: c.lastErr}
}

// Invalidate makes the next Get read the source, for after a write that
// changes what it holds.
func (c *Cache[T]) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tried = time.Time{}
}

// Age is how old the value is, for a gauge. Zero before the first read.
func (c *Cache[T]) Age() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.good.IsZero() {
		return 0
	}
	return c.now().Sub(c.good)
}

// NewTiles is the cache of tiles, logged as the catalog.
func NewTiles(read Reader[Directory], ttl time.Duration, log *slog.Logger, onRead func(ctx context.Context, outcome string)) *Cache[Directory] {
	return NewCache("catalog", read, func(d Directory) int { return len(d.Apps) }, ttl, log, onRead)
}

// NewCoverageCache is the cache of what the lenses draw from, logged as
// the landscape.
func NewCoverageCache(read Reader[Coverage], ttl time.Duration, log *slog.Logger, onRead func(ctx context.Context, outcome string)) *Cache[Coverage] {
	return NewCache("landscape", read, func(c Coverage) int { return len(c.Services) + len(c.APIs) }, ttl, log, onRead)
}
