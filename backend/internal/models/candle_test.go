package models

import (
	"testing"
	"time"
)

func TestActiveCandleUpdateFromTickPreservesStartTime(t *testing.T) {
	start := time.Date(2026, time.May, 27, 12, 15, 0, 0, time.UTC)
	tickTime := time.Date(2026, time.May, 27, 13, 9, 16, 0, time.UTC)

	ac := &ActiveCandle{StartTime: start}
	ac.UpdateFromTick(248.15, 100, tickTime)

	if !ac.StartTime.Equal(start) {
		t.Fatalf("StartTime = %v, want %v", ac.StartTime, start)
	}
	if !ac.LastTick.Equal(tickTime) {
		t.Fatalf("LastTick = %v, want %v", ac.LastTick, tickTime)
	}
}

func TestActiveCandleUpdateFromTickSetsMissingStartTime(t *testing.T) {
	tickTime := time.Date(2026, time.May, 27, 13, 9, 16, 0, time.UTC)

	ac := &ActiveCandle{}
	ac.UpdateFromTick(248.15, 100, tickTime)

	if !ac.StartTime.Equal(tickTime) {
		t.Fatalf("StartTime = %v, want %v", ac.StartTime, tickTime)
	}
}
