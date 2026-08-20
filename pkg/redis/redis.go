package redis

import (
	"context"
	"fmt"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type Redis struct {
	Client *goredis.Client

	// mem is an in-process fallback used when no Redis server is reachable
	// (the "running without cache" mode). It keeps refresh-token caching and
	// quote caching functional on a single instance without an external Redis.
	mu  sync.RWMutex
	mem map[string]memEntry
}

type memEntry struct {
	value string
	exp   time.Time
}

type Config struct {
	Host     string
	Port     int
	Password string
	DB       int
}

// NewRedis connects to Redis. If the server is unreachable it does NOT fail:
// it returns a non-nil *Redis backed by an in-process map so callers never hit
// a nil-pointer panic. Callers can detect the degraded mode via Client == nil.
func NewRedis(cfg Config) (*Redis, error) {
	r := &Redis{mem: make(map[string]memEntry)}
	client := goredis.NewClient(&goredis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		// Degraded mode: no external Redis, use in-process fallback.
		return r, nil
	}
	r.Client = client
	return r, nil
}

func (r *Redis) Close() error {
	if r.Client != nil {
		return r.Client.Close()
	}
	return nil
}

// Cache helpers (nil-safe: fall back to in-process map when Client == nil)
func (r *Redis) CacheSet(ctx context.Context, key string, value any, ttl time.Duration) error {
	if r.Client != nil {
		return r.Client.Set(ctx, key, value, ttl).Err()
	}
	r.mu.Lock()
	r.mem[key] = memEntry{value: fmt.Sprintf("%v", value), exp: time.Now().Add(ttl)}
	r.mu.Unlock()
	return nil
}

func (r *Redis) CacheGet(ctx context.Context, key string) (string, error) {
	if r.Client != nil {
		return r.Client.Get(ctx, key).Result()
	}
	r.mu.RLock()
	e, ok := r.mem[key]
	r.mu.RUnlock()
	if !ok || time.Now().After(e.exp) {
		if ok {
			r.mu.Lock()
			delete(r.mem, key)
			r.mu.Unlock()
		}
		return "", goredis.Nil
	}
	return e.value, nil
}

func (r *Redis) CacheDel(ctx context.Context, keys ...string) error {
	if r.Client != nil {
		return r.Client.Del(ctx, keys...).Err()
	}
	r.mu.Lock()
	for _, k := range keys {
		delete(r.mem, k)
	}
	r.mu.Unlock()
	return nil
}

// RateLimit is best-effort: without a real Redis it always allows (the app's
// separate in-process limiters in pkg/ratelimit already cover registration abuse).
func (r *Redis) RateLimit(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	if r.Client != nil {
		pipe := r.Client.Pipeline()
		incr := pipe.Incr(ctx, key)
		pipe.Expire(ctx, key, window)
		if _, err := pipe.Exec(ctx); err != nil {
			return false, err
		}
		return incr.Val() <= int64(limit), nil
	}
	return true, nil
}
