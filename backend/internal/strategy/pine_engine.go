package strategy

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/tradenexus/backend/internal/indicator"
	"github.com/tradenexus/backend/internal/logger"
	"github.com/tradenexus/backend/internal/models"
)

// PineEngine implements the "Chase Momentum Pro Clean" strategy in Go.
// This is an exact conversion of the Pine Script v6 indicator logic.
type PineEngine struct {
	breakoutLookback int
	volumeMultiplier float64
	cooldownBars     int
}

// NewPineEngine creates the strategy engine with configurable parameters.
func NewPineEngine(breakoutLookback int, volumeMultiplier float64, cooldownBars int) *PineEngine {
	return &PineEngine{
		breakoutLookback: breakoutLookback,
		volumeMultiplier: volumeMultiplier,
		cooldownBars:     cooldownBars,
	}
}

// PineSignalResult contains the output of a strategy evaluation.
type PineSignalResult struct {
	BuySignal  bool
	SellSignal bool

	// Context for Telegram alert
	BullTrend        bool
	BearTrend        bool
	FreshBullBreak   bool
	FreshBearBreak   bool
	VolumeSpike      bool
	RelativeVolume   float64
	StrongBullCandle bool
	StrongBearCandle bool
	BullMomentum     bool
	BearMomentum     bool
	RSIValue         float64
	ATRValue         float64
	BodyStrength     float64 // bodySize / atr
	HighestLevel     float64
	LowestLevel      float64
}

// Evaluate runs the Pine strategy on a finalized candle with current indicator state.
// ONLY call this on CandleFinalized events for 4H, 1D, 1W, 1M timeframes.
func (p *PineEngine) Evaluate(ctx context.Context, candle models.Candle, state *indicator.State) *PineSignalResult {
	log := logger.WithComponent("strategy.pine")

	// Ensure indicators are warmed up
	if !state.IsReady() {
		log.Debug().
			Str("symbol", candle.Symbol).
			Str("tf", candle.Timeframe).
			Int("warmup", state.WarmupCount).
			Msg("Indicators not warmed up, skipping strategy evaluation")
		return nil
	}

	result := &PineSignalResult{}

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// MOVING AVERAGES (already updated by indicator manager)
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

	ema10 := state.EMA10.Value
	ema20 := state.EMA20.Value
	sma40 := state.SMA40.Value
	// ema50 used for display only, not in signal logic

	prevSma40 := state.PrevSMA40

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// TREND CONDITIONS
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

	result.BullTrend = ema10 > ema20 &&
		ema20 > sma40 &&
		candle.Close > ema10 &&
		sma40 > prevSma40

	result.BearTrend = ema10 < ema20 &&
		ema20 < sma40 &&
		candle.Close < ema10 &&
		sma40 < prevSma40

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// BREAKOUT LEVELS
	// highestLevel = ta.highest(high, breakoutLength)[1]
	// lowestLevel = ta.lowest(low, breakoutLength)[1]
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

	result.HighestLevel = state.Breakout20.PrevHighest
	result.LowestLevel = state.Breakout20.PrevLowest

	// freshBullBreakout = ta.crossover(close, highestLevel)
	result.FreshBullBreak = indicator.FreshBullBreakout(candle.Close, state.PrevClose, result.HighestLevel)

	// freshBearBreakout = ta.crossunder(close, lowestLevel)
	result.FreshBearBreak = indicator.FreshBearBreakout(candle.Close, state.PrevClose, result.LowestLevel)

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// VOLUME ANALYSIS
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

	avgVolume := state.VolSMA20.Value
	if avgVolume > 0 {
		result.RelativeVolume = float64(candle.Volume) / avgVolume
	}
	result.VolumeSpike = float64(candle.Volume) > avgVolume*p.volumeMultiplier

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// STRONG CANDLE DETECTION
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

	bodySize := math.Abs(candle.Close - candle.Open)
	atrValue := state.ATR14.Value
	result.ATRValue = atrValue

	if atrValue > 0 {
		result.BodyStrength = bodySize / atrValue
	}

	result.StrongBullCandle = candle.Close > candle.Open && bodySize > atrValue*0.5
	result.StrongBearCandle = candle.Close < candle.Open && bodySize > atrValue*0.5

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// RSI MOMENTUM
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

	rsi := state.RSI14.Value
	result.RSIValue = rsi
	result.BullMomentum = rsi > 60
	result.BearMomentum = rsi < 40

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// SIGNAL STATE MANAGEMENT
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

	// Reset conditions
	resetLong := candle.Close < ema10 ||
		indicator.Crossunder(ema10, ema20, state.PrevEMA10, state.PrevEMA20)

	resetShort := candle.Close > ema10 ||
		indicator.Crossover(ema10, ema20, state.PrevEMA10, state.PrevEMA20)

	if resetLong {
		state.LongActive = false
	}
	if resetShort {
		state.ShortActive = false
	}

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// COOLDOWN
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

	canBuy := (state.BarIndex - state.LastBuyBar) > p.cooldownBars
	canSell := (state.BarIndex - state.LastSellBar) > p.cooldownBars

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// FINAL BUY/SELL SIGNALS
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

	result.BuySignal = result.BullTrend &&
		result.FreshBullBreak &&
		result.VolumeSpike &&
		result.StrongBullCandle &&
		result.BullMomentum &&
		!state.LongActive &&
		canBuy

	result.SellSignal = result.BearTrend &&
		result.FreshBearBreak &&
		result.VolumeSpike &&
		result.StrongBearCandle &&
		result.BearMomentum &&
		!state.ShortActive &&
		canSell

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// UPDATE STATE
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

	if result.BuySignal {
		state.LongActive = true
		state.ShortActive = false
		state.LastBuyBar = state.BarIndex

		log.Info().
			Str("symbol", candle.Symbol).
			Str("tf", candle.Timeframe).
			Float64("close", candle.Close).
			Float64("rsi", rsi).
			Float64("relVol", result.RelativeVolume).
			Msg("🟢 BUY SIGNAL generated")
	}

	if result.SellSignal {
		state.ShortActive = true
		state.LongActive = false
		state.LastSellBar = state.BarIndex

		log.Info().
			Str("symbol", candle.Symbol).
			Str("tf", candle.Timeframe).
			Float64("close", candle.Close).
			Float64("rsi", rsi).
			Float64("relVol", result.RelativeVolume).
			Msg("🔴 SELL SIGNAL generated")
	}

	return result
}

// BuildSignal constructs a Signal model from a PineSignalResult.
func (p *PineEngine) BuildSignal(candle models.Candle, result *PineSignalResult) *models.Signal {
	if !result.BuySignal && !result.SellSignal {
		return nil
	}

	signalType := models.SignalBuy
	breakoutReason := "Close crossed above 20-bar high"
	trendConfirm := "EMA 10 > 20 > SMA 40 (Bullish stack)"
	if result.SellSignal {
		signalType = models.SignalSell
		breakoutReason = "Close crossed below 20-bar low"
		trendConfirm = "EMA 10 < 20 < SMA 40 (Bearish stack)"
	}

	volumeConfirm := "Normal"
	if result.VolumeSpike {
		volumeConfirm = fmt.Sprintf("%.1fx above average (Spike)", result.RelativeVolume)
	}

	rsiState := "Neutral"
	if result.BullMomentum {
		rsiState = "Bullish (>60)"
	} else if result.BearMomentum {
		rsiState = "Bearish (<40)"
	}

	hash := models.GenerateSignalHash(candle.Symbol, candle.Timeframe, signalType, candle.Timestamp)

	return &models.Signal{
		SignalHash:      hash,
		Symbol:          candle.Symbol,
		Exchange:        candle.Exchange,
		Timeframe:       candle.Timeframe,
		SignalType:      signalType,
		Category:        models.CategoryPineMomentum,
		Conviction:      models.ConvictionHigh,
		CandleTimestamp: candle.Timestamp,
		Price:           candle.Close,
		BreakoutReason:  breakoutReason,
		TrendConfirm:    trendConfirm,
		VolumeConfirm:   volumeConfirm,
		RSIState:        rsiState,
		RelativeVolume:  result.RelativeVolume,
		RSIValue:        result.RSIValue,
		ATRValue:        result.ATRValue,
		BodyStrength:    result.BodyStrength,
		CreatedAt:       time.Now(),
	}
}
