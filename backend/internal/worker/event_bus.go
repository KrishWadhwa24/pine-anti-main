package worker

import (
	"sync"
)

// EventType represents different event categories in the system.
type EventType string

const (
	EventTickReceived      EventType = "tick_received"
	EventCandleFinalized   EventType = "candle_finalized"
	EventIndicatorUpdated  EventType = "indicator_updated"
	EventSignalGenerated   EventType = "signal_generated"
	EventSignalValidated   EventType = "signal_validated"
	EventTelegramQueued    EventType = "telegram_queued"
	EventRecoveryStarted   EventType = "recovery_started"
	EventRecoveryCompleted EventType = "recovery_completed"
	EventWSConnected       EventType = "ws_connected"
	EventWSDisconnected    EventType = "ws_disconnected"
)

// Event is a message passed through the event bus.
type Event struct {
	Type    EventType
	Payload interface{}
}

// CandleFinalizedPayload is the payload for candle finalization events.
type CandleFinalizedPayload struct {
	Symbol    string
	Timeframe string
	Timestamp int64
}

// TickReceivedPayload is the payload for live tick events.
type TickReceivedPayload struct {
	Token     string
	Price     float64
	Timestamp int64
}

// EventHandler is a function that handles an event.
type EventHandler func(event Event)

// EventBus provides a publish-subscribe event system for decoupled communication.
type EventBus struct {
	mu       sync.RWMutex
	handlers map[EventType][]EventHandler
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		handlers: make(map[EventType][]EventHandler),
	}
}

// Subscribe registers a handler for a specific event type.
func (eb *EventBus) Subscribe(eventType EventType, handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.handlers[eventType] = append(eb.handlers[eventType], handler)
}

// Publish dispatches an event to all registered handlers asynchronously.
func (eb *EventBus) Publish(event Event) {
	eb.mu.RLock()
	handlers := make([]EventHandler, len(eb.handlers[event.Type]))
	copy(handlers, eb.handlers[event.Type])
	eb.mu.RUnlock()

	for _, handler := range handlers {
		go handler(event)
	}
}

// PublishSync dispatches an event to all registered handlers synchronously.
func (eb *EventBus) PublishSync(event Event) {
	eb.mu.RLock()
	handlers := make([]EventHandler, len(eb.handlers[event.Type]))
	copy(handlers, eb.handlers[event.Type])
	eb.mu.RUnlock()

	for _, handler := range handlers {
		handler(event)
	}
}
