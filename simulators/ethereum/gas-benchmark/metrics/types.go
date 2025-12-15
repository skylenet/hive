// Package metrics provides types and utilities for benchmark metrics calculation.
package metrics

import (
	"fmt"
	"time"
)

// BenchmarkMetrics contains all metrics for a single benchmark run.
type BenchmarkMetrics struct {
	TotalGas      uint64        // Total gas executed
	Duration      time.Duration // Total execution time
	MGasPerSecond float64       // Megagas per second
	CallCount     int           // Number of RPC calls

	// Per-call latencies
	Latencies   []time.Duration
	LatencyP50  time.Duration
	LatencyP95  time.Duration
	LatencyP99  time.Duration
	LatencyMin  time.Duration
	LatencyMax  time.Duration
	LatencyMean time.Duration
}

// String returns a human-readable metrics summary.
func (m *BenchmarkMetrics) String() string {
	return fmt.Sprintf(
		"Gas: %d | Duration: %v | MGas/s: %.2f | Calls: %d | P50: %v | P95: %v | P99: %v",
		m.TotalGas, m.Duration, m.MGasPerSecond, m.CallCount,
		m.LatencyP50, m.LatencyP95, m.LatencyP99,
	)
}

// ToDetails returns metrics formatted for Hive test details output.
func (m *BenchmarkMetrics) ToDetails() string {
	return fmt.Sprintf(`
Benchmark Results
=================
Total Gas:     %d
Duration:      %v
MGas/s:        %.2f

Latency Statistics
------------------
Calls:         %d
Min:           %v
Max:           %v
Mean:          %v
P50:           %v
P95:           %v
P99:           %v
`,
		m.TotalGas, m.Duration, m.MGasPerSecond,
		m.CallCount, m.LatencyMin, m.LatencyMax, m.LatencyMean,
		m.LatencyP50, m.LatencyP95, m.LatencyP99)
}

// CallTiming records timing for a single RPC call.
type CallTiming struct {
	Method   string
	Duration time.Duration
	GasUsed  uint64
	Success  bool
	Error    string
}

// IsValid returns true if the call was successful.
func (t *CallTiming) IsValid() bool {
	return t.Success && t.Error == ""
}
