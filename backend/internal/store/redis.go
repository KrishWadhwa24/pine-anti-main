package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/tradenexus/backend/internal/config"
	"github.com/tradenexus/backend/internal/logger"
)

// Redis key prefixes.
const (
	PrefixDedup    = "tn:dedup:"
	PrefixCandle   = "tn:candle:"
	PrefixWS       = "tn:ws:"
	PrefixRecovery = "tn:recovery:"
	PrefixLock     = "tn:lock:"
	PrefixQueue    = "tn:queue:"

	// Stream keys
	StreamTelegramDispatch = "tn:telegram:dispatch"
	StreamTelegramDLQ      = "tn:telegram:dlq"
	ConsumerGroupTelegram  = "telegram-dispatchers"
)

// RedisStore wraps the Redis client with namespaced helpers.
type RedisStore struct {
	client *redis.Client
	cfg    *config.Config
}

// NewRedisStore creates a new Redis connection.
func NewRedisStore(ctx context.Context, cfg *config.Config) (*RedisStore, error) {
	log := logger.WithComponent("redis")

	client := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	log.Info().Str("addr", cfg.RedisAddr).Msg("Redis connected")

	store := &RedisStore{client: client, cfg: cfg}

	// Ensure consumer group exists for telegram dispatch stream
	store.ensureConsumerGroup(ctx)

	return store, nil
}

// Client returns the underlying Redis client.
func (r *RedisStore) Client() *redis.Client {
	return r.client
}

// Close shuts down the Redis connection.
func (r *RedisStore) Close() error {
	return r.client.Close()
}

// Ping checks Redis connectivity.
func (r *RedisStore) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// DEDUPLICATION
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// SetDedup stores a signal hash with TTL for deduplication.
func (r *RedisStore) SetDedup(ctx context.Context, hash string) error {
	return r.client.Set(ctx, PrefixDedup+hash, "1", r.cfg.TTLDedupCache).Err()
}

// ExistsDedup checks if a signal hash exists in the dedup cache.
func (r *RedisStore) ExistsDedup(ctx context.Context, hash string) (bool, error) {
	n, err := r.client.Exists(ctx, PrefixDedup+hash).Result()
	return n > 0, err
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// ACTIVE CANDLE BACKUP
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// SetActiveCandle backs up an active candle state to Redis.
func (r *RedisStore) SetActiveCandle(ctx context.Context, key string, data []byte) error {
	return r.client.Set(ctx, PrefixCandle+key, data, r.cfg.TTLActiveCandle).Err()
}

// GetActiveCandle retrieves a backed-up active candle state.
func (r *RedisStore) GetActiveCandle(ctx context.Context, key string) ([]byte, error) {
	val, err := r.client.Get(ctx, PrefixCandle+key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	return val, err
}

// GetActiveCandles retrieves all backed-up active candle states.
func (r *RedisStore) GetActiveCandles(ctx context.Context) (map[string][]byte, error) {
	result := make(map[string][]byte)
	iter := r.client.Scan(ctx, 0, PrefixCandle+"*", 100).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		val, err := r.client.Get(ctx, key).Bytes()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			return nil, err
		}
		result[key[len(PrefixCandle):]] = val
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteActiveCandle removes a backed-up active candle state.
func (r *RedisStore) DeleteActiveCandle(ctx context.Context, key string) error {
	return r.client.Del(ctx, PrefixCandle+key).Err()
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// TELEGRAM DISPATCH STREAM
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// EnqueueTelegram adds a message to the Telegram dispatch stream.
func (r *RedisStore) EnqueueTelegram(ctx context.Context, msg map[string]interface{}) error {
	return r.client.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamTelegramDispatch,
		Values: msg,
	}).Err()
}

// ReadTelegramStream reads messages from the telegram dispatch stream for a consumer.
func (r *RedisStore) ReadTelegramStream(ctx context.Context, consumer string, count int64, block time.Duration) ([]redis.XStream, error) {
	return r.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    ConsumerGroupTelegram,
		Consumer: consumer,
		Streams:  []string{StreamTelegramDispatch, ">"},
		Count:    count,
		Block:    block,
	}).Result()
}

// AckTelegram acknowledges a processed message.
func (r *RedisStore) AckTelegram(ctx context.Context, id string) error {
	return r.client.XAck(ctx, StreamTelegramDispatch, ConsumerGroupTelegram, id).Err()
}

// MoveToDLQ moves a failed message to the dead letter queue.
func (r *RedisStore) MoveToDLQ(ctx context.Context, msg map[string]interface{}) error {
	return r.client.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamTelegramDLQ,
		Values: msg,
	}).Err()
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// GENERIC HELPERS
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// SetJSON stores a JSON-serializable value with TTL.
func (r *RedisStore) SetJSON(ctx context.Context, key string, val interface{}, ttl time.Duration) error {
	data, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, key, data, ttl).Err()
}

// GetJSON retrieves and unmarshals a JSON value.
func (r *RedisStore) GetJSON(ctx context.Context, key string, dest interface{}) error {
	val, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(val, dest)
}

func (r *RedisStore) ensureConsumerGroup(ctx context.Context) {
	log := logger.WithComponent("redis")
	// MKSTREAM creates the stream if it doesn't exist
	err := r.client.XGroupCreateMkStream(ctx, StreamTelegramDispatch, ConsumerGroupTelegram, "0").Err()
	if err != nil {
		// Group already exists is fine
		log.Debug().Err(err).Msg("Consumer group creation (may already exist)")
	}
}
