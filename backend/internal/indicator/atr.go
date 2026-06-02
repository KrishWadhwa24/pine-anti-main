package indicator

import "math"

// ATR implements a rolling Average True Range using Wilder's smoothing.
type ATR struct {
	Period    int
	Value     float64
	PrevClose float64
	count     int
	trValues  []float64
	ready     bool
}

// NewATR creates a new ATR calculator.
func NewATR(period int) *ATR {
	return &ATR{
		Period:   period,
		trValues: make([]float64, 0, period),
	}
}

// Update processes a new candle's high, low, close and returns the current ATR.
func (a *ATR) Update(high, low, close float64) float64 {
	a.count++

	var tr float64
	if a.count == 1 {
		tr = high - low
	} else {
		tr = math.Max(high-low, math.Max(
			math.Abs(high-a.PrevClose),
			math.Abs(low-a.PrevClose),
		))
	}
	a.PrevClose = close

	if !a.ready {
		a.trValues = append(a.trValues, tr)
		if len(a.trValues) == a.Period {
			var sum float64
			for _, v := range a.trValues {
				sum += v
			}
			a.Value = sum / float64(a.Period)
			a.ready = true
			a.trValues = nil
		}
		return a.Value
	}

	// Wilder's smoothing: ATR = (prevATR * (period-1) + TR) / period
	a.Value = (a.Value*float64(a.Period-1) + tr) / float64(a.Period)
	return a.Value
}

// IsReady returns true once enough data points have been processed.
func (a *ATR) IsReady() bool {
	return a.ready
}

// SetState restores ATR state from a snapshot.
func (a *ATR) SetState(value, prevClose float64, count int) {
	a.Value = value
	a.PrevClose = prevClose
	a.count = count
	a.ready = count >= a.Period
}
