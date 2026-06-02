package main

import (
	"testing"
	"time"

	"github.com/tradenexus/backend/internal/candle"
	"github.com/tradenexus/backend/internal/models"
)

func TestCompletedHigherTimeframeWeek(t *testing.T) {
	weekStart := time.Date(2026, time.May, 25, 0, 0, 0, 0, candle.IST)

	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{
			name: "current week on Wednesday is partial",
			now:  time.Date(2026, time.May, 27, 12, 0, 0, 0, candle.IST),
			want: false,
		},
		{
			name: "Friday before market close is partial",
			now:  time.Date(2026, time.May, 29, 15, 29, 0, 0, candle.IST),
			want: false,
		},
		{
			name: "Friday at market close is complete",
			now:  time.Date(2026, time.May, 29, 15, 30, 0, 0, candle.IST),
			want: true,
		},
		{
			name: "Saturday after the trading week is complete",
			now:  time.Date(2026, time.May, 30, 10, 0, 0, 0, candle.IST),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := models.Candle{Timestamp: weekStart}
			got := isCompletedHigherTimeframeCandle(c, models.Timeframe1W, tt.now)
			if got != tt.want {
				t.Fatalf("isCompletedHigherTimeframeCandle() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompletedHigherTimeframeMonth(t *testing.T) {
	monthStart := time.Date(2026, time.May, 1, 0, 0, 0, 0, candle.IST)

	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{
			name: "current month before last trading day is partial",
			now:  time.Date(2026, time.May, 28, 12, 0, 0, 0, candle.IST),
			want: false,
		},
		{
			name: "last trading day before market close is partial",
			now:  time.Date(2026, time.May, 29, 15, 29, 0, 0, candle.IST),
			want: false,
		},
		{
			name: "last trading day at market close is complete",
			now:  time.Date(2026, time.May, 29, 15, 30, 0, 0, candle.IST),
			want: true,
		},
		{
			name: "next month means previous month is complete",
			now:  time.Date(2026, time.June, 1, 9, 0, 0, 0, candle.IST),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := models.Candle{Timestamp: monthStart}
			got := isCompletedHigherTimeframeCandle(c, models.Timeframe1M, tt.now)
			if got != tt.want {
				t.Fatalf("isCompletedHigherTimeframeCandle() = %v, want %v", got, tt.want)
			}
		})
	}
}
