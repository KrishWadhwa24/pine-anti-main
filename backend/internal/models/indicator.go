package models

import "time"

// IndicatorSnapshot stores the persistent state of all indicators for a symbol+timeframe.
type IndicatorSnapshot struct {
	Symbol    string    `bson:"symbol" json:"symbol"`
	Timeframe string    `bson:"timeframe" json:"timeframe"`
	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt"`

	// EMA states
	EMA10 float64 `bson:"ema10" json:"ema10"`
	EMA20 float64 `bson:"ema20" json:"ema20"`
	EMA50 float64 `bson:"ema50" json:"ema50"`

	// SMA states
	SMA40       float64   `bson:"sma40" json:"sma40"`
	PrevSMA40   float64   `bson:"prevSma40" json:"prevSma40"`
	SMA40Buffer []float64 `bson:"sma40Buffer" json:"sma40Buffer"`

	// RSI state (Wilder's smoothing)
	RSIAvgGain   float64 `bson:"rsiAvgGain" json:"rsiAvgGain"`
	RSIAvgLoss   float64 `bson:"rsiAvgLoss" json:"rsiAvgLoss"`
	RSIValue     float64 `bson:"rsiValue" json:"rsiValue"`
	RSIPrevClose float64 `bson:"rsiPrevClose" json:"rsiPrevClose"`

	// ATR state
	ATRValue     float64 `bson:"atrValue" json:"atrValue"`
	ATRPrevClose float64 `bson:"atrPrevClose" json:"atrPrevClose"`

	// Volume analysis
	AvgVolume    float64   `bson:"avgVolume" json:"avgVolume"`
	VolumeBuffer []float64 `bson:"volumeBuffer" json:"volumeBuffer"`

	// Breakout detection
	HighBuffer  []float64 `bson:"highBuffer" json:"highBuffer"`
	LowBuffer   []float64 `bson:"lowBuffer" json:"lowBuffer"`
	PrevHighest float64   `bson:"prevHighest" json:"prevHighest"`
	PrevLowest  float64   `bson:"prevLowest" json:"prevLowest"`

	// Weekly scanner EMA states (used for weekly candles)
	EMA200   float64 `bson:"ema200,omitempty" json:"ema200,omitempty"`
	EMAVol20 float64 `bson:"emaVol20,omitempty" json:"emaVol20,omitempty"`

	// Signal state
	LongActive  bool `bson:"longActive" json:"longActive"`
	ShortActive bool `bson:"shortActive" json:"shortActive"`
	LastBuyBar  int  `bson:"lastBuyBar" json:"lastBuyBar"`
	LastSellBar int  `bson:"lastSellBar" json:"lastSellBar"`
	BarIndex    int  `bson:"barIndex" json:"barIndex"`

	// EMA crossover detection
	PrevEMA10           float64   `bson:"prevEma10" json:"prevEma10"`
	PrevEMA20           float64   `bson:"prevEma20" json:"prevEma20"`
	PrevClose           float64   `bson:"prevClose" json:"prevClose"`
	LastCandleTimestamp time.Time `bson:"lastCandleTimestamp,omitempty" json:"lastCandleTimestamp,omitempty"`

	// Warmup tracking
	WarmupCount int  `bson:"warmupCount" json:"warmupCount"`
	IsWarmedUp  bool `bson:"isWarmedUp" json:"isWarmedUp"`
}

// IndicatorKey returns a unique key for this snapshot.
func IndicatorKey(symbol, timeframe string) string {
	return symbol + ":" + timeframe
}
