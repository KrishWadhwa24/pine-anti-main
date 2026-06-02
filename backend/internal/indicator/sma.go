package indicator

// SMA implements a rolling Simple Moving Average using a circular buffer.
type SMA struct {
	Period int
	Value  float64
	buffer []float64
	pos    int
	count  int
	sum    float64
	ready  bool
}

// NewSMA creates a new SMA calculator.
func NewSMA(period int) *SMA {
	return &SMA{
		Period: period,
		buffer: make([]float64, period),
	}
}

// Update processes a new value and returns the current SMA.
func (s *SMA) Update(value float64) float64 {
	// Remove oldest value from sum if buffer is full
	if s.count >= s.Period {
		s.sum -= s.buffer[s.pos]
	}

	// Add new value
	s.buffer[s.pos] = value
	s.sum += value
	s.pos = (s.pos + 1) % s.Period

	if s.count < s.Period {
		s.count++
	}

	if s.count >= s.Period {
		s.ready = true
	}

	s.Value = s.sum / float64(s.count)
	return s.Value
}

// IsReady returns true once enough data points have been processed.
func (s *SMA) IsReady() bool {
	return s.ready
}

// GetBuffer returns a copy of the internal buffer values (oldest to newest).
func (s *SMA) GetBuffer() []float64 {
	result := make([]float64, 0, s.count)
	n := s.count
	if n > s.Period {
		n = s.Period
	}
	start := (s.pos - n + s.Period) % s.Period
	for i := 0; i < n; i++ {
		idx := (start + i) % s.Period
		result = append(result, s.buffer[idx])
	}
	return result
}

// SetState restores SMA state from a snapshot.
func (s *SMA) SetState(value float64, buffer []float64, count int) {
	s.Value = value
	s.count = count
	s.ready = count >= s.Period
	s.sum = 0
	copy(s.buffer, buffer)
	for _, v := range buffer {
		s.sum += v
	}
	s.pos = len(buffer) % s.Period
}
