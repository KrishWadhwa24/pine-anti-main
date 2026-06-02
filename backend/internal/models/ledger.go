package models

import "time"

// ProcessingLedger tracks the last processed state per symbol+timeframe.
type ProcessingLedger struct {
	Symbol    string `bson:"symbol" json:"symbol"`
	Timeframe string `bson:"timeframe" json:"timeframe"`

	LastFinalizedCandleTS   time.Time `bson:"lastFinalizedCandleTs" json:"lastFinalizedCandleTs"`
	LastStrategyExecutionTS time.Time `bson:"lastStrategyExecutionTs" json:"lastStrategyExecutionTs"`
	LastSignalGenerationTS  time.Time `bson:"lastSignalGenerationTs" json:"lastSignalGenerationTs"`
	LastTelegramDispatchTS  time.Time `bson:"lastTelegramDispatchTs" json:"lastTelegramDispatchTs"`
	LastRecoveryTS          time.Time `bson:"lastRecoveryTs" json:"lastRecoveryTs"`

	CandleIntegrityOK bool      `bson:"candleIntegrityOk" json:"candleIntegrityOk"`
	UpdatedAt         time.Time `bson:"updatedAt" json:"updatedAt"`
}

// RecoveryCheckpoint stores system-level recovery state.
type RecoveryCheckpoint struct {
	CheckpointType string    `bson:"checkpointType" json:"checkpointType"`
	LastTimestamp   time.Time `bson:"lastTimestamp" json:"lastTimestamp"`
	Metadata       map[string]interface{} `bson:"metadata,omitempty" json:"metadata,omitempty"`
	UpdatedAt      time.Time `bson:"updatedAt" json:"updatedAt"`
}

// Checkpoint types
const (
	CheckpointWSSession      = "ws_session"
	CheckpointCandleEngine   = "candle_engine"
	CheckpointStrategyEngine = "strategy_engine"
	CheckpointWeeklyScanner  = "weekly_scanner"
	CheckpointTelegramQueue  = "telegram_queue"
)
