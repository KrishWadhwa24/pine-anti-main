package candle

import (
	"sync"
	"time"

	"github.com/tradenexus/backend/internal/broker"
	"github.com/tradenexus/backend/internal/logger"
	"github.com/tradenexus/backend/internal/models"
)

// IST timezone
var IST = time.FixedZone("IST", 5*3600+30*60)

// Market hours
var (
	marketOpenHour  = 9
	marketOpenMin   = 15
	marketCloseHour = 15
	marketCloseMin  = 30
)

// Builder converts incoming ticks into active candles and finalizes them at timeframe boundaries.
type Builder struct {
	mu            sync.RWMutex
	activeCandles map[string]*models.ActiveCandle // key: symbol:timeframe
	lastBackup    map[string]time.Time
	onFinalize    func(candle models.Candle)
	onBackup      func(key string, candle models.ActiveCandle)
	onDelete      func(key string)
}

// NewBuilder creates a new candle builder.
func NewBuilder(onFinalize func(models.Candle)) *Builder {
	return &Builder{
		activeCandles: make(map[string]*models.ActiveCandle),
		lastBackup:    make(map[string]time.Time),
		onFinalize:    onFinalize,
	}
}

// SetPersistenceCallbacks configures best-effort persistence for active candles.
func (b *Builder) SetPersistenceCallbacks(onBackup func(string, models.ActiveCandle), onDelete func(string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onBackup = onBackup
	b.onDelete = onDelete
}

// ProcessTick updates the active 1H candle for the given tick.
// Only builds 1H candles from ticks — higher timeframes are aggregated separately.
func (b *Builder) ProcessTick(tick *broker.Tick) {
	log := logger.WithComponent("candle.builder")

	now := time.Now().In(IST)
	if !isMarketHours(now) {
		return
	}

	symbol := tick.Token
	exchange := broker.ExchangeName(tick.ExchangeType)
	price := tick.LastTradedPrice

	if price <= 0 {
		return
	}

	// Determine the 1H candle boundary for this tick
	candleStart := get1HBoundary(now)
	key := models.CandleKey(symbol, models.Timeframe1H)

	b.mu.Lock()
	defer b.mu.Unlock()

	ac, exists := b.activeCandles[key]
	var deleteKey string

	// If the active candle belongs to a previous period, finalize it first
	if exists && !ac.StartTime.Equal(candleStart) {
		finalized := ac.Finalize()
		delete(b.activeCandles, key)
		delete(b.lastBackup, key)
		deleteKey = key

		go func() {
			log.Info().
				Str("symbol", finalized.Symbol).
				Str("tf", finalized.Timeframe).
				Time("ts", finalized.Timestamp).
				Float64("close", finalized.Close).
				Msg("1H candle finalized from tick rotation")
			b.onFinalize(finalized)
		}()

		exists = false
	}

	if !exists {
		ac = &models.ActiveCandle{
			Symbol:    symbol,
			Exchange:  exchange,
			Token:     tick.Token,
			Timeframe: models.Timeframe1H,
			StartTime: candleStart,
			EndTime:   candleStart.Add(1 * time.Hour),
		}
		b.activeCandles[key] = ac
	}

	// Use daily volume from quote mode (cumulative)
	var vol int64
	if tick.SubscriptionMode >= broker.ModeQuote {
		vol = tick.VolumeTradedDay
	}
	ac.UpdateFromTick(price, vol, now)

	var backupCopy *models.ActiveCandle
	if b.onBackup != nil {
		last := b.lastBackup[key]
		if last.IsZero() || now.Sub(last) >= 5*time.Second {
			copy := *ac
			backupCopy = &copy
			b.lastBackup[key] = now
		}
	}

	if deleteKey != "" && b.onDelete != nil {
		go b.onDelete(deleteKey)
	}
	if backupCopy != nil {
		go b.onBackup(key, *backupCopy)
	}
}

// RestoreActiveCandles restores active candles from a previous process.
func (b *Builder) RestoreActiveCandles(candles map[string]*models.ActiveCandle) int {
	now := time.Now().In(IST)

	b.mu.Lock()
	defer b.mu.Unlock()

	restored := 0
	for key, ac := range candles {
		if ac == nil || ac.TickCount == 0 || !now.Before(ac.EndTime) {
			continue
		}
		copy := *ac
		b.activeCandles[key] = &copy
		b.lastBackup[key] = now
		restored++
	}
	return restored
}

// CheckFinalizations checks all active candles and finalizes any whose time boundary has passed.
// Called periodically (every 5 seconds) by the candle engine.
func (b *Builder) CheckFinalizations() []models.Candle {
	now := time.Now().In(IST)
	var finalized []models.Candle

	b.mu.Lock()
	defer b.mu.Unlock()

	for key, ac := range b.activeCandles {
		if ac == nil {
			continue
		}
		if now.After(ac.EndTime) && ac.TickCount > 0 {
			c := ac.Finalize()
			finalized = append(finalized, c)
			delete(b.activeCandles, key)
			delete(b.lastBackup, key)
			if b.onDelete != nil {
				go b.onDelete(key)
			}
		}
	}

	return finalized
}

// GetActiveCandle returns a copy of the active candle for a given key.
func (b *Builder) GetActiveCandle(symbol, timeframe string) *models.ActiveCandle {
	b.mu.RLock()
	defer b.mu.RUnlock()
	key := models.CandleKey(symbol, timeframe)
	ac, exists := b.activeCandles[key]
	if !exists || ac == nil {
		return nil
	}
	copy := *ac
	return &copy
}

// GetAllActiveCandles returns copies of all active candles.
func (b *Builder) GetAllActiveCandles() map[string]*models.ActiveCandle {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make(map[string]*models.ActiveCandle, len(b.activeCandles))
	for k, ac := range b.activeCandles {
		if ac != nil {
			copy := *ac
			result[k] = &copy
		}
	}
	return result
}

// get1HBoundary returns the start time of the 1H candle containing the given time.
// Hourly boundaries during market hours: 9:15, 10:15, 11:15, 12:15, 13:15, 14:15.
// Last candle: 14:15-15:30 (75 min, partial).
func get1HBoundary(t time.Time) time.Time {
	t = t.In(IST)
	hour := t.Hour()
	min := t.Minute()

	// Market opens at 9:15
	marketOpen := time.Date(t.Year(), t.Month(), t.Day(), marketOpenHour, marketOpenMin, 0, 0, IST)

	if hour == 9 && min < 15 {
		return marketOpen
	}

	// Calculate which hourly slot: 9:15, 10:15, 11:15, ...
	minutesSinceOpen := (hour-marketOpenHour)*60 + (min - marketOpenMin)
	if minutesSinceOpen < 0 {
		return marketOpen
	}

	slotIndex := minutesSinceOpen / 60
	return marketOpen.Add(time.Duration(slotIndex) * time.Hour)
}

// isMarketHours returns true if the given time is during Indian market hours.
func isMarketHours(t time.Time) bool {
	t = t.In(IST)
	weekday := t.Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		return false
	}

	totalMinutes := t.Hour()*60 + t.Minute()
	openMinutes := marketOpenHour*60 + marketOpenMin
	closeMinutes := marketCloseHour*60 + marketCloseMin

	return totalMinutes >= openMinutes && totalMinutes <= closeMinutes
}

// IsMarketOpen returns whether the market is currently open.
func IsMarketOpen() bool {
	return isMarketHours(time.Now().In(IST))
}

// Current1HBoundary returns the active 1H candle boundary for the given time.
func Current1HBoundary(t time.Time) time.Time {
	return get1HBoundary(t)
}
