package metrics

import (
	"sort"
	"time"
)

// Calculator computes benchmark metrics from timing data.
type Calculator struct{}

// NewCalculator creates a new metrics calculator.
func NewCalculator() *Calculator {
	return &Calculator{}
}

// Calculate computes metrics from call timings.
func (c *Calculator) Calculate(timings []CallTiming, totalGas uint64) *BenchmarkMetrics {
	if len(timings) == 0 {
		return &BenchmarkMetrics{}
	}

	m := &BenchmarkMetrics{
		TotalGas:  totalGas,
		CallCount: len(timings),
		Latencies: make([]time.Duration, len(timings)),
	}

	// Extract latencies and calculate total duration
	var totalDuration time.Duration
	for i, t := range timings {
		m.Latencies[i] = t.Duration
		totalDuration += t.Duration
	}
	m.Duration = totalDuration

	// Calculate MGas/s
	if m.Duration > 0 {
		seconds := m.Duration.Seconds()
		m.MGasPerSecond = float64(m.TotalGas) / seconds / 1_000_000
	}

	// Calculate latency statistics
	c.calculateLatencyStats(m)

	return m
}

func (c *Calculator) calculateLatencyStats(m *BenchmarkMetrics) {
	if len(m.Latencies) == 0 {
		return
	}

	// Create sorted copy for percentile calculation
	sorted := make([]time.Duration, len(m.Latencies))
	copy(sorted, m.Latencies)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	// Min/Max
	m.LatencyMin = sorted[0]
	m.LatencyMax = sorted[len(sorted)-1]

	// Mean
	var sum time.Duration
	for _, l := range sorted {
		sum += l
	}
	m.LatencyMean = sum / time.Duration(len(sorted))

	// Percentiles
	m.LatencyP50 = c.percentile(sorted, 0.50)
	m.LatencyP95 = c.percentile(sorted, 0.95)
	m.LatencyP99 = c.percentile(sorted, 0.99)
}

func (c *Calculator) percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}

	idx := int(float64(len(sorted)-1) * p)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	return sorted[idx]
}
