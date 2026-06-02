package candle

import (
	"sort"
	"sync"
	"time"

	"github.com/tradenexus/backend/internal/logger"
	"github.com/tradenexus/backend/internal/models"
)

// 4H candle boundaries for Indian market (Option A):
// Candle 1: 9:15 - 13:15 (4 hours)
// Candle 2: 13:15 - 15:30 (2h15m partial)
var fourHourBoundaries = []struct {
	StartH, StartM int
	EndH, EndM     int
}{
	{9, 15, 13, 15},
	{13, 15, 15, 30},
}

// Aggregator aggregates finalized lower-timeframe candles into higher timeframes.
type Aggregator struct {
	mu sync.Mutex
	// Buffers for building higher-TF candles from component candles
	hourlyBuffer map[string][]models.Candle // symbol -> sorted 1H candles for 4H aggregation
	dailyBuffer  map[string][]models.Candle // symbol -> sorted 1D candles for 1W/1M aggregation
	onFinalize   func(candle models.Candle)
}

// NewAggregator creates a new timeframe aggregator.
func NewAggregator(onFinalize func(models.Candle)) *Aggregator {
	return &Aggregator{
		hourlyBuffer: make(map[string][]models.Candle),
		dailyBuffer:  make(map[string][]models.Candle),
		onFinalize:   onFinalize,
	}
}

// ProcessFinalized1H receives a finalized 1H candle and checks if a 4H candle can be built.
func (a *Aggregator) ProcessFinalized1H(candle models.Candle) {
	log := logger.WithComponent("candle.aggregator")

	a.mu.Lock()
	defer a.mu.Unlock()

	key := candle.Symbol
	a.hourlyBuffer[key] = append(a.hourlyBuffer[key], candle)

	// Sort by timestamp
	sort.Slice(a.hourlyBuffer[key], func(i, j int) bool {
		return a.hourlyBuffer[key][i].Timestamp.Before(a.hourlyBuffer[key][j].Timestamp)
	})

	// Check if we can finalize a 4H candle
	now := time.Now().In(IST)
	day := candle.Timestamp.In(IST)

	for _, boundary := range fourHourBoundaries {
		start := time.Date(day.Year(), day.Month(), day.Day(), boundary.StartH, boundary.StartM, 0, 0, IST)
		end := time.Date(day.Year(), day.Month(), day.Day(), boundary.EndH, boundary.EndM, 0, 0, IST)

		// Only finalize if current time has passed the boundary end
		if !now.After(end) {
			continue
		}

		// Collect all 1H candles that fall within this 4H boundary
		var inBoundary []models.Candle
		for _, c := range a.hourlyBuffer[key] {
			ct := c.Timestamp.In(IST)
			if !ct.Before(start) && ct.Before(end) {
				inBoundary = append(inBoundary, c)
			}
		}

		if len(inBoundary) == 0 {
			continue
		}

		// Check if we already have enough candles for this boundary
		// For 9:15-13:15 boundary: expect ~4 1H candles (9:15, 10:15, 11:15, 12:15)
		// For 13:15-15:30 boundary: expect ~2 1H candles (13:15, 14:15)
		fourHCandle := aggregateCandles(inBoundary, candle.Symbol, candle.Exchange, candle.Token, models.Timeframe4H, start)

		log.Info().
			Str("symbol", fourHCandle.Symbol).
			Time("ts", fourHCandle.Timestamp).
			Float64("close", fourHCandle.Close).
			Msg("4H candle finalized")

		a.onFinalize(fourHCandle)

		// Remove used candles from buffer
		a.hourlyBuffer[key] = removeUsedCandles(a.hourlyBuffer[key], inBoundary)
	}
}

// ProcessFinalizedDaily receives a finalized 1D candle and checks if 1W/1M candles can be built.
func (a *Aggregator) ProcessFinalizedDaily(candle models.Candle) {
	log := logger.WithComponent("candle.aggregator")

	a.mu.Lock()
	defer a.mu.Unlock()

	key := candle.Symbol
	a.dailyBuffer[key] = append(a.dailyBuffer[key], candle)

	sort.Slice(a.dailyBuffer[key], func(i, j int) bool {
		return a.dailyBuffer[key][i].Timestamp.Before(a.dailyBuffer[key][j].Timestamp)
	})

	now := time.Now().In(IST)

	// Check for weekly candle — finalize on Friday after market close, OR any day after
	// if the previous week's candle wasn't finalized (handles restarts, holidays on Friday, etc.)
	currentWeekStart := getWeekStart(now)
	var weekCandles []models.Candle
	var matchedWeekStart time.Time

	if now.Weekday() == time.Friday && isAfterMarketClose(now) {
		// Standard case: finalize the current week on Friday after close
		matchedWeekStart = currentWeekStart
	} else if now.Weekday() == time.Saturday || now.Weekday() == time.Sunday ||
		(now.Weekday() >= time.Monday && now.Weekday() <= time.Thursday) {
		// Check if the previous week's candle is still in the buffer (wasn't finalized on Friday)
		prevWeekStart := currentWeekStart.AddDate(0, 0, -7)
		for _, c := range a.dailyBuffer[key] {
			ct := c.Timestamp.In(IST)
			if !ct.Before(prevWeekStart) && ct.Before(currentWeekStart) {
				weekCandles = append(weekCandles, c)
			}
		}
		if len(weekCandles) > 0 {
			matchedWeekStart = prevWeekStart
		}
	}

	// If no previous-week catch-up, try current week on Friday
	if matchedWeekStart.IsZero() {
		weekCandles = nil
	} else if matchedWeekStart.Equal(currentWeekStart) {
		weekCandles = nil // re-collect for current week
		for _, c := range a.dailyBuffer[key] {
			ct := c.Timestamp.In(IST)
			if !ct.Before(currentWeekStart) && ct.Before(currentWeekStart.AddDate(0, 0, 7)) {
				weekCandles = append(weekCandles, c)
			}
		}
	}

	if len(weekCandles) > 0 {
		wc := aggregateCandles(weekCandles, candle.Symbol, candle.Exchange, candle.Token, models.Timeframe1W, matchedWeekStart)
		log.Info().Str("symbol", wc.Symbol).Time("ts", wc.Timestamp).Msg("1W candle finalized")
		a.onFinalize(wc)
		a.dailyBuffer[key] = removeUsedCandles(a.dailyBuffer[key], weekCandles)
	}

	// Check for monthly candle (finalize on last trading day of month)
	lastTradingDay := getLastTradingDayOfMonth(now)
	if sameDate(now, lastTradingDay) && isAfterMarketClose(now) {
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, IST)
		nextMonth := monthStart.AddDate(0, 1, 0)

		var monthCandles []models.Candle
		for _, c := range a.dailyBuffer[key] {
			ct := c.Timestamp.In(IST)
			if !ct.Before(monthStart) && ct.Before(nextMonth) {
				monthCandles = append(monthCandles, c)
			}
		}

		if len(monthCandles) > 0 {
			mc := aggregateCandles(monthCandles, candle.Symbol, candle.Exchange, candle.Token, models.Timeframe1M, monthStart)
			log.Info().Str("symbol", mc.Symbol).Time("ts", mc.Timestamp).Msg("1M candle finalized")
			a.onFinalize(mc)
			a.dailyBuffer[key] = removeUsedCandles(a.dailyBuffer[key], monthCandles)
		}
	}
}

// AggregateFromHistorical builds higher timeframe candles deterministically from historical data.
// Used during recovery to reconstruct missing 4H/1W/1M candles.
func AggregateFromHistorical(candles []models.Candle, symbol, exchange, token, targetTF string) []models.Candle {
	if len(candles) == 0 {
		return nil
	}

	sort.Slice(candles, func(i, j int) bool {
		return candles[i].Timestamp.Before(candles[j].Timestamp)
	})

	var result []models.Candle

	switch targetTF {
	case models.Timeframe4H:
		// Group 1H candles by 4H boundary
		groups := group1HBy4HBoundary(candles)
		for ts, group := range groups {
			c := aggregateCandles(group, symbol, exchange, token, models.Timeframe4H, ts)
			result = append(result, c)
		}

	case models.Timeframe1W:
		// Group daily candles by week
		groups := groupDailyByWeek(candles)
		for ts, group := range groups {
			c := aggregateCandles(group, symbol, exchange, token, models.Timeframe1W, ts)
			result = append(result, c)
		}

	case models.Timeframe1M:
		// Group daily candles by month
		groups := groupDailyByMonth(candles)
		for ts, group := range groups {
			c := aggregateCandles(group, symbol, exchange, token, models.Timeframe1M, ts)
			result = append(result, c)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.Before(result[j].Timestamp)
	})

	return result
}

// aggregateCandles merges multiple candles into one OHLCV candle.
func aggregateCandles(candles []models.Candle, symbol, exchange, token, tf string, ts time.Time) models.Candle {
	if len(candles) == 0 {
		return models.Candle{}
	}

	agg := models.Candle{
		Symbol:    symbol,
		Exchange:  exchange,
		Token:     token,
		Timeframe: tf,
		Timestamp: ts,
		Open:      candles[0].Open,
		High:      candles[0].High,
		Low:       candles[0].Low,
		Close:     candles[len(candles)-1].Close,
		Volume:    0,
		Finalized: true,
		Source:    "aggregated",
		CreatedAt: time.Now(),
	}

	for _, c := range candles {
		if c.High > agg.High {
			agg.High = c.High
		}
		if c.Low < agg.Low {
			agg.Low = c.Low
		}
		agg.Volume += c.Volume
	}

	return agg
}

func group1HBy4HBoundary(candles []models.Candle) map[time.Time][]models.Candle {
	groups := make(map[time.Time][]models.Candle)
	for _, c := range candles {
		ct := c.Timestamp.In(IST)
		day := time.Date(ct.Year(), ct.Month(), ct.Day(), 0, 0, 0, 0, IST)
		for _, b := range fourHourBoundaries {
			start := day.Add(time.Duration(b.StartH)*time.Hour + time.Duration(b.StartM)*time.Minute)
			end := day.Add(time.Duration(b.EndH)*time.Hour + time.Duration(b.EndM)*time.Minute)
			if !ct.Before(start) && ct.Before(end) {
				groups[start] = append(groups[start], c)
				break
			}
		}
	}
	return groups
}

func groupDailyByWeek(candles []models.Candle) map[time.Time][]models.Candle {
	groups := make(map[time.Time][]models.Candle)
	for _, c := range candles {
		ws := getWeekStart(c.Timestamp.In(IST))
		groups[ws] = append(groups[ws], c)
	}
	return groups
}

func groupDailyByMonth(candles []models.Candle) map[time.Time][]models.Candle {
	groups := make(map[time.Time][]models.Candle)
	for _, c := range candles {
		ct := c.Timestamp.In(IST)
		ms := time.Date(ct.Year(), ct.Month(), 1, 0, 0, 0, 0, IST)
		groups[ms] = append(groups[ms], c)
	}
	return groups
}

func getWeekStart(t time.Time) time.Time {
	t = t.In(IST)
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	monday := t.AddDate(0, 0, -(weekday - 1))
	return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, IST)
}

func getLastTradingDayOfMonth(t time.Time) time.Time {
	t = t.In(IST)
	nextMonth := time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, IST)
	day := nextMonth.AddDate(0, 0, -1)
	for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		day = day.AddDate(0, 0, -1)
	}
	return day
}

func isAfterMarketClose(t time.Time) bool {
	t = t.In(IST)
	closeTime := time.Date(t.Year(), t.Month(), t.Day(), marketCloseHour, marketCloseMin, 0, 0, IST)
	return !t.Before(closeTime)
}

func sameDate(a, b time.Time) bool {
	a = a.In(IST)
	b = b.In(IST)
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}

func removeUsedCandles(all, used []models.Candle) []models.Candle {
	usedSet := make(map[int64]bool)
	for _, u := range used {
		usedSet[u.Timestamp.UnixNano()] = true
	}
	var remaining []models.Candle
	for _, c := range all {
		if !usedSet[c.Timestamp.UnixNano()] {
			remaining = append(remaining, c)
		}
	}
	return remaining
}
