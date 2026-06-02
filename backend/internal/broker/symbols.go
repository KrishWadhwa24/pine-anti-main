package broker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tradenexus/backend/internal/logger"
)

const (
	instrumentMasterURL = "https://margincalculator.angelone.in/OpenAPI_File/files/OpenAPIScripMaster.json"
)

// Instrument represents an Angel One tradeable instrument.
type Instrument struct {
	Token       string `json:"token"`
	Symbol      string `json:"symbol"`
	Name        string `json:"name"`
	Expiry      string `json:"expiry"`
	Strike      string `json:"strike"`
	LotSize     string `json:"lotsize"`
	InstrType   string `json:"instrumenttype"`
	ExchSeg     string `json:"exch_seg"`
	TickSize    string `json:"tick_size"`
}

// SymbolResolver maps stock symbols to Angel One instrument tokens.
type SymbolResolver struct {
	mu          sync.RWMutex
	instruments map[string]*Instrument // symbol -> instrument (NSE equity)
	byToken     map[string]*Instrument // token -> instrument
	lastRefresh time.Time
}

// NewSymbolResolver creates a new symbol resolver.
func NewSymbolResolver() *SymbolResolver {
	return &SymbolResolver{
		instruments: make(map[string]*Instrument),
		byToken:     make(map[string]*Instrument),
	}
}

// Refresh downloads and parses the Angel One instrument master file.
func (s *SymbolResolver) Refresh() error {
	log := logger.WithComponent("broker.symbols")
	log.Info().Msg("Refreshing instrument master...")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(instrumentMasterURL)
	if err != nil {
		return fmt.Errorf("failed to fetch instrument master: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read instrument master: %w", err)
	}

	var instruments []Instrument
	if err := json.Unmarshal(body, &instruments); err != nil {
		return fmt.Errorf("failed to parse instrument master: %w", err)
	}

	newMap := make(map[string]*Instrument)
	newByToken := make(map[string]*Instrument)

	for i := range instruments {
		inst := &instruments[i]
		// Only index NSE CM and BSE CM equity instruments
		if (inst.ExchSeg == "NSE" || inst.ExchSeg == "BSE") && inst.InstrType == "" {
			key := inst.ExchSeg + ":" + inst.Symbol
			newMap[key] = inst
			tokenKey := inst.ExchSeg + ":" + inst.Token
			newByToken[tokenKey] = inst
		}
	}

	s.mu.Lock()
	s.instruments = newMap
	s.byToken = newByToken
	s.lastRefresh = time.Now()
	s.mu.Unlock()

	log.Info().Int("nseCount", len(newMap)).Msg("Instrument master refreshed")
	return nil
}

// Resolve looks up an instrument by exchange and symbol.
func (s *SymbolResolver) Resolve(exchange, symbol string) (*Instrument, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := exchange + ":" + symbol
	inst, ok := s.instruments[key]
	if !ok {
		return nil, fmt.Errorf("symbol not found: %s", key)
	}
	return inst, nil
}

// ResolveByToken looks up an instrument by exchange and token.
func (s *SymbolResolver) ResolveByToken(exchange, token string) (*Instrument, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := exchange + ":" + token
	inst, ok := s.byToken[key]
	if !ok {
		return nil, fmt.Errorf("token not found: %s", key)
	}
	return inst, nil
}

// SearchSymbols searches for instruments matching a query string.
func (s *SymbolResolver) SearchSymbols(query string, limit int) []*Instrument {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query = strings.ToUpper(query)
	var results []*Instrument

	for _, inst := range s.instruments {
		if strings.Contains(inst.Symbol, query) || strings.Contains(strings.ToUpper(inst.Name), query) {
			results = append(results, inst)
			if len(results) >= limit {
				break
			}
		}
	}

	return results
}

// ExchangeTypeFromSeg converts exchange segment string to WebSocket exchange type int.
func ExchangeTypeFromSeg(exchSeg string) int {
	switch exchSeg {
	case "NSE":
		return ExchangeNSE_CM
	case "BSE":
		return ExchangeBSE_CM
	case "NFO":
		return ExchangeNSE_FO
	case "BFO":
		return ExchangeBSE_FO
	case "MCX":
		return ExchangeMCX_FO
	default:
		return ExchangeNSE_CM
	}
}

// NeedsRefresh returns true if instrument data is stale (>24h).
func (s *SymbolResolver) NeedsRefresh() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.lastRefresh) > 24*time.Hour
}
