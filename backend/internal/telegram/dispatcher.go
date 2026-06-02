package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/tradenexus/backend/internal/logger"
	"github.com/tradenexus/backend/internal/models"
	"github.com/tradenexus/backend/internal/store"
)

// Dispatcher consumes the Redis Telegram queue and delivers messages.
type Dispatcher struct {
	redisStore *store.RedisStore
	httpClient *http.Client
	botToken   string
	chatID     string
}

// NewDispatcher creates a Telegram dispatcher.
func NewDispatcher(redisStore *store.RedisStore) *Dispatcher {
	return &Dispatcher{
		redisStore: redisStore,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// SetCredentials updates the bot token and chat ID.
func (d *Dispatcher) SetCredentials(botToken, chatID string) {
	d.botToken = botToken
	d.chatID = chatID
}

// IsConfigured reports whether the dispatcher has credentials available.
func (d *Dispatcher) IsConfigured() bool {
	return d.botToken != "" && d.chatID != ""
}

// EnqueueSignal formats a signal and adds it to the Redis dispatch queue.
func (d *Dispatcher) EnqueueSignal(ctx context.Context, sig models.Signal) error {
	if !d.IsConfigured() {
		return fmt.Errorf("telegram not configured")
	}

	msg := FormatSignalAlert(sig)

	payload := map[string]interface{}{
		"signalHash": sig.SignalHash,
		"chatId":     d.chatID,
		"message":    msg,
		"retries":    "0",
		"createdAt":  fmt.Sprintf("%d", time.Now().Unix()),
	}

	return d.redisStore.EnqueueTelegram(ctx, payload)
}

// Start begins consuming the dispatch queue.
func (d *Dispatcher) Start(ctx context.Context) {
	log := logger.WithComponent("telegram.dispatcher")

	if d.botToken == "" || d.chatID == "" {
		log.Warn().Msg("Telegram not configured, dispatcher idle")
	}

	consumer := fmt.Sprintf("dispatcher-%d", time.Now().UnixMilli())

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			if d.botToken == "" || d.chatID == "" {
				time.Sleep(5 * time.Second)
				continue
			}

			streams, err := d.redisStore.ReadTelegramStream(ctx, consumer, 1, 3*time.Second)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				continue
			}

			for _, stream := range streams {
				for _, msg := range stream.Messages {
					chatID, _ := msg.Values["chatId"].(string)
					message, _ := msg.Values["message"].(string)
					if chatID == "" || message == "" {
						_ = d.redisStore.MoveToDLQ(ctx, msg.Values)
						_ = d.redisStore.AckTelegram(ctx, msg.ID)
						log.Error().Str("id", msg.ID).Msg("Malformed Telegram message moved to DLQ")
						continue
					}

					retriesStr, _ := msg.Values["retries"].(string)
					retries := 0
					fmt.Sscanf(retriesStr, "%d", &retries)

					if err := d.sendMessage(chatID, message); err != nil {
						log.Error().Err(err).Str("id", msg.ID).Int("retries", retries).Msg("Telegram send failed")

						if retries >= 5 {
							// Dead letter
							_ = d.redisStore.MoveToDLQ(ctx, msg.Values)
							_ = d.redisStore.AckTelegram(ctx, msg.ID)
							log.Error().Str("id", msg.ID).Msg("Message moved to DLQ after max retries")
						} else {
							// Re-enqueue with incremented retry count
							msg.Values["retries"] = fmt.Sprintf("%d", retries+1)
							_ = d.redisStore.EnqueueTelegram(ctx, msg.Values)
							_ = d.redisStore.AckTelegram(ctx, msg.ID)

							// Exponential backoff
							delay := time.Duration(1<<uint(retries)) * time.Second
							time.Sleep(delay)
						}
						continue
					}

					_ = d.redisStore.AckTelegram(ctx, msg.ID)
					log.Info().Str("id", msg.ID).Msg("Telegram message delivered")
				}
			}
		}
	}()

	log.Info().Msg("Telegram dispatcher started")
}

// SendTestMessage sends a test message to verify Telegram integration.
func (d *Dispatcher) SendTestMessage(botToken, chatID string) error {
	msg := "🔔 *TradeNexus* — Test Connection Successful!\n\n" +
		"✅ Your Telegram integration is working correctly.\n" +
		"📊 You will receive trading signals here.\n\n" +
		"━━━━━━━━━━━━━━━━━━━\n" +
		"_Sent at: " + time.Now().In(time.FixedZone("IST", 19800)).Format("02 Jan 2006, 15:04:05 IST") + "_"

	return d.sendMessageWithToken(botToken, chatID, msg)
}

func (d *Dispatcher) sendMessage(chatID, message string) error {
	return d.sendMessageWithToken(d.botToken, chatID, message)
}

func (d *Dispatcher) sendMessageWithToken(botToken, chatID, message string) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	params := url.Values{}
	params.Set("chat_id", chatID)
	params.Set("text", message)
	params.Set("parse_mode", "Markdown")

	resp, err := d.httpClient.PostForm(apiURL, params)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		return fmt.Errorf("telegram API error: %v", result)
	}

	return nil
}

// FormatSignalAlert formats a signal into a premium Telegram message.
func FormatSignalAlert(sig models.Signal) string {
	emoji := "🟢"
	action := "BUY"
	if sig.SignalType == models.SignalSell {
		emoji = "🔴"
		action = "SELL"
	}

	tfLabel := models.TimeframeLabel(sig.Timeframe)

	msg := fmt.Sprintf(
		"%s *%s SIGNAL — %s (%s)*\n"+
			"━━━━━━━━━━━━━━━━━━━\n"+
			"📊 *Strategy:* %s\n"+
			"⏱ *Timeframe:* %s — %s\n\n"+
			"📈 *Breakout:* %s\n"+
			"💪 *Candle:* Body strength %.2fx ATR\n"+
			"📊 *Volume:* %s\n"+
			"🔥 *RSI:* %.1f (%s)\n"+
			"📐 *Trend:* %s\n\n"+
			"⚡ *Conviction:* %s",
		emoji, action, sig.Symbol, sig.Timeframe,
		categoryLabel(sig.Category),
		sig.Timeframe, tfLabel,
		sig.BreakoutReason,
		sig.BodyStrength,
		sig.VolumeConfirm,
		sig.RSIValue, sig.RSIState,
		sig.TrendConfirm,
		sig.Conviction,
	)

	if len(sig.MatchedScanners) > 0 {
		msg += fmt.Sprintf("\n🏛 *Scanners:* %d/4 matched", len(sig.MatchedScanners))
	}

	msg += fmt.Sprintf(
		"\n━━━━━━━━━━━━━━━━━━━\n"+
			"💰 *Price:* ₹%.2f\n"+
			"🕐 *Candle Close:* %s",
		sig.Price,
		sig.CandleTimestamp.In(time.FixedZone("IST", 19800)).Format("02 Jan 2006, 15:04 IST"),
	)

	return msg
}

func categoryLabel(cat string) string {
	switch cat {
	case models.CategoryPineMomentum:
		return "Pine Script Momentum"
	case models.CategoryWeeklyConsolidated:
		return "Weekly Institutional Scanner"
	default:
		return cat
	}
}
