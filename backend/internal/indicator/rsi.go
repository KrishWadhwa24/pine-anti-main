package indicator

import "math"

// RSI implements a rolling Relative Strength Index using Wilder's smoothing method.
type RSI struct {
	Period    int
	Value     float64
	AvgGain   float64
	AvgLoss   float64
	PrevClose float64
	count     int
	gains     []float64
	losses    []float64
	ready     bool
}

// NewRSI creates a new RSI calculator.
func NewRSI(period int) *RSI {
	return &RSI{
		Period: period,
		gains:  make([]float64, 0, period),
		losses: make([]float64, 0, period),
	}
}

// Update processes a new close price and returns the current RSI.
func (r *RSI) Update(close float64) float64 {
	r.count++

	if r.count == 1 {
		r.PrevClose = close
		return 50 // neutral until ready
	}

	change := close - r.PrevClose
	gain := math.Max(change, 0)
	loss := math.Max(-change, 0)
	r.PrevClose = close

	if !r.ready {
		r.gains = append(r.gains, gain)
		r.losses = append(r.losses, loss)

		if len(r.gains) == r.Period {
			// First RSI: use simple average of gains/losses
			var sumGain, sumLoss float64
			for _, g := range r.gains {
				sumGain += g
			}
			for _, l := range r.losses {
				sumLoss += l
			}
			r.AvgGain = sumGain / float64(r.Period)
			r.AvgLoss = sumLoss / float64(r.Period)
			r.ready = true

			if r.AvgLoss == 0 {
				r.Value = 100
			} else {
				rs := r.AvgGain / r.AvgLoss
				r.Value = 100 - (100 / (1 + rs))
			}

			// Free the init buffers
			r.gains = nil
			r.losses = nil
		}
		return r.Value
	}

	// Wilder's smoothing: avgGain = (prevAvgGain * (period-1) + currentGain) / period
	r.AvgGain = (r.AvgGain*float64(r.Period-1) + gain) / float64(r.Period)
	r.AvgLoss = (r.AvgLoss*float64(r.Period-1) + loss) / float64(r.Period)

	if r.AvgLoss == 0 {
		r.Value = 100
	} else {
		rs := r.AvgGain / r.AvgLoss
		r.Value = 100 - (100 / (1 + rs))
	}

	return r.Value
}

// IsReady returns true once enough data points have been processed.
func (r *RSI) IsReady() bool {
	return r.ready
}

// SetState restores RSI state from a snapshot.
func (r *RSI) SetState(avgGain, avgLoss, value, prevClose float64, count int) {
	r.AvgGain = avgGain
	r.AvgLoss = avgLoss
	r.Value = value
	r.PrevClose = prevClose
	r.count = count
	r.ready = count >= r.Period+1
}
