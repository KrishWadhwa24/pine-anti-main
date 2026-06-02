package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tradenexus/backend/internal/logger"
)

const (
	wsURL              = "wss://smartapisocket.angelone.in/smart-stream"
	heartbeatInterval  = 30 * time.Second
	reconnectBaseDelay = 1 * time.Second
	reconnectMaxDelay  = 30 * time.Second
	maxReconnectAttempts = 100
)

// SubscriptionRequest is the JSON request for subscribing/unsubscribing.
type SubscriptionRequest struct {
	CorrelationID string         `json:"correlationID,omitempty"`
	Action        int            `json:"action"` // 1=subscribe, 0=unsubscribe
	Params        SubParams      `json:"params"`
}

type SubParams struct {
	Mode      int         `json:"mode"`
	TokenList []TokenList `json:"tokenList"`
}

type TokenList struct {
	ExchangeType int      `json:"exchangeType"`
	Tokens       []string `json:"tokens"`
}

// WebSocketManager manages the Angel One WebSocket V2 connection.
type WebSocketManager struct {
	auth       *AuthManager
	conn       *websocket.Conn
	tickChan   chan *Tick

	mu              sync.RWMutex
	connected       bool
	reconnecting    int32 // atomic guard to prevent multiple reconnect loops
	subscriptions   map[string]int // token -> exchangeType for resubscription
	lastTickTime    time.Time

	ctx        context.Context
	cancel     context.CancelFunc
	onConnect  func() // callback when connection established
}

// NewWebSocketManager creates a new WebSocket manager.
func NewWebSocketManager(auth *AuthManager, tickChan chan *Tick) *WebSocketManager {
	return &WebSocketManager{
		auth:          auth,
		tickChan:      tickChan,
		subscriptions: make(map[string]int),
	}
}

// SetOnConnect sets a callback that fires after each successful connection.
func (ws *WebSocketManager) SetOnConnect(fn func()) {
	ws.onConnect = fn
}

// Connect establishes the WebSocket connection with authentication headers.
func (ws *WebSocketManager) Connect(ctx context.Context) error {
	log := logger.WithComponent("broker.websocket")

	ws.ctx, ws.cancel = context.WithCancel(ctx)

	jwt, err := ws.auth.GetJWTToken()
	if err != nil {
		return fmt.Errorf("failed to get JWT for WS: %w", err)
	}

	feedToken, err := ws.auth.GetFeedToken()
	if err != nil {
		return fmt.Errorf("failed to get feed token: %w", err)
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+jwt)
	header.Set("x-api-key", ws.auth.GetAPIKey())
	header.Set("x-client-code", ws.auth.GetClientCode())
	header.Set("x-feed-token", feedToken)

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}

	conn, _, err := dialer.DialContext(ws.ctx, wsURL, header)
	if err != nil {
		return fmt.Errorf("WS dial failed: %w", err)
	}

	ws.mu.Lock()
	ws.conn = conn
	ws.connected = true
	ws.mu.Unlock()

	log.Info().Msg("WebSocket V2 connected")

	// Start reader and heartbeat goroutines
	go ws.readLoop()
	go ws.heartbeatLoop()

	// Resubscribe if we have existing subscriptions
	ws.resubscribe()

	if ws.onConnect != nil {
		ws.onConnect()
	}

	return nil
}

// Subscribe subscribes to market data for the given tokens.
func (ws *WebSocketManager) Subscribe(exchangeType int, tokens []string, mode int) error {
	log := logger.WithComponent("broker.websocket")

	ws.mu.Lock()
	for _, t := range tokens {
		ws.subscriptions[t] = exchangeType
	}
	ws.mu.Unlock()

	req := SubscriptionRequest{
		CorrelationID: fmt.Sprintf("sub_%d", time.Now().UnixMilli()),
		Action:        1,
		Params: SubParams{
			Mode: mode,
			TokenList: []TokenList{
				{ExchangeType: exchangeType, Tokens: tokens},
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		return err
	}

	ws.mu.RLock()
	conn := ws.conn
	ws.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("WebSocket not connected")
	}

	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Error().Err(err).Msg("Failed to send subscription")
		return err
	}

	log.Info().Int("exchange", exchangeType).Int("count", len(tokens)).Msg("Subscribed to tokens")
	return nil
}

// Unsubscribe removes market data subscription for the given tokens.
func (ws *WebSocketManager) Unsubscribe(exchangeType int, tokens []string, mode int) error {
	log := logger.WithComponent("broker.websocket")

	ws.mu.Lock()
	for _, t := range tokens {
		delete(ws.subscriptions, t)
	}
	ws.mu.Unlock()

	req := SubscriptionRequest{
		CorrelationID: fmt.Sprintf("unsub_%d", time.Now().UnixMilli()),
		Action:        0,
		Params: SubParams{
			Mode: mode,
			TokenList: []TokenList{
				{ExchangeType: exchangeType, Tokens: tokens},
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		return err
	}

	ws.mu.RLock()
	conn := ws.conn
	ws.mu.RUnlock()

	if conn == nil {
		return nil
	}

	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Error().Err(err).Msg("Failed to send unsubscription")
		return err
	}

	log.Info().Int("exchange", exchangeType).Int("count", len(tokens)).Msg("Unsubscribed from tokens")
	return nil
}

// IsConnected returns the current connection state.
func (ws *WebSocketManager) IsConnected() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.connected
}

// LastTickTime returns the time of the last received tick.
func (ws *WebSocketManager) LastTickTime() time.Time {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.lastTickTime
}

// SubscriptionCount returns the number of active subscriptions.
func (ws *WebSocketManager) SubscriptionCount() int {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return len(ws.subscriptions)
}

// Close shuts down the WebSocket connection.
func (ws *WebSocketManager) Close() {
	if ws.cancel != nil {
		ws.cancel()
	}
	ws.mu.Lock()
	if ws.conn != nil {
		ws.conn.Close()
		ws.connected = false
	}
	ws.mu.Unlock()
}

// readLoop continuously reads binary messages from the WebSocket.
func (ws *WebSocketManager) readLoop() {
	log := logger.WithComponent("broker.websocket")

	for {
		select {
		case <-ws.ctx.Done():
			return
		default:
		}

		ws.mu.RLock()
		conn := ws.conn
		ws.mu.RUnlock()

		if conn == nil {
			return
		}

		msgType, data, err := conn.ReadMessage()
		if err != nil {
			log.Warn().Err(err).Msg("WebSocket read error, initiating reconnect")
			ws.mu.Lock()
			ws.connected = false
			ws.mu.Unlock()
			// Only spawn one reconnect loop at a time
			if atomic.CompareAndSwapInt32(&ws.reconnecting, 0, 1) {
				go ws.reconnectLoop()
			}
			return
		}

		// Text messages are heartbeat pong responses — ignore
		if msgType == websocket.TextMessage {
			continue
		}

		// Binary messages are market data ticks
		if msgType == websocket.BinaryMessage {
			tick, err := ParseBinaryTick(data)
			if err != nil {
				log.Debug().Err(err).Int("bytes", len(data)).Msg("Failed to parse tick")
				continue
			}

			ws.mu.Lock()
			ws.lastTickTime = time.Now()
			ws.mu.Unlock()

			// Non-blocking send to tick channel
			select {
			case ws.tickChan <- tick:
			default:
				log.Warn().Str("token", tick.Token).Msg("Tick channel full, dropping tick")
			}
		}
	}
}

// heartbeatLoop sends ping messages every 30 seconds to keep the connection alive.
func (ws *WebSocketManager) heartbeatLoop() {
	log := logger.WithComponent("broker.websocket")
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ws.ctx.Done():
			return
		case <-ticker.C:
			ws.mu.RLock()
			conn := ws.conn
			connected := ws.connected
			ws.mu.RUnlock()

			if !connected || conn == nil {
				return
			}

			if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
				log.Warn().Err(err).Msg("Heartbeat ping failed")
				return
			}
			log.Debug().Msg("Heartbeat ping sent")
		}
	}
}

// reconnectLoop attempts to reconnect with exponential backoff.
func (ws *WebSocketManager) reconnectLoop() {
	log := logger.WithComponent("broker.websocket")
	delay := reconnectBaseDelay

	for attempt := 1; attempt <= maxReconnectAttempts; attempt++ {
		select {
		case <-ws.ctx.Done():
			return
		default:
		}

		log.Info().Int("attempt", attempt).Dur("delay", delay).Msg("Attempting WebSocket reconnect")

		time.Sleep(delay)

		// Re-authenticate before reconnecting (tokens may have expired)
		if err := ws.auth.Login(); err != nil {
			log.Error().Err(err).Msg("Re-auth failed during reconnect")
			delay = minDuration(delay*2, reconnectMaxDelay)
			continue
		}

		if err := ws.Connect(ws.ctx); err != nil {
			log.Error().Err(err).Msg("Reconnect failed")
			delay = minDuration(delay*2, reconnectMaxDelay)
			continue
		}

		log.Info().Int("attempt", attempt).Msg("WebSocket reconnected successfully")
		atomic.StoreInt32(&ws.reconnecting, 0)
		return
	}

	log.Error().Msg("Max reconnection attempts exhausted")
	atomic.StoreInt32(&ws.reconnecting, 0)
}

// resubscribe sends subscription requests for all tracked tokens after reconnect.
func (ws *WebSocketManager) resubscribe() {
	log := logger.WithComponent("broker.websocket")

	ws.mu.RLock()
	subs := make(map[int][]string) // exchangeType -> tokens
	for token, exchType := range ws.subscriptions {
		subs[exchType] = append(subs[exchType], token)
	}
	ws.mu.RUnlock()

	if len(subs) == 0 {
		return
	}

	for exchType, tokens := range subs {
		if err := ws.Subscribe(exchType, tokens, ModeQuote); err != nil {
			log.Error().Err(err).Int("exchange", exchType).Msg("Resubscription failed")
		}
	}

	log.Info().Int("totalTokens", len(ws.subscriptions)).Msg("Resubscription complete")
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
