package candle

import (
	"context"
	"encoding/json"
	"time"

	"github.com/tradenexus/backend/internal/broker"
	"github.com/tradenexus/backend/internal/logger"
	"github.com/tradenexus/backend/internal/models"
	"github.com/tradenexus/backend/internal/store"
	"github.com/tradenexus/backend/internal/worker"
)

// Engine orchestrates the entire candle pipeline:
// ticks → builder → aggregator → store → event bus
type Engine struct {
	builder    *Builder
	aggregator *Aggregator
	store      *Store
	redis      *store.RedisStore
	eventBus   *worker.EventBus
	tickChan   chan *broker.Tick
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewEngine creates and wires up the candle engine components.
func NewEngine(store *Store, redisStore *store.RedisStore, eventBus *worker.EventBus, tickChan chan *broker.Tick) *Engine {
	e := &Engine{
		store:    store,
		redis:    redisStore,
		eventBus: eventBus,
		tickChan: tickChan,
	}

	// Wire up the pipeline callbacks
	e.aggregator = NewAggregator(func(candle models.Candle) {
		e.onHigherTFFinalized(candle)
	})

	e.builder = NewBuilder(func(candle models.Candle) {
		e.on1HFinalized(candle)
	})
	if redisStore != nil {
		e.builder.SetPersistenceCallbacks(
			func(key string, candle models.ActiveCandle) {
				log := logger.WithComponent("candle.engine")
				data, err := json.Marshal(candle)
				if err != nil {
					log.Warn().Err(err).Str("key", key).Msg("Failed to encode active candle")
					return
				}
				if err := redisStore.SetActiveCandle(context.Background(), key, data); err != nil {
					log.Warn().Err(err).Str("key", key).Msg("Failed to backup active candle")
				}
			},
			func(key string) {
				log := logger.WithComponent("candle.engine")
				if err := redisStore.DeleteActiveCandle(context.Background(), key); err != nil {
					log.Warn().Err(err).Str("key", key).Msg("Failed to delete active candle backup")
				}
			},
		)
	}

	return e
}

// RestoreActiveCandles loads backed-up active candles from Redis.
func (e *Engine) RestoreActiveCandles(ctx context.Context) {
	if e.redis == nil {
		return
	}

	log := logger.WithComponent("candle.engine")
	raw, err := e.redis.GetActiveCandles(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load active candle backups")
		return
	}

	candles := make(map[string]*models.ActiveCandle, len(raw))
	for key, data := range raw {
		var ac models.ActiveCandle
		if err := json.Unmarshal(data, &ac); err != nil {
			log.Warn().Err(err).Str("key", key).Msg("Skipping invalid active candle backup")
			continue
		}
		candles[key] = &ac
	}

	restored := e.builder.RestoreActiveCandles(candles)
	log.Info().Int("restored", restored).Int("available", len(raw)).Msg("Active candle backups restored")
}

// Start begins processing ticks and checking for finalizations.
func (e *Engine) Start(ctx context.Context) {
	log := logger.WithComponent("candle.engine")
	e.ctx, e.cancel = context.WithCancel(ctx)

	// Goroutine: process incoming ticks
	go func() {
		for {
			select {
			case <-e.ctx.Done():
				return
			case tick, ok := <-e.tickChan:
				if !ok {
					return
				}
				e.builder.ProcessTick(tick)
				e.eventBus.Publish(worker.Event{
					Type: worker.EventTickReceived,
					Payload: worker.TickReceivedPayload{
						Token:     tick.Token,
						Price:     tick.LastTradedPrice,
						Timestamp: time.Now().Unix(),
					},
				})
			}
		}
	}()

	// Goroutine: periodic finalization check (every 5 seconds)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-e.ctx.Done():
				return
			case <-ticker.C:
				finalized := e.builder.CheckFinalizations()
				for _, c := range finalized {
					e.on1HFinalized(c)
				}
			}
		}
	}()

	log.Info().Msg("Candle engine started")
}

// ProcessHistoricalCandle processes a candle from historical data (used during recovery).
func (e *Engine) ProcessHistoricalCandle(candle models.Candle) {
	ctx := context.Background()

	// Save to MongoDB
	if err := e.store.SaveCandle(ctx, candle); err != nil {
		log := logger.WithComponent("candle.engine")
		log.Error().Err(err).Str("symbol", candle.Symbol).Msg("Failed to save historical candle")
		return
	}

	// Route to appropriate aggregation
	switch candle.Timeframe {
	case models.Timeframe1H:
		e.aggregator.ProcessFinalized1H(candle)
	case models.Timeframe1D:
		e.aggregator.ProcessFinalizedDaily(candle)
	}

	// Emit event for strategy processing
	e.eventBus.PublishSync(worker.Event{
		Type: worker.EventCandleFinalized,
		Payload: worker.CandleFinalizedPayload{
			Symbol:    candle.Symbol,
			Timeframe: candle.Timeframe,
			Timestamp: candle.Timestamp.Unix(),
		},
	})
}

// Stop shuts down the candle engine.
func (e *Engine) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
}

// GetBuilder returns the builder for active candle queries.
func (e *Engine) GetBuilder() *Builder {
	return e.builder
}

// on1HFinalized handles a finalized 1H candle.
func (e *Engine) on1HFinalized(candle models.Candle) {
	log := logger.WithComponent("candle.engine")
	ctx := context.Background()

	// Persist to MongoDB
	if err := e.store.SaveCandle(ctx, candle); err != nil {
		log.Error().Err(err).Str("symbol", candle.Symbol).Msg("Failed to save 1H candle")
	}

	// Feed to 4H aggregator
	e.aggregator.ProcessFinalized1H(candle)

	// 1H candles don't trigger strategy directly — only 4H+ timeframes do
	log.Debug().Str("symbol", candle.Symbol).Time("ts", candle.Timestamp).Msg("1H candle saved")
}

// onHigherTFFinalized handles finalized 4H/1D/1W/1M candles.
func (e *Engine) onHigherTFFinalized(candle models.Candle) {
	log := logger.WithComponent("candle.engine")
	ctx := context.Background()

	// Persist to MongoDB
	if err := e.store.SaveCandle(ctx, candle); err != nil {
		log.Error().Err(err).Str("symbol", candle.Symbol).Str("tf", candle.Timeframe).Msg("Failed to save candle")
	}

	// If daily and from historical backfill, also feed to weekly/monthly aggregator.
	// Skip re-feeding aggregated candles to avoid double-processing.
	if candle.Timeframe == models.Timeframe1D && candle.Source != "aggregated" {
		e.aggregator.ProcessFinalizedDaily(candle)
	}

	// Emit candle finalized event for strategy engine (4H, 1D, 1W, 1M only)
	e.eventBus.Publish(worker.Event{
		Type: worker.EventCandleFinalized,
		Payload: worker.CandleFinalizedPayload{
			Symbol:    candle.Symbol,
			Timeframe: candle.Timeframe,
			Timestamp: candle.Timestamp.Unix(),
		},
	})

	log.Info().
		Str("symbol", candle.Symbol).
		Str("tf", candle.Timeframe).
		Time("ts", candle.Timestamp).
		Float64("o", candle.Open).
		Float64("h", candle.High).
		Float64("l", candle.Low).
		Float64("c", candle.Close).
		Int64("v", candle.Volume).
		Msg("Higher TF candle finalized and event emitted")
}
