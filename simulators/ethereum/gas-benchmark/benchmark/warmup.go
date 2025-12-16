package benchmark

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/hive/simulators/ethereum/gas-benchmark/client"
	"github.com/ethereum/hive/simulators/ethereum/gas-benchmark/payload"
	"github.com/sirupsen/logrus"
)

// WarmupConfig contains configuration for warmup execution.
type WarmupConfig struct {
	// Enabled controls whether warmup is performed.
	Enabled bool
	// Iterations is the number of warmup iterations.
	Iterations int
	// Timeout is the maximum time for all warmup iterations.
	Timeout time.Duration
}

// DefaultWarmupConfig returns sensible defaults for warmup.
func DefaultWarmupConfig() WarmupConfig {
	return WarmupConfig{
		Enabled:    true,
		Iterations: 3,
		Timeout:    5 * time.Minute,
	}
}

// WarmupResult contains the results of warmup execution.
type WarmupResult struct {
	// Executed indicates whether warmup was actually run.
	Executed bool
	// Iterations is the number of iterations completed.
	Iterations int
	// TotalDuration is the total time for all warmup iterations.
	TotalDuration time.Duration
	// IterationDurations contains the duration of each iteration.
	IterationDurations []time.Duration
	// Errors contains any errors encountered during warmup.
	Errors []error
}

// Warmup handles warmup phase execution.
type Warmup interface {
	// Execute runs the warmup phase.
	Execute(ctx context.Context, p *payload.Payload, cfg WarmupConfig) (*WarmupResult, error)
}

// warmup implements Warmup.
type warmup struct {
	log    logrus.FieldLogger
	client client.EngineClient
}

// NewWarmup creates a new warmup executor.
func NewWarmup(log logrus.FieldLogger, engineClient client.EngineClient) Warmup {
	return &warmup{
		log:    log.WithField("component", "warmup"),
		client: engineClient,
	}
}

// Execute runs the warmup phase.
func (w *warmup) Execute(ctx context.Context, p *payload.Payload, cfg WarmupConfig) (*WarmupResult, error) {
	result := &WarmupResult{
		IterationDurations: make([]time.Duration, 0, cfg.Iterations),
		Errors:             make([]error, 0),
	}

	if !cfg.Enabled || p == nil || len(p.Calls) == 0 {
		w.log.Debug("Warmup skipped (disabled or no payload)")
		return result, nil
	}

	// Apply timeout to entire warmup phase.
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	w.log.WithFields(logrus.Fields{
		"iterations": cfg.Iterations,
		"calls":      len(p.Calls),
	}).Info("Starting warmup phase")

	startTime := time.Now()

	for i := 0; i < cfg.Iterations; i++ {
		select {
		case <-ctx.Done():
			result.TotalDuration = time.Since(startTime)
			return result, ctx.Err()
		default:
		}

		iterStart := time.Now()

		w.log.WithField("iteration", i+1).Debug("Starting warmup iteration")

		_, err := w.client.ExecutePayloads(ctx, p)
		iterDuration := time.Since(iterStart)

		result.IterationDurations = append(result.IterationDurations, iterDuration)
		result.Iterations++

		if err != nil {
			w.log.WithError(err).WithField("iteration", i+1).Warn("Warmup iteration failed")
			result.Errors = append(result.Errors, fmt.Errorf("iteration %d: %w", i+1, err))
			// Continue with next iteration despite error.
			continue
		}

		w.log.WithFields(logrus.Fields{
			"iteration": i + 1,
			"duration":  iterDuration,
		}).Debug("Warmup iteration completed")
	}

	result.Executed = true
	result.TotalDuration = time.Since(startTime)

	w.log.WithFields(logrus.Fields{
		"iterations": result.Iterations,
		"duration":   result.TotalDuration,
		"errors":     len(result.Errors),
	}).Info("Warmup phase completed")

	return result, nil
}

// Verify interface compliance.
var _ Warmup = (*warmup)(nil)
