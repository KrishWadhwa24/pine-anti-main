package models

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// Signal types
const (
	SignalBuy  = "BUY"
	SignalSell = "SELL"
)

// Signal categories
const (
	CategoryPineMomentum       = "PINE_MOMENTUM"
	CategoryWeeklyBreakout     = "WEEKLY_BREAKOUT"
	CategoryWeeklyContinuation = "WEEKLY_CONTINUATION"
	CategoryWeekly52WkHigh     = "WEEKLY_52WK_HIGH"
	CategoryWeeklyPriceAction  = "WEEKLY_PRICE_ACTION"
	CategoryWeeklyConsolidated = "WEEKLY_CONSOLIDATED"
)

// Conviction levels
const (
	ConvictionModerate = "MODERATE"
	ConvictionHigh     = "HIGH"
	ConvictionVeryHigh = "VERY_HIGH"
	ConvictionMaximum  = "MAXIMUM"
)

// Signal represents a generated trading signal.
type Signal struct {
	ID              string    `bson:"_id,omitempty" json:"id"`
	SignalHash      string    `bson:"signalHash" json:"signalHash"`
	Symbol          string    `bson:"symbol" json:"symbol"`
	Exchange        string    `bson:"exchange" json:"exchange"`
	Timeframe       string    `bson:"timeframe" json:"timeframe"`
	SignalType      string    `bson:"signalType" json:"signalType"` // BUY / SELL
	Category        string    `bson:"category" json:"category"`
	Conviction      string    `bson:"conviction" json:"conviction"`
	CandleTimestamp time.Time `bson:"candleTimestamp" json:"candleTimestamp"`

	// Signal details
	Price          float64 `bson:"price" json:"price"`
	BreakoutReason string  `bson:"breakoutReason" json:"breakoutReason"`
	TrendConfirm   string  `bson:"trendConfirm" json:"trendConfirm"`
	VolumeConfirm  string  `bson:"volumeConfirm" json:"volumeConfirm"`
	RSIState       string  `bson:"rsiState" json:"rsiState"`
	RelativeVolume float64 `bson:"relativeVolume" json:"relativeVolume"`
	RSIValue       float64 `bson:"rsiValue" json:"rsiValue"`
	ATRValue       float64 `bson:"atrValue" json:"atrValue"`
	BodyStrength   float64 `bson:"bodyStrength" json:"bodyStrength"`

	// Weekly scanner specific
	MatchedScanners []string `bson:"matchedScanners,omitempty" json:"matchedScanners,omitempty"`
	ConvictionScore int      `bson:"convictionScore,omitempty" json:"convictionScore,omitempty"`

	// Dispatch state
	TelegramSent   bool      `bson:"telegramSent" json:"telegramSent"`
	TelegramSentAt time.Time `bson:"telegramSentAt,omitempty" json:"telegramSentAt,omitempty"`
	CreatedAt      time.Time `bson:"createdAt" json:"createdAt"`
}

// GenerateSignalHash creates a unique hash for deduplication.
// Hash = SHA256(symbol:timeframe:signalType:candleTimestampUTC)
func GenerateSignalHash(symbol, timeframe, signalType string, candleTimestamp time.Time) string {
	raw := fmt.Sprintf("%s:%s:%s:%d", symbol, timeframe, signalType, candleTimestamp.UTC().Unix())
	hash := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", hash)
}

// TimeframeLabel returns a human-readable label for the timeframe role.
func TimeframeLabel(tf string) string {
	switch tf {
	case Timeframe4H:
		return "Early Momentum"
	case Timeframe1D:
		return "Swing Confirmation"
	case Timeframe1W:
		return "Institutional Trend"
	case Timeframe1M:
		return "Macro Trend"
	default:
		return tf
	}
}
