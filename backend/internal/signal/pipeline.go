package signal

import (
	"context"
	"time"

	"github.com/tradenexus/backend/internal/logger"
	"github.com/tradenexus/backend/internal/models"
	"github.com/tradenexus/backend/internal/store"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Pipeline handles signal validation, deduplication, persistence, and dispatch.
type Pipeline struct {
	mongoStore *store.MongoStore
	redisStore *store.RedisStore
	onSignal   func(signal models.Signal) // callback to queue for Telegram
}

// NewPipeline creates a new signal pipeline.
func NewPipeline(mongoStore *store.MongoStore, redisStore *store.RedisStore, onSignal func(models.Signal)) *Pipeline {
	return &Pipeline{
		mongoStore: mongoStore,
		redisStore: redisStore,
		onSignal:   onSignal,
	}
}

// Process validates, deduplicates, persists, and dispatches a signal.
func (p *Pipeline) Process(ctx context.Context, signal models.Signal) error {
	log := logger.WithComponent("signal.pipeline")

	// ━━━ Step 1: Check Redis dedup cache (fast path) ━━━
	exists, err := p.redisStore.ExistsDedup(ctx, signal.SignalHash)
	if err != nil {
		log.Warn().Err(err).Str("hash", signal.SignalHash).Msg("Redis dedup check failed, falling through to MongoDB")
	}
	if exists {
		log.Info().
			Str("symbol", signal.Symbol).
			Str("tf", signal.Timeframe).
			Str("type", signal.SignalType).
			Msg("Signal suppressed (Redis dedup)")
		return nil
	}

	// ━━━ Step 2: Check MongoDB persistence (fallback) ━━━
	count, err := p.mongoStore.Signals().CountDocuments(ctx, bson.M{"signalHash": signal.SignalHash})
	if err != nil {
		log.Warn().Err(err).Msg("MongoDB dedup check failed")
	}
	if count > 0 {
		log.Info().
			Str("symbol", signal.Symbol).
			Str("tf", signal.Timeframe).
			Msg("Signal suppressed (MongoDB dedup)")
		// Set Redis cache for faster future lookups
		_ = p.redisStore.SetDedup(ctx, signal.SignalHash)
		return nil
	}

	// ━━━ Step 3: Persist signal to MongoDB ━━━
	signal.CreatedAt = time.Now()
	_, err = p.mongoStore.Signals().InsertOne(ctx, signal)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			log.Info().Str("hash", signal.SignalHash).Msg("Signal already exists (race condition)")
			return nil
		}
		return err
	}

	// ━━━ Step 4: Set Redis dedup cache ━━━
	if err := p.redisStore.SetDedup(ctx, signal.SignalHash); err != nil {
		log.Warn().Err(err).Msg("Failed to set Redis dedup cache")
	}

	// ━━━ Step 5: Dispatch for Telegram ━━━
	if p.onSignal != nil {
		p.onSignal(signal)
	}

	log.Info().
		Str("symbol", signal.Symbol).
		Str("tf", signal.Timeframe).
		Str("type", signal.SignalType).
		Str("category", signal.Category).
		Float64("price", signal.Price).
		Msg("Signal processed and queued for delivery")

	return nil
}

// GetRecentSignals returns the most recent signals within the last 7 days.
func (p *Pipeline) GetRecentSignals(ctx context.Context, limit int, timeframe, category string) ([]models.Signal, error) {
	filter := bson.M{
		"createdAt": bson.M{"$gte": time.Now().Add(-7 * 24 * time.Hour)},
	}
	if timeframe != "" {
		filter["timeframe"] = timeframe
	}
	if category != "" {
		filter["category"] = category
	}

	opts := options.Find().SetSort(bson.M{"createdAt": -1})
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	cursor, err := p.mongoStore.Signals().Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var signals []models.Signal
	return signals, cursor.All(ctx, &signals)
}

// GetSignalStats returns signal counts by category and timeframe.
func (p *Pipeline) GetSignalStats(ctx context.Context) (map[string]int64, error) {
	stats := make(map[string]int64)

	for _, cat := range []string{models.CategoryPineMomentum, models.CategoryWeeklyConsolidated} {
		count, err := p.mongoStore.Signals().CountDocuments(ctx, bson.M{"category": cat})
		if err != nil {
			return nil, err
		}
		stats[cat] = count
	}

	return stats, nil
}
