package indicator

// EMA implements a rolling Exponential Moving Average calculator.
type EMA struct {
	Period     int
	Multiplier float64
	Value      float64
	count      int
	sum        float64
	ready      bool
}

// NewEMA creates a new EMA calculator.
func NewEMA(period int) *EMA {
	return &EMA{
		Period:     period,
		Multiplier: 2.0 / float64(period+1),
	}
}

// Update processes a new value and returns the current EMA.
// Seeds with SMA of the first `period` values.
func (e *EMA) Update(value float64) float64 {
	e.count++

	if !e.ready {
		e.sum += value
		if e.count == e.Period {
			e.Value = e.sum / float64(e.Period)
			e.ready = true
		}
		return e.Value
	}

	e.Value = (value-e.Value)*e.Multiplier + e.Value
	return e.Value
}

// IsReady returns true once enough data points have been processed.
func (e *EMA) IsReady() bool {
	return e.ready
}

// Reset clears the EMA state.
func (e *EMA) Reset() {
	e.Value = 0
	e.count = 0
	e.sum = 0
	e.ready = false
}

// SetState restores EMA state from a snapshot.
func (e *EMA) SetState(value float64, count int) {
	e.Value = value
	e.count = count
	e.ready = count >= e.Period
}
