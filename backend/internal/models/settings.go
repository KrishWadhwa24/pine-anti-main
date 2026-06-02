package models

import "time"

// TelegramSettings stores encrypted Telegram Bot configuration.
type TelegramSettings struct {
	Key               string    `bson:"key" json:"key"` // "telegram_config"
	BotTokenEncrypted string    `bson:"botTokenEncrypted" json:"-"`
	ChatIDEncrypted   string    `bson:"chatIdEncrypted" json:"-"`
	BotToken          string    `bson:"-" json:"botToken,omitempty"` // decrypted, never persisted
	ChatID            string    `bson:"-" json:"chatId,omitempty"`   // decrypted, never persisted
	IsConfigured      bool      `bson:"isConfigured" json:"isConfigured"`
	LastTestedAt      time.Time `bson:"lastTestedAt,omitempty" json:"lastTestedAt,omitempty"`
	TestSuccess       bool      `bson:"testSuccess" json:"testSuccess"`
	UpdatedAt         time.Time `bson:"updatedAt" json:"updatedAt"`
}

// SystemHealth provides a snapshot of backend health for the frontend.
type SystemHealth struct {
	WebSocketConnected  bool      `json:"webSocketConnected"`
	WebSocketLastTick   time.Time `json:"webSocketLastTick"`
	MongoConnected      bool      `json:"mongoConnected"`
	RedisConnected      bool      `json:"redisConnected"`
	TelegramConfigured  bool      `json:"telegramConfigured"`
	ActiveSubscriptions int       `json:"activeSubscriptions"`
	UptimeSeconds       int64     `json:"uptimeSeconds"`
	LastSignalAt        time.Time `json:"lastSignalAt"`
	LastRecoveryAt      time.Time `json:"lastRecoveryAt"`
	MarketOpen          bool      `json:"marketOpen"`
}

// TelegramQueueMessage is the payload for the Redis dispatch stream.
type TelegramQueueMessage struct {
	SignalHash string `json:"signalHash"`
	ChatID     string `json:"chatId"`
	Message    string `json:"message"`
	Retries    int    `json:"retries"`
	CreatedAt  int64  `json:"createdAt"` // Unix timestamp — intentional for Redis
}
