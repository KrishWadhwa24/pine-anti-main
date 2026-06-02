package models

import (
	"time"
)

// Timeframe constants
const (
	Timeframe1H = "1H"
	Timeframe4H = "4H"
	Timeframe1D = "1D"
	Timeframe1W = "1W"
	Timeframe1M = "1M"
)

// Candle represents an OHLCV candlestick for a given symbol and timeframe.
type Candle struct {
	Symbol    string    `bson:"symbol" json:"symbol"`
	Exchange  string    `bson:"exchange" json:"exchange"`
	Token     string    `bson:"token" json:"token"`
	Timeframe string    `bson:"timeframe" json:"timeframe"`
	Timestamp time.Time `bson:"timestamp" json:"timestamp"`
	Open      float64   `bson:"open" json:"open"`
	High      float64   `bson:"high" json:"high"`
	Low       float64   `bson:"low" json:"low"`
	Close     float64   `bson:"close" json:"close"`
	Volume    int64     `bson:"volume" json:"volume"`
	Finalized bool      `bson:"finalized" json:"finalized"`
	Source    string    `bson:"source" json:"source"` // "live", "historical", "reconstructed"
	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
}

// ActiveCandle is an in-memory structure for building candles from ticks.
type ActiveCandle struct {
	Symbol    string
	Exchange  string
	Token     string
	Timeframe string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    int64
	StartTime time.Time
	EndTime   time.Time
	TickCount int64
	LastTick  time.Time
}

// UpdateFromTick updates the active candle with a new tick price and volume.
func (ac *ActiveCandle) UpdateFromTick(price float64, volume int64, tickTime time.Time) {
	if ac.TickCount == 0 {
		ac.Open = price
		ac.High = price
		ac.Low = price
		ac.Close = price
		ac.Volume = volume
		if ac.StartTime.IsZero() {
			ac.StartTime = tickTime
		}
	} else {
		if price > ac.High {
			ac.High = price
		}
		if price < ac.Low {
			ac.Low = price
		}
		ac.Close = price
		ac.Volume = volume // Angel One cumulative daily volume — do NOT sum across ticks
	}
	ac.TickCount++
	ac.LastTick = tickTime
}

// Finalize converts an ActiveCandle into a finalized Candle.
func (ac *ActiveCandle) Finalize() Candle {
	return Candle{
		Symbol:    ac.Symbol,
		Exchange:  ac.Exchange,
		Token:     ac.Token,
		Timeframe: ac.Timeframe,
		Timestamp: ac.StartTime,
		Open:      ac.Open,
		High:      ac.High,
		Low:       ac.Low,
		Close:     ac.Close,
		Volume:    ac.Volume,
		Finalized: true,
		Source:    "live",
		CreatedAt: time.Now(),
	}
}

// CandleKey returns a unique key for this candle's symbol+timeframe.
func CandleKey(symbol, timeframe string) string {
	return symbol + ":" + timeframe
}
