package models

import "time"

// Watchlist represents a user-created stock watchlist.
type Watchlist struct {
	ID        string    `bson:"_id,omitempty" json:"id"`
	Name      string    `bson:"name" json:"name"`
	Stocks    []Stock   `bson:"stocks" json:"stocks"`
	IsActive  bool      `bson:"isActive" json:"isActive"`
	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt"`
}

// Stock represents a stock in a watchlist.
type Stock struct {
	Symbol       string `bson:"symbol" json:"symbol"`
	Token        string `bson:"token" json:"token"`
	Exchange     string `bson:"exchange" json:"exchange"`     // NSE / BSE
	ExchangeType int    `bson:"exchangeType" json:"exchangeType"` // 1=NSE_CM, 3=BSE_CM
	Name         string `bson:"name" json:"name"`
}

// MaxWatchlists is the maximum number of watchlists allowed.
const MaxWatchlists = 10
