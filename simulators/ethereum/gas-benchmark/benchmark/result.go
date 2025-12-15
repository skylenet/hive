// Package benchmark provides the core benchmark execution logic.
package benchmark

import (
	"github.com/ethereum/hive/simulators/ethereum/gas-benchmark/metrics"
)

// Result represents the outcome of a benchmark execution.
type Result struct {
	ScenarioName string
	PayloadName  string
	ClientName   string
	Success      bool
	Error        error
	Metrics      *metrics.BenchmarkMetrics
	Logs         []string

	// Snapshot info
	SnapshotUsed bool
	ChainHeight  uint64

	// Warmup info
	WarmupExecuted bool
	WarmupIters    int
}

// IsValid returns true if the benchmark completed successfully.
func (r *Result) IsValid() bool {
	return r.Success && r.Error == nil && r.Metrics != nil
}

// AddLog adds a log entry to the result.
func (r *Result) AddLog(format string, args ...interface{}) {
	if len(args) == 0 {
		r.Logs = append(r.Logs, format)
	} else {
		r.Logs = append(r.Logs, format)
	}
}
