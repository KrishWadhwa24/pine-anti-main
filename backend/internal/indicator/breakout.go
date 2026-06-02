package indicator

// Breakout implements rolling highest-high and lowest-low detection with crossover/crossunder.
type Breakout struct {
	Period     int
	highBuffer []float64
	lowBuffer  []float64
	pos        int
	count      int
	ready      bool

	// [1]-shifted values (prior bar's highest/lowest)
	PrevHighest float64
	PrevLowest  float64

	// Current values
	Highest float64
	Lowest  float64
}

// NewBreakout creates a new breakout detector.
func NewBreakout(period int) *Breakout {
	return &Breakout{
		Period:     period,
		highBuffer: make([]float64, period),
		lowBuffer:  make([]float64, period),
	}
}

// Update processes a new candle's high and low.
// Returns the [1]-shifted highest/lowest (i.e., excluding the current bar).
func (b *Breakout) Update(high, low float64) (prevHighest, prevLowest float64) {
	// Save current as "previous" before updating
	b.PrevHighest = b.Highest
	b.PrevLowest = b.Lowest

	// Add to buffers
	b.highBuffer[b.pos] = high
	b.lowBuffer[b.pos] = low
	b.pos = (b.pos + 1) % b.Period

	if b.count < b.Period {
		b.count++
	}

	if b.count >= b.Period {
		b.ready = true
	}

	// Recalculate max/min from buffer
	b.Highest = b.highBuffer[0]
	b.Lowest = b.lowBuffer[0]
	n := b.count
	if n > b.Period {
		n = b.Period
	}
	for i := 1; i < n; i++ {
		if b.highBuffer[i] > b.Highest {
			b.Highest = b.highBuffer[i]
		}
		if b.lowBuffer[i] < b.Lowest {
			b.Lowest = b.lowBuffer[i]
		}
	}

	return b.PrevHighest, b.PrevLowest
}

// GetState returns the current internal state buffers for persistence.
func (b *Breakout) GetState() ([]float64, []float64) {
	hb := make([]float64, len(b.highBuffer))
	lb := make([]float64, len(b.lowBuffer))
	copy(hb, b.highBuffer)
	copy(lb, b.lowBuffer)
	return hb, lb
}

// SetState restores the breakout detector from a saved state.
func (b *Breakout) SetState(highBuffer, lowBuffer []float64, prevHighest, prevLowest float64, count int) {
	if len(highBuffer) == b.Period {
		copy(b.highBuffer, highBuffer)
	}
	if len(lowBuffer) == b.Period {
		copy(b.lowBuffer, lowBuffer)
	}
	b.PrevHighest = prevHighest
	b.PrevLowest = prevLowest
	b.count = count
	b.pos = count % b.Period
	if b.count >= b.Period {
		b.ready = true
	}
	
	// Recalculate current highest/lowest
	if b.count > 0 {
		b.Highest = b.highBuffer[0]
		b.Lowest = b.lowBuffer[0]
		n := b.count
		if n > b.Period {
			n = b.Period
		}
		for i := 1; i < n; i++ {
			if b.highBuffer[i] > b.Highest {
				b.Highest = b.highBuffer[i]
			}
			if b.lowBuffer[i] < b.Lowest {
				b.Lowest = b.lowBuffer[i]
			}
		}
	}
}

// IsReady returns true once enough data points have been processed.
func (b *Breakout) IsReady() bool {
	return b.ready
}

// FreshBullBreakout detects if close just crossed above the prior bar's highest level.
// Implements: ta.crossover(close, highestLevel) where highestLevel = ta.highest(high, period)[1]
func FreshBullBreakout(close, prevClose, prevHighest float64) bool {
	return close > prevHighest && prevClose <= prevHighest
}

// FreshBearBreakout detects if close just crossed below the prior bar's lowest level.
// Implements: ta.crossunder(close, lowestLevel) where lowestLevel = ta.lowest(low, period)[1]
func FreshBearBreakout(close, prevClose, prevLowest float64) bool {
	return close < prevLowest && prevClose >= prevLowest
}

// Crossover returns true if a just crossed above b (was below, now above).
func Crossover(currentA, currentB, prevA, prevB float64) bool {
	return currentA > currentB && prevA <= prevB
}

// Crossunder returns true if a just crossed below b (was above, now below).
func Crossunder(currentA, currentB, prevA, prevB float64) bool {
	return currentA < currentB && prevA >= prevB
}

// MaxN returns the maximum value over the last n items in a slice (most recent at end).
func MaxN(values []float64, n int) float64 {
	if len(values) == 0 {
		return 0
	}
	start := len(values) - n
	if start < 0 {
		start = 0
	}
	max := values[start]
	for i := start + 1; i < len(values); i++ {
		if values[i] > max {
			max = values[i]
		}
	}
	return max
}

// MinN returns the minimum value over the last n items in a slice.
func MinN(values []float64, n int) float64 {
	if len(values) == 0 {
		return 0
	}
	start := len(values) - n
	if start < 0 {
		start = 0
	}
	min := values[start]
	for i := start + 1; i < len(values); i++ {
		if values[i] < min {
			min = values[i]
		}
	}
	return min
}
