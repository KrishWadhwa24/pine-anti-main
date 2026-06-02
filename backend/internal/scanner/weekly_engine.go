package scanner

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"github.com/tradenexus/backend/internal/candle"
	"github.com/tradenexus/backend/internal/logger"
	"github.com/tradenexus/backend/internal/models"
)

// WeeklyEngine coordinates all 4 weekly institutional scanners.
type WeeklyEngine struct {
	candleStore *candle.Store
}

// NewWeeklyEngine creates a new weekly scanner engine.
func NewWeeklyEngine(candleStore *candle.Store) *WeeklyEngine {
	return &WeeklyEngine{candleStore: candleStore}
}

// ScanStock runs all 4 scanners against a stock's weekly candle history.
// Returns a consolidated result with conviction scoring.
func (we *WeeklyEngine) ScanStock(ctx context.Context, symbol, exchange string) (*models.ConsolidatedScanResult, error) {
	log := logger.WithComponent("scanner.weekly")

	candles, err := we.loadWeeklyCandlesForScan(ctx, symbol, exchange)
	if err != nil {
		return nil, err
	}

	return scanWeeklyCandles(log, symbol, exchange, candles, time.Now().In(candle.IST))
}

// ScanCompletedWeek runs scanners against a specific finalized weekly candle.
func (we *WeeklyEngine) ScanCompletedWeek(ctx context.Context, symbol, exchange string, weekTimestamp time.Time) (*models.ConsolidatedScanResult, error) {
	log := logger.WithComponent("scanner.weekly")

	candles, err := we.candleStore.GetRecentCandles(ctx, symbol, models.Timeframe1W, 260)
	if err != nil {
		return nil, err
	}

	weekTimestamp = weekTimestamp.In(candle.IST)
	var throughWeek []models.Candle
	for _, c := range candles {
		if !c.Timestamp.In(candle.IST).After(weekTimestamp) {
			throughWeek = append(throughWeek, c)
		}
	}

	return scanWeeklyCandles(log, symbol, exchange, throughWeek, time.Now().In(candle.IST))
}

func scanWeeklyCandles(log zerolog.Logger, symbol, exchange string, candles []models.Candle, now time.Time) (*models.ConsolidatedScanResult, error) {
	if len(candles) < 10 {
		log.Debug().Str("symbol", symbol).Int("candles", len(candles)).Msg("Insufficient weekly candles for scanner")
		return nil, nil
	}

	current := candles[len(candles)-1]
	prev := candles[len(candles)-2]

	// Build lookback slices
	var closes, highs, lows []float64
	var volumes []int64
	for _, c := range candles {
		closes = append(closes, c.Close)
		highs = append(highs, c.High)
		lows = append(lows, c.Low)
		volumes = append(volumes, c.Volume)
	}

	// Calculate weekly EMAs from weekly candles, matching Chartink weekly(ema(...)).
	ema20, ema50, ema200, emaVol20 := calcWeeklyEMAs(candles)

	// Calculate RSI
	rsi := calcWeeklyRSI(closes)

	weekTs := current.Timestamp

	var matchedScanners []string

	if len(candles) >= 200 {
		// Scanner 1: Weekly Breakout
		if scanWeeklyBreakout(current, closes, ema20, ema50, ema200, emaVol20, rsi) {
			matchedScanners = append(matchedScanners, models.ScannerWeeklyBreakout)
			log.Info().Str("symbol", symbol).Msg("Scanner 1: Weekly Breakout matched")
		}

		// Scanner 2: Weekly Continuation
		if scanWeeklyContinuation(current, prev, ema20, ema50, ema200, rsi) {
			matchedScanners = append(matchedScanners, models.ScannerWeeklyContinuation)
			log.Info().Str("symbol", symbol).Msg("Scanner 2: Weekly Continuation matched")
		}
	}

	// Scanner 3: 52-Week High Breakout
	if scanWeekly52WkHigh(current, prev, highs, lows, volumes, rsi) {
		matchedScanners = append(matchedScanners, models.ScannerWeekly52WkHigh)
		log.Info().Str("symbol", symbol).Msg("Scanner 3: 52-Week High Breakout matched")
	}

	// Scanner 4: Price Action Continuation
	if len(candles) >= 5 {
		prev4 := candles[len(candles)-5]
		prev2 := candles[len(candles)-3]
		if scanWeeklyPriceAction(current, prev, prev2, prev4, rsi) {
			matchedScanners = append(matchedScanners, models.ScannerWeeklyPriceAction)
			log.Info().Str("symbol", symbol).Msg("Scanner 4: Price Action Continuation matched")
		}
	}

	if len(matchedScanners) == 0 {
		// Normal case: the stock simply did not match any weekly scanner.
		return nil, nil
	}

	// Conviction scoring
	score := len(matchedScanners)
	conviction := models.ConvictionModerate
	switch score {
	case 2:
		conviction = models.ConvictionHigh
	case 3:
		conviction = models.ConvictionVeryHigh
	case 4:
		conviction = models.ConvictionMaximum
	}

	return &models.ConsolidatedScanResult{
		Symbol:          symbol,
		Exchange:        exchange,
		MatchedScanners: matchedScanners,
		ConvictionScore: score,
		Conviction:      conviction,
		WeekTimestamp:   weekTs,
		CurrentCandle:   current,
		IsPartialWeek:   isCurrentWeekPartial(current.Timestamp, now),
	}, nil
}

func isCurrentWeekPartial(weekStart, now time.Time) bool {
	weekStart = weekStart.In(candle.IST)
	now = now.In(candle.IST)
	currentWeekStart := candle.GetWeekStartExported(now)
	if !weekStart.Equal(currentWeekStart) {
		return false
	}
	fridayClose := currentWeekStart.AddDate(0, 0, 4)
	fridayClose = time.Date(fridayClose.Year(), fridayClose.Month(), fridayClose.Day(), 15, 30, 0, 0, candle.IST)
	return now.Before(fridayClose)
}

func (we *WeeklyEngine) loadWeeklyCandlesForScan(ctx context.Context, symbol, exchange string) ([]models.Candle, error) {
	// Manual scans should behave like Chartink's latest weekly candle: rebuild
	// weekly candles from finalized daily candles so the current week can be
	// scanned before the stored Friday weekly candle exists.
	dailyCandles, err := we.candleStore.GetRecentCandles(ctx, symbol, models.Timeframe1D, 1200)
	if err != nil {
		return nil, err
	}

	if len(dailyCandles) > 0 {
		weeklyCandles := candle.AggregateFromHistorical(dailyCandles, symbol, exchange, symbol, models.Timeframe1W)
		if len(weeklyCandles) > 0 {
			if len(weeklyCandles) > 220 {
				weeklyCandles = weeklyCandles[len(weeklyCandles)-220:]
			}
			return weeklyCandles, nil
		}
	}

	// Fallback for databases that already have weekly candles but not enough
	// daily history loaded yet.
	return we.candleStore.GetRecentCandles(ctx, symbol, models.Timeframe1W, 220)
}

// ━━━━ Scanner 1: Weekly Breakout ━━━━
func scanWeeklyBreakout(current models.Candle, closes []float64, ema20, ema50, ema200, emaVol20, rsi float64) bool {
	if len(closes) < 53 {
		return false
	}
	// close > max(close[1..52])
	max52 := maxSlice(closes[:len(closes)-1], 52)
	if current.Close <= max52 {
		return false
	}
	// volume > ema(20, volume)
	if float64(current.Volume) <= emaVol20 {
		return false
	}
	// close > ema(20)
	if current.Close <= ema20 {
		return false
	}
	// ema(20) > ema(50) > ema(200)
	if !(ema20 > ema50 && ema50 > ema200) {
		return false
	}
	// rsi between 50-75
	if rsi <= 50 || rsi >= 75 {
		return false
	}
	// close >= open (green candle)
	if current.Close < current.Open {
		return false
	}
	return true
}

// ━━━━ Scanner 2: Weekly Continuation ━━━━
func scanWeeklyContinuation(current, prev models.Candle, ema20, ema50, ema200, rsi float64) bool {
	// close > last week's close
	if current.Close <= prev.Close {
		return false
	}
	// close > ema(20) and ema stack
	if current.Close <= ema20 || !(ema20 > ema50 && ema50 > ema200) {
		return false
	}
	// low >= last week's low (higher low)
	if current.Low < prev.Low {
		return false
	}
	// close > last week's high (inside bar breakout)
	if current.Close <= prev.High {
		return false
	}
	// volume >= last week's volume
	if current.Volume < prev.Volume {
		return false
	}
	// rsi between 50-70
	if rsi <= 50 || rsi >= 70 {
		return false
	}
	return true
}

// ━━━━ Scanner 3: 52-Week High Breakout ━━━━
func scanWeekly52WkHigh(current, prev models.Candle, highs, lows []float64, volumes []int64, rsi float64) bool {
	if len(highs) < 53 {
		return false
	}
	// close > max(high[1..52])
	max52High := maxSlice(highs[:len(highs)-1], 52)
	if current.Close <= max52High {
		return false
	}
	// volume > max(volume[1..4])
	if len(volumes) >= 5 {
		max4Vol := maxInt64Slice(volumes[len(volumes)-5:len(volumes)-1], 4)
		if current.Volume <= max4Vol {
			return false
		}
	}
	// close >= open
	if current.Close < current.Open {
		return false
	}
	// high > high[1]
	if current.High <= prev.High {
		return false
	}
	// low > low[4]
	if len(lows) >= 5 {
		if current.Low <= lows[len(lows)-5] {
			return false
		}
	}
	// rsi 50-75
	if rsi <= 50 || rsi >= 75 {
		return false
	}
	return true
}

// ━━━━ Scanner 4: Price Action Continuation ━━━━
func scanWeeklyPriceAction(current, prev, prev2, prev4 models.Candle, rsi float64) bool {
	// close > high[1]
	if current.Close <= prev.High {
		return false
	}
	// low >= low[1]
	if current.Low < prev.Low {
		return false
	}
	// high > high[1]
	if current.High <= prev.High {
		return false
	}
	// low > low[4]
	if current.Low <= prev4.Low {
		return false
	}
	// close >= open
	if current.Close < current.Open {
		return false
	}
	// volume >= volume[1]
	if current.Volume < prev.Volume {
		return false
	}
	// rsi 50-70
	if rsi <= 50 || rsi >= 70 {
		return false
	}
	// close > close[1] and close[1] > close[2]
	if current.Close <= prev.Close || prev.Close <= prev2.Close {
		return false
	}
	return true
}

// ━━━━ Helper functions ━━━━

func maxSlice(values []float64, n int) float64 {
	start := len(values) - n
	if start < 0 {
		start = 0
	}
	max := values[start]
	for i := start + 1; i < len(values); i++ {
		if values[i] > max {
			max = values[i]
		}
	}
	return max
}

func maxInt64Slice(values []int64, n int) int64 {
	start := len(values) - n
	if start < 0 {
		start = 0
	}
	max := values[start]
	for i := start + 1; i < len(values); i++ {
		if values[i] > max {
			max = values[i]
		}
	}
	return max
}

func calcWeeklyEMAs(weeklyCandles []models.Candle) (ema20, ema50, ema200, emaVol20 float64) {
	e20 := NewSimpleEMA(20)
	e50 := NewSimpleEMA(50)
	e200 := NewSimpleEMA(200)
	ev20 := NewSimpleEMA(20)

	for _, c := range weeklyCandles {
		e20.Update(c.Close)
		e50.Update(c.Close)
		e200.Update(c.Close)
		ev20.Update(float64(c.Volume))
	}
	return e20.Value, e50.Value, e200.Value, ev20.Value
}

func calcWeeklyRSI(closes []float64) float64 {
	if len(closes) < 15 {
		return 50
	}
	period := 14
	var gains, losses float64

	for i := 1; i <= period; i++ {
		change := closes[i] - closes[i-1]
		if change > 0 {
			gains += change
		} else {
			losses -= change
		}
	}
	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	// Continue with Wilder's smoothing through the latest weekly close.
	for i := period + 1; i < len(closes); i++ {
		change := closes[i] - closes[i-1]
		gain := 0.0
		loss := 0.0
		if change > 0 {
			gain = change
		} else {
			loss = -change
		}
		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
	}

	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

// SimpleEMA is a standalone EMA for scanner calculations.
type SimpleEMA struct {
	Period     int
	Multiplier float64
	Value      float64
	count      int
	sum        float64
	ready      bool
}

func NewSimpleEMA(period int) *SimpleEMA {
	return &SimpleEMA{
		Period:     period,
		Multiplier: 2.0 / float64(period+1),
	}
}

func (e *SimpleEMA) Update(value float64) float64 {
	e.count++
	if !e.ready {
		e.sum += value
		if e.count == e.Period {
			e.Value = e.sum / float64(e.Period)
			e.ready = true
		}
		return e.Value
	}
	e.Value = (value-e.Value)*e.Multiplier + e.Value
	return e.Value
}
