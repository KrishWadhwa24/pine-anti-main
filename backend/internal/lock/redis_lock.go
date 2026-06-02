package lock

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/tradenexus/backend/internal/logger"
)

// RedisLock provides distributed locking via Redis SET NX.
type RedisLock struct {
	client *redis.Client
	key    string
	value  string
	ttl    time.Duration
}

// NewRedisLock creates a distributed lock.
func NewRedisLock(client *redis.Client, resource string, ttl time.Duration) *RedisLock {
	return &RedisLock{
		client: client,
		key:    "tn:lock:" + resource,
		value:  fmt.Sprintf("%d", time.Now().UnixNano()),
		ttl:    ttl,
	}
}

// Acquire attempts to acquire the lock. Returns true if successful.
func (l *RedisLock) Acquire(ctx context.Context) bool {
	log := logger.WithComponent("lock")
	ok, err := l.client.SetNX(ctx, l.key, l.value, l.ttl).Result()
	if err != nil {
		log.Error().Err(err).Str("key", l.key).Msg("Lock acquire failed")
		return false
	}
	if ok {
		log.Debug().Str("key", l.key).Msg("Lock acquired")
	}
	return ok
}

// Release releases the lock (only if we still own it).
func (l *RedisLock) Release(ctx context.Context) {
	log := logger.WithComponent("lock")
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`
	_, err := l.client.Eval(ctx, script, []string{l.key}, l.value).Result()
	if err != nil {
		log.Error().Err(err).Str("key", l.key).Msg("Lock release failed")
	}
	log.Debug().Str("key", l.key).Msg("Lock released")
}

// Extend extends the lock TTL.
func (l *RedisLock) Extend(ctx context.Context, ttl time.Duration) bool {
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("PEXPIRE", KEYS[1], ARGV[2])
		else
			return 0
		end
	`
	result, err := l.client.Eval(ctx, script, []string{l.key}, l.value, ttl.Milliseconds()).Result()
	if err != nil {
		return false
	}
	return result.(int64) == 1
}
