package models

import "time"

// ScannerMatch represents a weekly scanner result for a stock.
type ScannerMatch struct {
	ID            string    `bson:"_id,omitempty" json:"id"`
	Symbol        string    `bson:"symbol" json:"symbol"`
	Exchange      string    `bson:"exchange" json:"exchange"`
	ScannerType   string    `bson:"scannerType" json:"scannerType"`
	WeekTimestamp time.Time `bson:"weekTimestamp" json:"weekTimestamp"`
	Matched       bool      `bson:"matched" json:"matched"`
	ScannerMode   string    `bson:"scannerMode,omitempty" json:"scannerMode,omitempty"`
	IsPartialWeek bool      `bson:"isPartialWeek,omitempty" json:"isPartialWeek,omitempty"`

	// Scanner details
	ClosePrice float64 `bson:"closePrice" json:"closePrice"`
	Volume     int64   `bson:"volume" json:"volume"`
	RSI        float64 `bson:"rsi" json:"rsi"`
	Reason     string  `bson:"reason" json:"reason"`

	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
}

// ScannerRun records that a stock/week was scanned, even when no scanner matched.
type ScannerRun struct {
	ID              string    `bson:"_id,omitempty" json:"id"`
	Symbol          string    `bson:"symbol" json:"symbol"`
	Exchange        string    `bson:"exchange" json:"exchange"`
	WeekTimestamp   time.Time `bson:"weekTimestamp" json:"weekTimestamp"`
	ScannerMode     string    `bson:"scannerMode" json:"scannerMode"`
	MatchedScanners []string  `bson:"matchedScanners" json:"matchedScanners"`
	AlertEligible   bool      `bson:"alertEligible" json:"alertEligible"`
	AlertSent       bool      `bson:"alertSent" json:"alertSent"`
	CreatedAt       time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt       time.Time `bson:"updatedAt" json:"updatedAt"`
}

// ConsolidatedScanResult holds the combined result of all weekly scanners for a stock.
type ConsolidatedScanResult struct {
	Symbol          string    `bson:"symbol" json:"symbol"`
	Exchange        string    `bson:"exchange" json:"exchange"`
	MatchedScanners []string  `bson:"matchedScanners" json:"matchedScanners"`
	ConvictionScore int       `bson:"convictionScore" json:"convictionScore"`
	Conviction      string    `bson:"conviction" json:"conviction"`
	WeekTimestamp   time.Time `bson:"weekTimestamp" json:"weekTimestamp"`
	CurrentCandle   Candle    `bson:"currentCandle" json:"currentCandle"`
	IsPartialWeek   bool      `bson:"isPartialWeek" json:"isPartialWeek"`
}

const (
	ScannerModeAutomatic = "automatic"
	ScannerModeManual    = "manual"
)

// Scanner type constants
const (
	ScannerWeeklyBreakout     = "WEEKLY_BREAKOUT"
	ScannerWeeklyContinuation = "WEEKLY_CONTINUATION"
	ScannerWeekly52WkHigh     = "WEEKLY_52WK_HIGH"
	ScannerWeeklyPriceAction  = "WEEKLY_PRICE_ACTION"
)
