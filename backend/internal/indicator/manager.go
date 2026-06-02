package indicator

import (
	"context"
	"sync"
	"time"

	"github.com/tradenexus/backend/internal/logger"
	"github.com/tradenexus/backend/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// State holds all rolling indicator instances for a single symbol+timeframe.
type State struct {
	Symbol    string
	Timeframe string

	// Pine strategy indicators
	EMA10      *EMA
	EMA20      *EMA
	SMA40      *SMA
	EMA50      *EMA
	RSI14      *RSI
	ATR14      *ATR
	VolSMA20   *SMA
	Breakout20 *Breakout

	// Weekly scanner indicators
	EMA200   *EMA
	EMAVol20 *EMA // EMA of volume for weekly scanner

	// Previous values for crossover detection
	PrevEMA10           float64
	PrevEMA20           float64
	PrevSMA40           float64
	PrevClose           float64 // Previous candle close, used during the current evaluation.
	LastClose           float64 // Most recent processed close, persisted for the next update.
	LastCandleTimestamp time.Time

	// Signal state
	LongActive  bool
	ShortActive bool
	LastBuyBar  int
	LastSellBar int
	BarIndex    int
	WarmupCount int
}

// NewState creates a fresh indicator state for a symbol+timeframe.
func NewState(symbol, timeframe string) *State {
	return &State{
		Symbol:      symbol,
		Timeframe:   timeframe,
		EMA10:       NewEMA(10),
		EMA20:       NewEMA(20),
		SMA40:       NewSMA(40),
		EMA50:       NewEMA(50),
		RSI14:       NewRSI(14),
		ATR14:       NewATR(14),
		VolSMA20:    NewSMA(20),
		Breakout20:  NewBreakout(20),
		EMA200:      NewEMA(200),
		EMAVol20:    NewEMA(20),
		LastBuyBar:  -100,
		LastSellBar: -100,
	}
}

// UpdateFromCandle processes a finalized candle through all indicators.
func (s *State) UpdateFromCandle(c models.Candle) {
	// Save previous values for crossover detection
	s.PrevEMA10 = s.EMA10.Value
	s.PrevEMA20 = s.EMA20.Value
	s.PrevSMA40 = s.SMA40.Value
	s.PrevClose = s.LastClose

	// Update all indicators
	s.EMA10.Update(c.Close)
	s.EMA20.Update(c.Close)
	s.SMA40.Update(c.Close)
	s.EMA50.Update(c.Close)
	s.RSI14.Update(c.Close)
	s.ATR14.Update(c.High, c.Low, c.Close)
	s.VolSMA20.Update(float64(c.Volume))
	s.Breakout20.Update(c.High, c.Low)

	// Weekly scanner EMAs
	s.EMA200.Update(c.Close)
	s.EMAVol20.Update(float64(c.Volume))

	s.BarIndex++
	s.WarmupCount++
	s.LastClose = c.Close
	s.LastCandleTimestamp = c.Timestamp
}

// IsReady returns true if all core indicators have sufficient warmup.
func (s *State) IsReady() bool {
	return s.EMA10.IsReady() && s.EMA20.IsReady() && s.SMA40.IsReady() &&
		s.EMA50.IsReady() && s.RSI14.IsReady() && s.ATR14.IsReady() &&
		s.VolSMA20.IsReady() && s.Breakout20.IsReady()
}

// Manager manages indicator states for all symbol+timeframe combinations.
type Manager struct {
	mu     sync.RWMutex
	states map[string]*State // key: symbol:timeframe
	col    *mongo.Collection
}

// NewManager creates a new indicator manager.
func NewManager(snapshotCollection *mongo.Collection) *Manager {
	return &Manager{
		states: make(map[string]*State),
		col:    snapshotCollection,
	}
}

// GetOrCreate returns the state for a symbol+timeframe, creating if needed.
func (m *Manager) GetOrCreate(symbol, timeframe string) *State {
	key := models.IndicatorKey(symbol, timeframe)

	m.mu.RLock()
	state, exists := m.states[key]
	m.mu.RUnlock()

	if exists {
		return state
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if state, exists = m.states[key]; exists {
		return state
	}

	state = NewState(symbol, timeframe)
	m.states[key] = state
	return state
}

// Get returns the state for a symbol+timeframe, or nil if not found.
func (m *Manager) Get(symbol, timeframe string) *State {
	key := models.IndicatorKey(symbol, timeframe)
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.states[key]
}

// SaveSnapshot persists the current indicator state to MongoDB.
func (m *Manager) SaveSnapshot(ctx context.Context, state *State) error {
	hb, lb := state.Breakout20.GetState()
	
	snap := models.IndicatorSnapshot{
		Symbol:    state.Symbol,
		Timeframe: state.Timeframe,
		UpdatedAt: time.Now(),

		EMA10:       state.EMA10.Value,
		EMA20:       state.EMA20.Value,
		EMA50:       state.EMA50.Value,
		SMA40:       state.SMA40.Value,
		PrevSMA40:   state.PrevSMA40,
		SMA40Buffer: state.SMA40.GetBuffer(),

		RSIAvgGain:   state.RSI14.AvgGain,
		RSIAvgLoss:   state.RSI14.AvgLoss,
		RSIValue:     state.RSI14.Value,
		RSIPrevClose: state.RSI14.PrevClose,

		ATRValue:     state.ATR14.Value,
		ATRPrevClose: state.ATR14.PrevClose,

		AvgVolume:    state.VolSMA20.Value,
		VolumeBuffer: state.VolSMA20.GetBuffer(),

		HighBuffer:  hb,
		LowBuffer:   lb,
		PrevHighest: state.Breakout20.PrevHighest,
		PrevLowest:  state.Breakout20.PrevLowest,

		EMA200:   state.EMA200.Value,
		EMAVol20: state.EMAVol20.Value,

		LongActive:  state.LongActive,
		ShortActive: state.ShortActive,
		LastBuyBar:  state.LastBuyBar,
		LastSellBar: state.LastSellBar,
		BarIndex:    state.BarIndex,

		PrevEMA10:           state.PrevEMA10,
		PrevEMA20:           state.PrevEMA20,
		PrevClose:           state.LastClose,
		LastCandleTimestamp: state.LastCandleTimestamp,

		WarmupCount: state.WarmupCount,
		IsWarmedUp:  state.IsReady(),
	}

	filter := bson.M{"symbol": state.Symbol, "timeframe": state.Timeframe}
	update := bson.M{"$set": snap}
	opts := options.Update().SetUpsert(true)

	_, err := m.col.UpdateOne(ctx, filter, update, opts)
	return err
}

// LoadSnapshot restores indicator state from MongoDB.
func (m *Manager) LoadSnapshot(ctx context.Context, symbol, timeframe string) (*State, error) {
	log := logger.WithComponent("indicator.manager")

	filter := bson.M{"symbol": symbol, "timeframe": timeframe}
	var snap models.IndicatorSnapshot
	err := m.col.FindOne(ctx, filter).Decode(&snap)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	state := stateFromSnapshot(snap)
	m.setState(state)

	log.Info().
		Str("symbol", symbol).
		Str("tf", timeframe).
		Int("barIndex", snap.BarIndex).
		Bool("warmedUp", snap.IsWarmedUp).
		Msg("Indicator snapshot loaded")

	return state, nil
}

// GetOrLoad returns the state for a symbol+timeframe, loading a saved snapshot if available.
func (m *Manager) GetOrLoad(ctx context.Context, symbol, timeframe string) *State {
	if state := m.Get(symbol, timeframe); state != nil {
		return state
	}
	state, err := m.LoadSnapshot(ctx, symbol, timeframe)
	if err != nil {
		log := logger.WithComponent("indicator.manager")
		log.Warn().
			Err(err).
			Str("symbol", symbol).
			Str("tf", timeframe).
			Msg("Failed to load indicator snapshot")
	}
	if state != nil {
		return state
	}
	return m.GetOrCreate(symbol, timeframe)
}

// ResetState replaces any loaded snapshot/state with a fresh rolling state.
func (m *Manager) ResetState(symbol, timeframe string) *State {
	state := NewState(symbol, timeframe)
	m.setState(state)
	return state
}

// LoadAllSnapshots restores every persisted indicator snapshot into memory.
func (m *Manager) LoadAllSnapshots(ctx context.Context) error {
	log := logger.WithComponent("indicator.manager")

	cursor, err := m.col.Find(ctx, bson.M{})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	loaded := 0
	for cursor.Next(ctx) {
		var snap models.IndicatorSnapshot
		if err := cursor.Decode(&snap); err != nil {
			log.Warn().Err(err).Msg("Skipping malformed indicator snapshot")
			continue
		}
		m.setState(stateFromSnapshot(snap))
		loaded++
	}
	if err := cursor.Err(); err != nil {
		return err
	}

	log.Info().Int("loaded", loaded).Msg("Indicator snapshots loaded")
	return nil
}

func (m *Manager) setState(state *State) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[models.IndicatorKey(state.Symbol, state.Timeframe)] = state
}

func stateFromSnapshot(snap models.IndicatorSnapshot) *State {
	state := NewState(snap.Symbol, snap.Timeframe)

	state.EMA10.SetState(snap.EMA10, snap.WarmupCount)
	state.EMA20.SetState(snap.EMA20, snap.WarmupCount)
	state.EMA50.SetState(snap.EMA50, snap.WarmupCount)
	state.EMA200.SetState(snap.EMA200, snap.WarmupCount)
	state.EMAVol20.SetState(snap.EMAVol20, snap.WarmupCount)

	state.SMA40.SetState(snap.SMA40, snap.SMA40Buffer, snap.WarmupCount)
	state.VolSMA20.SetState(snap.AvgVolume, snap.VolumeBuffer, snap.WarmupCount)

	state.RSI14.SetState(snap.RSIAvgGain, snap.RSIAvgLoss, snap.RSIValue, snap.RSIPrevClose, snap.WarmupCount)
	state.ATR14.SetState(snap.ATRValue, snap.ATRPrevClose, snap.WarmupCount)

	state.LongActive = snap.LongActive
	state.ShortActive = snap.ShortActive
	state.LastBuyBar = snap.LastBuyBar
	state.LastSellBar = snap.LastSellBar
	state.BarIndex = snap.BarIndex
	state.WarmupCount = snap.WarmupCount

	state.PrevEMA10 = snap.PrevEMA10
	state.PrevEMA20 = snap.PrevEMA20
	state.PrevSMA40 = snap.PrevSMA40
	state.PrevClose = snap.PrevClose
	state.LastClose = snap.PrevClose
	state.LastCandleTimestamp = snap.LastCandleTimestamp

	state.Breakout20.SetState(snap.HighBuffer, snap.LowBuffer, snap.PrevHighest, snap.PrevLowest, snap.WarmupCount)

	return state
}

// SaveAllSnapshots persists all in-memory indicator states to MongoDB.
func (m *Manager) SaveAllSnapshots(ctx context.Context) error {
	log := logger.WithComponent("indicator.manager")

	m.mu.RLock()
	states := make([]*State, 0, len(m.states))
	for _, s := range m.states {
		states = append(states, s)
	}
	m.mu.RUnlock()

	var errCount int
	for _, s := range states {
		if err := m.SaveSnapshot(ctx, s); err != nil {
			log.Error().Err(err).Str("symbol", s.Symbol).Str("tf", s.Timeframe).Msg("Failed to save snapshot")
			errCount++
		}
	}

	log.Info().Int("total", len(states)).Int("errors", errCount).Msg("Indicator snapshots saved")
	return nil
}
