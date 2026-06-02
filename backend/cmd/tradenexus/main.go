package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/tradenexus/backend/internal/api"
	"github.com/tradenexus/backend/internal/broker"
	"github.com/tradenexus/backend/internal/candle"
	"github.com/tradenexus/backend/internal/config"
	"github.com/tradenexus/backend/internal/indicator"
	"github.com/tradenexus/backend/internal/logger"
	"github.com/tradenexus/backend/internal/models"
	"github.com/tradenexus/backend/internal/scanner"
	signalPkg "github.com/tradenexus/backend/internal/signal"
	"github.com/tradenexus/backend/internal/store"
	"github.com/tradenexus/backend/internal/telegram"
	"github.com/tradenexus/backend/internal/worker"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	// ━━━ Load Configuration ━━━
	cfg, err := config.Load()
	if err != nil {
		panic("Failed to load config: " + err.Error())
	}

	// ━━━ Initialize Logger ━━━
	logger.Init(cfg.LogLevel)
	log := logger.WithComponent("main")
	log.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Info().Msg("  TradeNexus — Starting Up")
	log.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ━━━ Initialize MongoDB ━━━
	mongoStore, err := store.NewMongoStore(ctx, cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("MongoDB connection failed")
	}
	defer mongoStore.Close(ctx)

	// ━━━ Initialize Redis ━━━
	redisStore, err := store.NewRedisStore(ctx, cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Redis connection failed")
	}
	defer redisStore.Close()

	// ━━━ Initialize Encryptor ━━━
	encryptor, err := store.NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		log.Warn().Err(err).Msg("Encryption disabled — set ENCRYPTION_KEY in .env")
		encryptor = nil
	}

	// ━━━ Initialize Event Bus ━━━
	eventBus := worker.NewEventBus()

	// ━━━ Initialize Broker Auth Pool (round-robin across 1-3 API credentials) ━━━
	authPool := broker.NewAuthPool(cfg)

	// ━━━ Initialize Symbol Resolver ━━━
	symbolResolver := broker.NewSymbolResolver()
	go func() {
		if err := symbolResolver.Refresh(); err != nil {
			log.Error().Err(err).Msg("Symbol master refresh failed")
		}
	}()

	// ━━━ Initialize Tick Channel ━━━
	tickChan := make(chan *broker.Tick, 10000)

	// ━━━ Initialize Candle Engine ━━━
	candleStore := candle.NewStore(mongoStore.Candles())
	candleEngine := candle.NewEngine(candleStore, redisStore, eventBus, tickChan)
	candleEngine.RestoreActiveCandles(ctx)
	candleEngine.Start(ctx)

	// ━━━ Initialize Indicator Manager ━━━
	indicatorMgr := indicator.NewManager(mongoStore.IndicatorSnapshots())
	if err := indicatorMgr.LoadAllSnapshots(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to load indicator snapshots")
	}

	// ━━━ Initialize Strategy Engine ━━━
	pineEngine := NewStrategyRunner(cfg, indicatorMgr, candleStore, eventBus)

	// ━━━ Initialize Telegram Dispatcher ━━━
	telegramDisp := telegram.NewDispatcher(redisStore)
	loadTelegramCredentials(ctx, cfg, mongoStore, encryptor, telegramDisp)
	telegramDisp.Start(ctx)

	// ━━━ Initialize Signal Pipeline ━━━
	signalPipeline := signalPkg.NewPipeline(mongoStore, redisStore, func(sig models.Signal) {
		if err := telegramDisp.EnqueueSignal(ctx, sig); err != nil {
			log.Error().Err(err).Str("symbol", sig.Symbol).Msg("Failed to enqueue Telegram alert")
		}
	})

	// ━━━ Wire Strategy Engine to Signal Pipeline ━━━
	pineEngine.SetSignalPipeline(signalPipeline)
	pineEngine.Start(ctx)

	// ━━━ Recover saved strategy progress and missed finalized candles ━━━
	go recoverCandleAndStrategyState(ctx, cfg, mongoStore, authPool, candleStore, candleEngine, pineEngine)

	// ━━━ Initialize Weekly Scanner ━━━
	weeklyEngine := scanner.NewWeeklyEngine(candleStore)
	go func() {
		if err := runWeeklyScannerCycle(ctx, mongoStore, weeklyEngine, signalPipeline, candleStore); err != nil {
			log.Error().Err(err).Msg("Startup automatic weekly scanner catch-up failed")
		}
	}()

	// ━━━ Initialize WebSocket Manager ━━━
	wsManager := broker.NewWebSocketManager(authPool.Primary(), tickChan)

	// ━━━ Connect WebSocket and Subscribe ━━━
	go func() {
		// Wait for symbol resolver to be ready
		time.Sleep(5 * time.Second)

		if !authPool.HasCredentials() {
			log.Warn().Msg("Angel One credentials not configured — WebSocket disabled")
			return
		}

		if err := wsManager.Connect(ctx); err != nil {
			log.Error().Err(err).Msg("WebSocket connection failed")
			return
		}

		// Subscribe to all stocks in active watchlists
		subscribeWatchlistStocks(ctx, mongoStore, wsManager)
	}()

	// ━━━ Start API Server ━━━
	apiServer := api.NewServer(
		cfg, mongoStore, redisStore, encryptor,
		wsManager, symbolResolver, candleStore,
		indicatorMgr, signalPipeline, weeklyEngine, telegramDisp, eventBus,
	)

	go func() {
		if err := apiServer.Start(cfg.ServerPort); err != nil {
			log.Fatal().Err(err).Msg("API server failed")
		}
	}()

	log.Info().Str("port", cfg.ServerPort).Msg("TradeNexus is running")
	log.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// ━━━ Periodic Tasks ━━━
	go periodicTasks(ctx, indicatorMgr, symbolResolver, mongoStore, weeklyEngine, signalPipeline, candleStore)

	// ━━━ Graceful Shutdown ━━━
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down TradeNexus...")

	// Save indicator snapshots before shutdown
	if err := indicatorMgr.SaveAllSnapshots(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to save indicator snapshots on shutdown")
	}

	wsManager.Close()
	candleEngine.Stop()
	cancel()

	log.Info().Msg("TradeNexus shutdown complete")
}

// ━━━ Strategy Runner bridges candle events to Pine strategy ━━━

func NewStrategyRunner(cfg *config.Config, im *indicator.Manager, cs *candle.Store, eb *worker.EventBus) *StrategyRunnerImpl {
	return &StrategyRunnerImpl{
		cfg:          cfg,
		indicatorMgr: im,
		candleStore:  cs,
		eventBus:     eb,
	}
}

type StrategyRunnerImpl struct {
	cfg          *config.Config
	indicatorMgr *indicator.Manager
	candleStore  *candle.Store
	eventBus     *worker.EventBus
	pipeline     *signalPkg.Pipeline
	pine         *pineEngineWrapper
	labelsMu     sync.RWMutex
	symbolLabels map[string]string // token -> watchlist symbol
}

func (sr *StrategyRunnerImpl) SetSignalPipeline(p *signalPkg.Pipeline) {
	sr.pipeline = p
}

func (sr *StrategyRunnerImpl) SetSymbolLabels(stocks []models.Stock) {
	labels := make(map[string]string, len(stocks))
	for _, stock := range stocks {
		if stock.Token == "" || stock.Symbol == "" {
			continue
		}
		labels[stock.Token] = stock.Symbol
	}

	sr.labelsMu.Lock()
	sr.symbolLabels = labels
	sr.labelsMu.Unlock()
}

func (sr *StrategyRunnerImpl) displaySymbol(token string) string {
	sr.labelsMu.RLock()
	label := sr.symbolLabels[token]
	sr.labelsMu.RUnlock()
	if label != "" {
		return label
	}
	return token
}

func (sr *StrategyRunnerImpl) Start(ctx context.Context) {
	log := logger.WithComponent("strategy.runner")

	// Import strategy package
	sr.pine = newPineEngineFromConfig(sr.cfg)

	// Subscribe to candle finalized events
	sr.eventBus.Subscribe(worker.EventCandleFinalized, func(event worker.Event) {
		payload, ok := event.Payload.(worker.CandleFinalizedPayload)
		if !ok {
			return
		}

		// Only process 4H, 1D, 1W, 1M
		switch payload.Timeframe {
		case models.Timeframe4H, models.Timeframe1D, models.Timeframe1W, models.Timeframe1M:
		default:
			return
		}

		// Get the finalized candle
		ts := time.Unix(payload.Timestamp, 0)
		candles, err := sr.candleStore.GetCandles(ctx, payload.Symbol, payload.Timeframe, ts, ts.Add(time.Second))
		if err != nil || len(candles) == 0 {
			log.Warn().Str("symbol", payload.Symbol).Msg("Could not find finalized candle for strategy")
			return
		}

		sr.processStrategyCandle(ctx, candles[0], time.Time{})
	})

	log.Info().Msg("Strategy runner started")
}

func (sr *StrategyRunnerImpl) processStrategyCandle(ctx context.Context, c models.Candle, alertFrom time.Time) {
	log := logger.WithComponent("strategy.runner")
	if sr.pine == nil {
		sr.pine = newPineEngineFromConfig(sr.cfg)
	}

	state := sr.indicatorMgr.GetOrLoad(ctx, c.Symbol, c.Timeframe)
	state.UpdateFromCandle(c)

	result := sr.pine.Evaluate(ctx, c, state)
	if result == nil {
		return
	}
	if !alertFrom.IsZero() && c.Timestamp.Before(alertFrom) {
		return
	}

	sig := sr.pine.BuildSignal(c, result)
	if sig != nil && sr.pipeline != nil {
		sig.Symbol = sr.displaySymbol(c.Symbol)
		if err := sr.pipeline.Process(ctx, *sig); err != nil {
			log.Error().Err(err).Str("symbol", sig.Symbol).Msg("Signal pipeline error")
		}
	}

	if state.BarIndex%5 == 0 {
		_ = sr.indicatorMgr.SaveSnapshot(ctx, state)
	}
}

func (sr *StrategyRunnerImpl) WarmAndReplayRecent(ctx context.Context, stocks []models.Stock, lookback time.Duration) {
	log := logger.WithComponent("recovery")
	sr.SetSymbolLabels(stocks)
	alertFrom := time.Now().In(candle.IST).Add(-lookback)
	timeframes := []string{models.Timeframe4H, models.Timeframe1D, models.Timeframe1W, models.Timeframe1M}
	replayed := 0

	for _, stock := range stocks {
		for _, tf := range timeframes {
			limit := strategyWarmupLimit(sr.cfg, tf)
			candles, err := sr.candleStore.GetRecentCandles(ctx, stock.Token, tf, limit)
			if err != nil {
				log.Warn().Err(err).Str("symbol", stock.Symbol).Str("tf", tf).Msg("Failed to load candles for recovery replay")
				continue
			}
			if len(candles) == 0 {
				continue
			}

			sr.indicatorMgr.ResetState(stock.Token, tf)
			for _, c := range candles {
				sr.processStrategyCandle(ctx, c, alertFrom)
				if !c.Timestamp.Before(alertFrom) {
					replayed++
				}
			}
		}
	}

	log.Info().Int("candles", replayed).Dur("lookback", lookback).Msg("Recent strategy candles replayed")
}

func strategyWarmupLimit(cfg *config.Config, timeframe string) int {
	switch timeframe {
	case models.Timeframe4H:
		return cfg.Warmup4H + 20
	case models.Timeframe1D:
		return cfg.Warmup1D + 10
	case models.Timeframe1W:
		return cfg.Warmup1W + 5
	case models.Timeframe1M:
		return cfg.Warmup1M + 2
	default:
		return 100
	}
}

func newPineEngineFromConfig(cfg *config.Config) *pineEngineWrapper {
	return &pineEngineWrapper{
		breakoutLookback: cfg.BreakoutLookback,
		volumeMultiplier: cfg.VolumeMultiplier,
		cooldownBars:     cfg.CooldownBars,
	}
}

// pineEngineWrapper wraps the strategy package to avoid import cycles
type pineEngineWrapper struct {
	breakoutLookback int
	volumeMultiplier float64
	cooldownBars     int
}

func (p *pineEngineWrapper) Evaluate(ctx context.Context, c models.Candle, state *indicator.State) *pineResult {
	if !state.IsReady() {
		return nil
	}

	result := &pineResult{}

	ema10 := state.EMA10.Value
	ema20 := state.EMA20.Value
	sma40 := state.SMA40.Value

	result.BullTrend = ema10 > ema20 && ema20 > sma40 && c.Close > ema10 && sma40 > state.PrevSMA40
	result.BearTrend = ema10 < ema20 && ema20 < sma40 && c.Close < ema10 && sma40 < state.PrevSMA40
	result.HighestLevel = state.Breakout20.PrevHighest
	result.LowestLevel = state.Breakout20.PrevLowest
	result.FreshBullBreak = indicator.FreshBullBreakout(c.Close, state.PrevClose, result.HighestLevel)
	result.FreshBearBreak = indicator.FreshBearBreakout(c.Close, state.PrevClose, result.LowestLevel)

	avgVol := state.VolSMA20.Value
	if avgVol > 0 {
		result.RelativeVolume = float64(c.Volume) / avgVol
	}
	result.VolumeSpike = float64(c.Volume) > avgVol*p.volumeMultiplier

	import_math_abs := func(x float64) float64 {
		if x < 0 {
			return -x
		}
		return x
	}
	bodySize := import_math_abs(c.Close - c.Open)
	atr := state.ATR14.Value
	result.ATRValue = atr
	if atr > 0 {
		result.BodyStrength = bodySize / atr
	}
	result.StrongBullCandle = c.Close > c.Open && bodySize > atr*0.5
	result.StrongBearCandle = c.Close < c.Open && bodySize > atr*0.5
	result.RSIValue = state.RSI14.Value
	result.BullMomentum = result.RSIValue > 60
	result.BearMomentum = result.RSIValue < 40

	resetLong := c.Close < ema10 || indicator.Crossunder(ema10, ema20, state.PrevEMA10, state.PrevEMA20)
	resetShort := c.Close > ema10 || indicator.Crossover(ema10, ema20, state.PrevEMA10, state.PrevEMA20)
	if resetLong {
		state.LongActive = false
	}
	if resetShort {
		state.ShortActive = false
	}

	canBuy := (state.BarIndex - state.LastBuyBar) > p.cooldownBars
	canSell := (state.BarIndex - state.LastSellBar) > p.cooldownBars

	result.BuySignal = result.BullTrend && result.FreshBullBreak && result.VolumeSpike &&
		result.StrongBullCandle && result.BullMomentum && !state.LongActive && canBuy
	result.SellSignal = result.BearTrend && result.FreshBearBreak && result.VolumeSpike &&
		result.StrongBearCandle && result.BearMomentum && !state.ShortActive && canSell

	if result.BuySignal {
		state.LongActive = true
		state.ShortActive = false
		state.LastBuyBar = state.BarIndex
	}
	if result.SellSignal {
		state.ShortActive = true
		state.LongActive = false
		state.LastSellBar = state.BarIndex
	}

	return result
}

func (p *pineEngineWrapper) BuildSignal(c models.Candle, r *pineResult) *models.Signal {
	if !r.BuySignal && !r.SellSignal {
		return nil
	}

	st := models.SignalBuy
	br := "Close crossed above 20-bar high"
	tc := "EMA 10 > 20 > SMA 40 (Bullish stack)"
	if r.SellSignal {
		st = models.SignalSell
		br = "Close crossed below 20-bar low"
		tc = "EMA 10 < 20 < SMA 40 (Bearish stack)"
	}

	hash := models.GenerateSignalHash(c.Symbol, c.Timeframe, st, c.Timestamp)
	return &models.Signal{
		SignalHash:      hash,
		Symbol:          c.Symbol,
		Exchange:        c.Exchange,
		Timeframe:       c.Timeframe,
		SignalType:      st,
		Category:        models.CategoryPineMomentum,
		Conviction:      models.ConvictionHigh,
		CandleTimestamp: c.Timestamp,
		Price:           c.Close,
		BreakoutReason:  br,
		TrendConfirm:    tc,
		VolumeConfirm:   fmt.Sprintf("Relative volume %.2fx", r.RelativeVolume),
		RSIState:        pineRSIState(r.RSIValue),
		RSIValue:        r.RSIValue,
		ATRValue:        r.ATRValue,
		BodyStrength:    r.BodyStrength,
		RelativeVolume:  r.RelativeVolume,
		CreatedAt:       time.Now(),
	}
}

func pineRSIState(rsi float64) string {
	switch {
	case rsi >= 60:
		return "Bullish momentum"
	case rsi <= 40:
		return "Bearish momentum"
	default:
		return "Neutral"
	}
}

type pineResult struct {
	BuySignal, SellSignal                                       bool
	BullTrend, BearTrend                                        bool
	FreshBullBreak, FreshBearBreak                              bool
	VolumeSpike                                                 bool
	RelativeVolume                                              float64
	StrongBullCandle, StrongBearCandle                          bool
	BullMomentum, BearMomentum                                  bool
	RSIValue, ATRValue, BodyStrength, HighestLevel, LowestLevel float64
}

// ━━━ Helper Functions ━━━

func loadTelegramCredentials(ctx context.Context, cfg *config.Config, ms *store.MongoStore, enc *store.Encryptor, disp *telegram.Dispatcher) {
	log := logger.WithComponent("main")
	if enc == nil {
		loadDefaultTelegramCredentials(cfg, disp, log)
		return
	}

	var settings models.TelegramSettings
	err := ms.Settings().FindOne(ctx, bson.M{"key": "telegram_config"}).Decode(&settings)
	if err != nil {
		log.Info().Msg("No Telegram settings found")
		loadDefaultTelegramCredentials(cfg, disp, log)
		return
	}

	botToken, err := enc.Decrypt(settings.BotTokenEncrypted)
	if err != nil {
		log.Error().Err(err).Msg("Failed to decrypt bot token")
		return
	}
	chatID, err := enc.Decrypt(settings.ChatIDEncrypted)
	if err != nil {
		log.Error().Err(err).Msg("Failed to decrypt chat ID")
		return
	}

	disp.SetCredentials(botToken, chatID)
	log.Info().Msg("Telegram credentials loaded")
}

func loadDefaultTelegramCredentials(cfg *config.Config, disp *telegram.Dispatcher, log zerolog.Logger) {
	if cfg.TelegramBotToken == "" || cfg.TelegramChatID == "" {
		return
	}

	disp.SetCredentials(cfg.TelegramBotToken, cfg.TelegramChatID)
	log.Info().Msg("Default Telegram credentials loaded from environment")
}

func subscribeWatchlistStocks(ctx context.Context, ms *store.MongoStore, wm *broker.WebSocketManager) {
	log := logger.WithComponent("main")

	stocks, err := loadActiveWatchlistStocks(ctx, ms)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load watchlists for subscription")
		return
	}

	tokensByExchange := make(map[int][]string)
	for _, stock := range stocks {
		tokensByExchange[stock.ExchangeType] = append(tokensByExchange[stock.ExchangeType], stock.Token)
	}

	for exchType, tokens := range tokensByExchange {
		if err := wm.Subscribe(exchType, tokens, broker.ModeQuote); err != nil {
			log.Error().Err(err).Int("exchange", exchType).Msg("Subscription failed")
		}
	}

	total := 0
	for _, t := range tokensByExchange {
		total += len(t)
	}
	log.Info().Int("tokens", total).Msg("Watchlist stocks subscribed")
}

func loadActiveWatchlistStocks(ctx context.Context, ms *store.MongoStore) ([]models.Stock, error) {
	cursor, err := ms.Watchlists().Find(ctx, bson.M{"isActive": true})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var watchlists []models.Watchlist
	if err := cursor.All(ctx, &watchlists); err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var stocks []models.Stock
	for _, wl := range watchlists {
		for _, stock := range wl.Stocks {
			key := stock.Exchange + ":" + stock.Token
			if seen[key] {
				continue
			}
			seen[key] = true
			stocks = append(stocks, stock)
		}
	}
	return stocks, nil
}

func recoverCandleAndStrategyState(
	ctx context.Context,
	cfg *config.Config,
	ms *store.MongoStore,
	authPool *broker.AuthPool,
	cs *candle.Store,
	ce *candle.Engine,
	sr *StrategyRunnerImpl,
) {
	log := logger.WithComponent("recovery")

	stocks, err := loadActiveWatchlistStocks(ctx, ms)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load watchlist stocks for recovery")
		return
	}
	if len(stocks) == 0 {
		log.Info().Msg("No active watchlist stocks to recover")
		return
	}

	sr.SetSymbolLabels(stocks)

	if !authPool.HasCredentials() {
		log.Warn().Msg("Angel One credentials not configured; historical candle backfill skipped")
		sr.WarmAndReplayRecent(ctx, stocks, 5*24*time.Hour)
		return
	}

	backfilled := 0
	for _, stock := range stocks {
		// Round-robin: each stock uses the next credential slot
		hc := authPool.NextHistoricalClient()
		n, err := backfillStockCandles(ctx, cfg, hc, cs, ce, stock)
		if err != nil {
			log.Warn().Err(err).Str("symbol", stock.Symbol).Str("token", stock.Token).Msg("Historical backfill failed")
			continue
		}
		backfilled += n
	}

	log.Info().Int("stocks", len(stocks)).Int("apiSlots", authPool.Size()).Int("candles", backfilled).Msg("Recovery backfill completed")
	sr.WarmAndReplayRecent(ctx, stocks, 5*24*time.Hour)
}

func backfillStockCandles(ctx context.Context, cfg *config.Config, hc *broker.HistoricalClient, cs *candle.Store, _ *candle.Engine, stock models.Stock) (int, error) {
	log := logger.WithComponent("recovery")
	total := 0

	// Backfill 1H candles — continue to daily even if hourly fails
	hourlyWarmup := maxInt(cfg.Warmup1H, cfg.Warmup4H*4)
	n, err := backfillStockTimeframe(ctx, hc, cs, stock, models.Timeframe1H, broker.IntervalOneHour, hourlyWarmup)
	if err != nil {
		log.Warn().Err(err).Str("symbol", stock.Symbol).Str("tf", models.Timeframe1H).Msg("Hourly backfill failed, continuing with daily")
	} else {
		total += n
	}

	// Backfill 1D candles — continue to rebuild even if daily fails
	dailyWarmup := maxInt(cfg.Warmup1D, cfg.Warmup1W*5, cfg.Warmup1M*22)
	n, err = backfillStockTimeframe(ctx, hc, cs, stock, models.Timeframe1D, broker.IntervalOneDay, dailyWarmup)
	if err != nil {
		log.Warn().Err(err).Str("symbol", stock.Symbol).Str("tf", models.Timeframe1D).Msg("Daily backfill failed, continuing with higher TF rebuild")
	} else {
		total += n
	}

	// Rebuild 4H from 1H — continue even if this fails
	n, err = rebuildHigherTimeframeHistory(ctx, cs, stock, models.Timeframe1H, models.Timeframe4H, cfg.Warmup4H)
	if err != nil {
		log.Warn().Err(err).Str("symbol", stock.Symbol).Str("tf", models.Timeframe4H).Msg("4H rebuild failed, continuing")
	} else {
		total += n
	}

	// Rebuild 1W from 1D — continue even if this fails
	n, err = rebuildHigherTimeframeHistory(ctx, cs, stock, models.Timeframe1D, models.Timeframe1W, cfg.Warmup1W)
	if err != nil {
		log.Warn().Err(err).Str("symbol", stock.Symbol).Str("tf", models.Timeframe1W).Msg("1W rebuild failed, continuing")
	} else {
		total += n
	}

	// Rebuild 1M from 1D
	n, err = rebuildHigherTimeframeHistory(ctx, cs, stock, models.Timeframe1D, models.Timeframe1M, cfg.Warmup1M)
	if err != nil {
		log.Warn().Err(err).Str("symbol", stock.Symbol).Str("tf", models.Timeframe1M).Msg("1M rebuild failed")
	} else {
		total += n
	}

	return total, nil
}

func backfillStockTimeframe(
	ctx context.Context,
	hc *broker.HistoricalClient,
	cs *candle.Store,
	stock models.Stock,
	timeframe string,
	interval string,
	warmupTarget int,
) (int, error) {
	last, err := cs.GetLastCandle(ctx, stock.Token, timeframe)
	if err != nil {
		return 0, err
	}

	now := time.Now().In(candle.IST)
	from := now.AddDate(0, 0, -7)
	if last != nil {
		from = nextBackfillStart(last.Timestamp, timeframe)
	}

	count, err := cs.CountCandles(ctx, stock.Token, timeframe)
	if err != nil {
		return 0, err
	}
	if warmupTarget > 0 && count < int64(warmupTarget) {
		warmupFrom := warmupStart(now, timeframe, warmupTarget)
		if last == nil || warmupFrom.Before(from) {
			from = warmupFrom
		}
	}
	if !from.Before(now) {
		return 0, nil
	}

	candles, err := hc.FetchCandlesBatch(stock.Exchange, stock.Token, interval, from, now, maxHistoricalDaysPerRequest(timeframe))
	if err != nil {
		return 0, err
	}

	saved := 0
	for _, c := range candles {
		if !isFinalizedHistoricalCandle(c, now) {
			continue
		}
		c.Symbol = stock.Token
		c.Token = stock.Token
		c.Exchange = stock.Exchange
		if err := cs.SaveCandle(ctx, c); err != nil {
			return saved, err
		}
		saved++
	}
	return saved, nil
}

func rebuildHigherTimeframeHistory(ctx context.Context, cs *candle.Store, stock models.Stock, sourceTF, targetTF string, warmupTarget int) (int, error) {
	if warmupTarget <= 0 {
		return 0, nil
	}

	sourceLimit := sourceCandlesForTarget(sourceTF, targetTF, warmupTarget)
	sourceCandles, err := cs.GetRecentCandles(ctx, stock.Token, sourceTF, sourceLimit)
	if err != nil {
		return 0, err
	}
	if len(sourceCandles) == 0 {
		return 0, nil
	}

	rebuilt := candle.AggregateFromHistorical(sourceCandles, stock.Token, stock.Exchange, stock.Token, targetTF)
	now := time.Now().In(candle.IST)
	saved := 0

	// Separate completed vs in-progress candles
	var completed []models.Candle
	for _, c := range rebuilt {
		if isCompletedHigherTimeframeCandle(c, targetTF, now) {
			completed = append(completed, c)
		} else {
			// Save in-progress candle as non-finalized
			c.Finalized = false
			if err := cs.SaveCandle(ctx, c); err != nil {
				return saved, err
			}
			saved++
		}
	}

	// Trim completed candles to warmup target
	if len(completed) > warmupTarget {
		completed = completed[len(completed)-warmupTarget:]
	}

	for _, c := range completed {
		if err := cs.SaveCandle(ctx, c); err != nil {
			return saved, err
		}
		saved++
	}
	return saved, nil
}

func warmupStart(now time.Time, timeframe string, target int) time.Time {
	switch timeframe {
	case models.Timeframe1H:
		days := (target*7)/30 + 21 // roughly 6 market hours/day, plus holiday buffer
		if days < 14 {
			days = 14
		}
		return now.AddDate(0, 0, -days)
	case models.Timeframe1D:
		days := (target*7)/5 + 45 // trading days to calendar days, plus holiday buffer
		if days < 14 {
			days = 14
		}
		return now.AddDate(0, 0, -days)
	default:
		return now.AddDate(0, 0, -7)
	}
}

func maxHistoricalDaysPerRequest(timeframe string) int {
	switch timeframe {
	case models.Timeframe1H:
		return 30
	case models.Timeframe1D:
		return 365
	default:
		return 30
	}
}

func sourceCandlesForTarget(sourceTF, targetTF string, target int) int {
	switch {
	case sourceTF == models.Timeframe1H && targetTF == models.Timeframe4H:
		return target * 4
	case sourceTF == models.Timeframe1D && targetTF == models.Timeframe1W:
		return target * 6
	case sourceTF == models.Timeframe1D && targetTF == models.Timeframe1M:
		return target * 24
	default:
		return target
	}
}

func maxInt(values ...int) int {
	max := 0
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	return max
}

func nextBackfillStart(ts time.Time, timeframe string) time.Time {
	switch timeframe {
	case models.Timeframe1H:
		return ts.Add(time.Hour)
	case models.Timeframe1D:
		return ts.AddDate(0, 0, 1)
	default:
		return ts.Add(time.Nanosecond)
	}
}

func isFinalizedHistoricalCandle(c models.Candle, now time.Time) bool {
	ts := c.Timestamp.In(candle.IST)
	switch c.Timeframe {
	case models.Timeframe1H:
		return ts.Before(candle.Current1HBoundary(now))
	case models.Timeframe1D:
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, candle.IST)
		marketClose := time.Date(now.Year(), now.Month(), now.Day(), 15, 30, 0, 0, candle.IST)
		if now.After(marketClose) {
			return ts.Before(today.AddDate(0, 0, 1))
		}
		return ts.Before(today)
	default:
		return true
	}
}

func isCompletedHigherTimeframeCandle(c models.Candle, timeframe string, now time.Time) bool {
	switch timeframe {
	case models.Timeframe1W:
		return isCompletedWeek(c.Timestamp, now)
	case models.Timeframe1M:
		return isCompletedMonth(c.Timestamp, now)
	default:
		return true
	}
}

func isCompletedWeek(weekStart, now time.Time) bool {
	weekStart = weekStart.In(candle.IST)
	now = now.In(candle.IST)
	currentWeekStart := candle.GetWeekStartExported(now)
	if weekStart.Before(currentWeekStart) {
		return true
	}
	if weekStart.After(currentWeekStart) {
		return false
	}
	return !now.Before(marketCloseOnDate(currentWeekFriday(weekStart)))
}

func isCompletedMonth(monthStart, now time.Time) bool {
	monthStart = monthStart.In(candle.IST)
	now = now.In(candle.IST)
	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, candle.IST)
	candleMonthStart := time.Date(monthStart.Year(), monthStart.Month(), 1, 0, 0, 0, 0, candle.IST)
	if candleMonthStart.Before(currentMonthStart) {
		return true
	}
	if candleMonthStart.After(currentMonthStart) {
		return false
	}
	return !now.Before(marketCloseOnDate(lastTradingDayOfMonth(now)))
}

func currentWeekFriday(weekStart time.Time) time.Time {
	weekStart = weekStart.In(candle.IST)
	return time.Date(weekStart.Year(), weekStart.Month(), weekStart.Day(), 0, 0, 0, 0, candle.IST).AddDate(0, 0, 4)
}

func lastTradingDayOfMonth(t time.Time) time.Time {
	t = t.In(candle.IST)
	nextMonth := time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, candle.IST)
	day := nextMonth.AddDate(0, 0, -1)
	for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		day = day.AddDate(0, 0, -1)
	}
	return day
}

func marketCloseOnDate(t time.Time) time.Time {
	t = t.In(candle.IST)
	return time.Date(t.Year(), t.Month(), t.Day(), 15, 30, 0, 0, candle.IST)
}

func periodicTasks(
	ctx context.Context,
	im *indicator.Manager,
	sr *broker.SymbolResolver,
	ms *store.MongoStore,
	we *scanner.WeeklyEngine,
	pipeline *signalPkg.Pipeline,
	cs *candle.Store,
) {
	log := logger.WithComponent("periodic")

	// Save indicator snapshots every 5 minutes
	snapshotTicker := time.NewTicker(5 * time.Minute)
	// Refresh symbol master and clean up old signals daily
	dailyTicker := time.NewTicker(24 * time.Hour)
	// Run weekly scanner once per market day after close
	weeklyScannerTicker := time.NewTicker(15 * time.Minute)
	lastScannerRunDate := ""

	defer snapshotTicker.Stop()
	defer dailyTicker.Stop()
	defer weeklyScannerTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-snapshotTicker.C:
			if err := im.SaveAllSnapshots(ctx); err != nil {
				log.Error().Err(err).Msg("Periodic snapshot save failed")
			}
		case <-dailyTicker.C:
			if err := sr.Refresh(); err != nil {
				log.Error().Err(err).Msg("Symbol master refresh failed")
			}

			// Clean up 7 days old signals
			cleanupThreshold := time.Now().AddDate(0, 0, -7)
			res, err := ms.Signals().DeleteMany(ctx, bson.M{
				"createdAt": bson.M{"$lt": cleanupThreshold},
			})
			if err != nil {
				log.Error().Err(err).Msg("Failed to clean up old signals")
			} else if res.DeletedCount > 0 {
				log.Info().Int64("deletedCount", res.DeletedCount).Msg("Cleaned up old signals")
			}

			// Clean up older candles
			stocks, err := loadActiveWatchlistStocks(ctx, ms)
			if err == nil && len(stocks) > 0 {
				limits := map[string]int{
					models.Timeframe1H: 1200,
					models.Timeframe4H: 320,
					models.Timeframe1D: 1320,
					models.Timeframe1W: 260,
					models.Timeframe1M: 62,
				}

				var totalPruned int64
				for _, stock := range stocks {
					for tf, limit := range limits {
						pruned, err := cs.PruneOldCandles(ctx, stock.Token, tf, limit)
						if err != nil {
							log.Warn().Err(err).Str("symbol", stock.Symbol).Str("tf", tf).Msg("Failed to prune old candles")
						} else {
							totalPruned += pruned
						}
					}
				}

				if totalPruned > 0 {
					log.Info().Int64("prunedCount", totalPruned).Msg("Cleaned up old candles")
				}
			} else if err != nil {
				log.Error().Err(err).Msg("Failed to load active watchlist stocks for candle cleanup")
			}

		case <-weeklyScannerTicker.C:
			now := time.Now().In(candle.IST)
			if now.Weekday() == time.Saturday || now.Weekday() == time.Sunday {
				continue
			}
			if now.Hour() < 15 || (now.Hour() == 15 && now.Minute() < 35) {
				continue
			}

			runKey := now.Format("2006-01-02")
			if runKey == lastScannerRunDate {
				continue
			}

			if err := runWeeklyScannerCycle(ctx, ms, we, pipeline, cs); err != nil {
				log.Error().Err(err).Msg("Automatic weekly scanner run failed")
				continue
			}
			lastScannerRunDate = runKey
		}
	}
}

func runWeeklyScannerCycle(
	ctx context.Context,
	ms *store.MongoStore,
	we *scanner.WeeklyEngine,
	pipeline *signalPkg.Pipeline,
	cs *candle.Store,
) error {
	log := logger.WithComponent("periodic.scanner")

	stocks, err := loadActiveWatchlistStocks(ctx, ms)
	if err != nil {
		return err
	}
	if len(stocks) == 0 {
		log.Info().Msg("No active stocks for automatic weekly scan")
		return nil
	}

	weeklyByToken := make(map[string][]models.Candle, len(stocks))
	var latestCompletedWeek time.Time
	for _, stock := range stocks {
		weeks, err := cs.GetRecentCandles(ctx, stock.Token, models.Timeframe1W, 260)
		if err != nil {
			log.Error().Err(err).Str("symbol", stock.Symbol).Msg("Failed to load finalized weekly candles for automatic scanner")
			continue
		}
		weeklyByToken[stock.Token] = weeks
		for _, week := range weeks {
			if week.Timestamp.After(latestCompletedWeek) {
				latestCompletedWeek = week.Timestamp
			}
		}
	}

	if latestCompletedWeek.IsZero() {
		log.Info().Int("stocks", len(stocks)).Msg("No finalized weekly candles available for automatic scanner")
		return nil
	}

	scanned := 0
	matches := 0
	alertsEligible := 0
	for _, stock := range stocks {
		for _, week := range weeklyByToken[stock.Token] {
			if scannerRunExists(ctx, ms, stock.Symbol, models.ScannerModeAutomatic, week.Timestamp) {
				continue
			}

			result, err := we.ScanCompletedWeek(ctx, stock.Token, stock.Exchange, week.Timestamp)
			if err != nil {
				log.Error().Err(err).Str("symbol", stock.Symbol).Time("week", week.Timestamp).Msg("Weekly scan failed")
				continue
			}

			alertEligible := sameInstant(week.Timestamp, latestCompletedWeek)
			if alertEligible {
				alertsEligible++
			}
			if result != nil {
				result.Symbol = stock.Symbol
				matches++
				persistWeeklyScannerResult(ctx, ms, pipeline, stock, result, alertEligible)
			}
			if err := persistScannerRun(ctx, ms, stock, week.Timestamp, result, alertEligible); err != nil {
				log.Warn().Err(err).Str("symbol", stock.Symbol).Time("week", week.Timestamp).Msg("Failed to persist scanner run")
			}
			scanned++
		}
	}

	log.Info().
		Int("stocks", len(stocks)).
		Int("weeksScanned", scanned).
		Int("matches", matches).
		Int("alertEligibleWeeks", alertsEligible).
		Time("latestCompletedWeek", latestCompletedWeek).
		Msg("Automatic weekly scan completed")
	return nil
}

func persistWeeklyScannerResult(
	ctx context.Context,
	ms *store.MongoStore,
	pipeline *signalPkg.Pipeline,
	stock models.Stock,
	result *models.ConsolidatedScanResult,
	alertEligible bool,
) {
	log := logger.WithComponent("periodic.scanner")
	current := result.CurrentCandle

	for _, scannerType := range result.MatchedScanners {
		match := models.ScannerMatch{
			Symbol:        stock.Symbol,
			Exchange:      stock.Exchange,
			ScannerType:   scannerType,
			WeekTimestamp: result.WeekTimestamp,
			Matched:       true,
			ScannerMode:   models.ScannerModeAutomatic,
			IsPartialWeek: result.IsPartialWeek,
			ClosePrice:    current.Close,
			Volume:        current.Volume,
			Reason:        scannerReason(scannerType),
			CreatedAt:     time.Now(),
		}

		_, err := ms.ScannerMatches().UpdateOne(ctx,
			bson.M{
				"symbol":        stock.Symbol,
				"scannerType":   scannerType,
				"weekTimestamp": result.WeekTimestamp,
			},
			bson.M{"$set": match},
			options.Update().SetUpsert(true),
		)
		if err != nil {
			log.Warn().Err(err).Str("symbol", stock.Symbol).Str("scanner", scannerType).Msg("Failed to persist scanner match")
		}
	}

	if pipeline == nil || !alertEligible {
		return
	}

	hash := models.GenerateSignalHash(stock.Symbol, models.Timeframe1W, models.SignalBuy, result.WeekTimestamp)
	sig := models.Signal{
		SignalHash:      hash,
		Symbol:          stock.Symbol,
		Exchange:        stock.Exchange,
		Timeframe:       models.Timeframe1W,
		SignalType:      models.SignalBuy,
		Category:        models.CategoryWeeklyConsolidated,
		Conviction:      result.Conviction,
		CandleTimestamp: current.Timestamp,
		Price:           current.Close,
		BreakoutReason:  "Weekly institutional scanner match",
		TrendConfirm:    "Matched weekly scanners: " + strings.Join(result.MatchedScanners, ", "),
		VolumeConfirm:   "Weekly volume " + fmt.Sprintf("%d", current.Volume),
		RSIState:        "Weekly scanner filter passed",
		MatchedScanners: result.MatchedScanners,
		ConvictionScore: result.ConvictionScore,
		CreatedAt:       time.Now(),
	}

	if err := pipeline.Process(ctx, sig); err != nil {
		log.Error().Err(err).Str("symbol", stock.Symbol).Msg("Failed to process weekly scanner signal")
	}
}

func scannerRunExists(ctx context.Context, ms *store.MongoStore, symbol, mode string, weekTimestamp time.Time) bool {
	count, err := ms.ScannerRuns().CountDocuments(ctx, bson.M{
		"symbol":        symbol,
		"scannerMode":   mode,
		"weekTimestamp": weekTimestamp,
	})
	if err != nil {
		log := logger.WithComponent("periodic.scanner")
		log.Warn().Err(err).Str("symbol", symbol).Msg("Failed to check scanner run ledger")
		return false
	}
	return count > 0
}

func persistScannerRun(
	ctx context.Context,
	ms *store.MongoStore,
	stock models.Stock,
	weekTimestamp time.Time,
	result *models.ConsolidatedScanResult,
	alertEligible bool,
) error {
	now := time.Now()
	matchedScanners := []string{}
	if result != nil {
		matchedScanners = result.MatchedScanners
	}

	run := models.ScannerRun{
		Symbol:          stock.Symbol,
		Exchange:        stock.Exchange,
		WeekTimestamp:   weekTimestamp,
		ScannerMode:     models.ScannerModeAutomatic,
		MatchedScanners: matchedScanners,
		AlertEligible:   alertEligible,
		AlertSent:       alertEligible && len(matchedScanners) > 0,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	_, err := ms.ScannerRuns().UpdateOne(ctx,
		bson.M{
			"symbol":        stock.Symbol,
			"scannerMode":   models.ScannerModeAutomatic,
			"weekTimestamp": weekTimestamp,
		},
		bson.M{"$set": run},
		options.Update().SetUpsert(true),
	)
	return err
}

func sameInstant(a, b time.Time) bool {
	return a.UTC().Equal(b.UTC())
}

func scannerReason(scannerType string) string {
	switch scannerType {
	case models.ScannerWeeklyBreakout:
		return "Close broke above prior 52 weekly closes with trend and volume confirmation"
	case models.ScannerWeeklyContinuation:
		return "Weekly continuation with higher low, breakout close, trend, volume, and RSI confirmation"
	case models.ScannerWeekly52WkHigh:
		return "Close broke above prior 52-week high with strong weekly volume"
	case models.ScannerWeeklyPriceAction:
		return "Weekly price-action continuation pattern confirmed"
	default:
		return scannerType
	}
}
