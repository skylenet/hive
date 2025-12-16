package benchmark

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/hive/hivesim"
	"github.com/ethereum/hive/simulators/ethereum/gas-benchmark/client"
	"github.com/ethereum/hive/simulators/ethereum/gas-benchmark/metrics"
	"github.com/ethereum/hive/simulators/ethereum/gas-benchmark/scenario"
	"github.com/sirupsen/logrus"
)

// RunnerConfig contains configuration for the benchmark runner.
type RunnerConfig struct {
	// JWTSecret is the secret used for Engine API authentication.
	JWTSecret []byte
	// WarmupConfig configures the warmup phase.
	WarmupConfig WarmupConfig
	// Timeout is the maximum time for the entire benchmark.
	Timeout time.Duration
}

// DefaultRunnerConfig returns sensible defaults for the runner.
func DefaultRunnerConfig() RunnerConfig {
	return RunnerConfig{
		WarmupConfig: DefaultWarmupConfig(),
		Timeout:      10 * time.Minute,
	}
}

// Runner executes benchmarks against Ethereum clients.
type Runner interface {
	// Run executes a benchmark scenario against a client.
	Run(ctx context.Context, s *scenario.Scenario, clientDef *hivesim.ClientDefinition) (*Result, error)
}

// runner implements Runner.
type runner struct {
	log        logrus.FieldLogger
	t          *hivesim.T
	config     RunnerConfig
	calculator *metrics.Calculator
}

// NewRunner creates a new benchmark runner.
func NewRunner(log logrus.FieldLogger, t *hivesim.T, config RunnerConfig) Runner {
	return &runner{
		log:        log.WithField("component", "runner"),
		t:          t,
		config:     config,
		calculator: metrics.NewCalculator(),
	}
}

// Run executes a benchmark scenario against a client.
func (r *runner) Run(ctx context.Context, s *scenario.Scenario, clientDef *hivesim.ClientDefinition) (*Result, error) {
	result := &Result{
		ScenarioName: s.Name,
		ClientName:   clientDef.Name,
		Logs:         make([]string, 0),
	}

	// Apply timeout.
	timeout := r.config.Timeout
	if s.Config.TimeoutSeconds > 0 {
		timeout = time.Duration(s.Config.TimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	r.log.WithFields(logrus.Fields{
		"scenario": s.Name,
		"client":   clientDef.Name,
		"timeout":  timeout,
	}).Info("Starting benchmark")

	// Prepare client parameters.
	params := r.prepareClientParams(s)

	// Start client.
	clientInstance, err := r.startClient(ctx, s, clientDef, params)
	if err != nil {
		result.Error = fmt.Errorf("failed to start client: %w", err)
		return result, nil
	}
	defer r.t.Sim.StopClient(r.t.SuiteID, r.t.TestID, clientInstance.Container)

	// Create Engine API client.
	engineEndpoint := fmt.Sprintf("http://%s:8551", clientInstance.IP)
	engineClient := client.NewEngineClient(r.log, engineEndpoint, r.config.JWTSecret)

	// Wait for client readiness.
	ethEndpoint := fmt.Sprintf("http://%s:8545", clientInstance.IP)
	waiter := client.NewWaiter(r.log, engineClient, ethEndpoint)

	if err := waiter.WaitForReady(ctx); err != nil {
		result.Error = fmt.Errorf("client failed to become ready: %w", err)
		return result, nil
	}

	// Wait for chain import if using snapshot.
	if s.HasSnapshot() {
		result.SnapshotUsed = true
		expectedHeight := r.getSnapshotHeight(s)

		if err := waiter.WaitForChainImport(ctx, expectedHeight); err != nil {
			result.Error = fmt.Errorf("chain import failed: %w", err)
			return result, nil
		}
		result.ChainHeight = expectedHeight
	}

	// Execute warmup phase.
	warmupConfig := r.config.WarmupConfig
	if !s.Config.WarmupEnabled {
		warmupConfig.Enabled = false
	} else if s.Config.WarmupIterations > 0 {
		warmupConfig.Iterations = s.Config.WarmupIterations
	}

	if s.WarmupPayload != nil && warmupConfig.Enabled {
		warmupExec := NewWarmup(r.log, engineClient)
		warmupResult, err := warmupExec.Execute(ctx, s.WarmupPayload, warmupConfig)
		if err != nil {
			r.log.WithError(err).Warn("Warmup phase had errors")
		}
		result.WarmupExecuted = warmupResult.Executed
		result.WarmupIters = warmupResult.Iterations
	}

	// Execute benchmark.
	r.log.Info("Starting benchmark measurement")
	benchmarkStart := time.Now()

	timings, err := engineClient.ExecutePayloads(ctx, s.BenchmarkPayload)
	if err != nil {
		result.Error = fmt.Errorf("benchmark execution failed: %w", err)
		return result, nil
	}

	benchmarkDuration := time.Since(benchmarkStart)
	r.log.WithField("duration", benchmarkDuration).Info("Benchmark measurement completed")

	// Calculate metrics.
	result.Metrics = r.calculator.Calculate(timings, s.TotalGas)
	result.Success = true
	result.PayloadName = s.BenchmarkPayload.Name

	r.log.WithFields(logrus.Fields{
		"scenario":     s.Name,
		"client":       clientDef.Name,
		"mgasPerSec":   result.Metrics.MGasPerSecond,
		"duration":     result.Metrics.Duration,
		"latencyP50":   result.Metrics.LatencyP50,
		"latencyP99":   result.Metrics.LatencyP99,
		"snapshotUsed": result.SnapshotUsed,
		"warmupIters":  result.WarmupIters,
	}).Info("Benchmark completed successfully")

	return result, nil
}

// prepareClientParams prepares client startup parameters.
func (r *runner) prepareClientParams(s *scenario.Scenario) hivesim.Params {
	params := hivesim.Params{
		"HIVE_NODETYPE": "full",
		// Enable Engine API by setting TTD (required for post-merge clients).
		"HIVE_TERMINAL_TOTAL_DIFFICULTY": "0",
		// Cancun fork activation (needed for engine_newPayloadV3).
		"HIVE_CANCUN_TIMESTAMP": "0",
	}

	// Add JWT secret.
	if len(r.config.JWTSecret) > 0 {
		params["HIVE_JWTSECRET"] = fmt.Sprintf("%x", r.config.JWTSecret)
	}

	// Add scenario-specific params.
	for k, v := range s.Config.ClientParams {
		params[k] = v
	}

	return params
}

// startClient starts the client with appropriate files.
func (r *runner) startClient(ctx context.Context, s *scenario.Scenario, clientDef *hivesim.ClientDefinition, params hivesim.Params) (*hivesim.Client, error) {
	// Prepare files to upload.
	files := make(map[string]string)

	// Add genesis file.
	if s.HasCustomGenesis() {
		genesisPath := s.FullPath(s.GenesisPath)
		files["/genesis.json"] = genesisPath
	}

	// Add chain.rlp for snapshot (legacy scenario-based snapshots).
	if s.HasSnapshot() {
		chainPath := s.FullPath(s.ChainRLPPath)
		files["/chain.rlp"] = chainPath
		r.log.WithField("path", chainPath).Info("Using chain.rlp snapshot")
	}

	// Build start options.
	opts := []hivesim.StartOption{params, hivesim.WithStaticFiles(files)}

	// Apply client's snapshot config if present (overlay-based snapshots from client-config.yaml).
	if clientDef.HasSnapshot() {
		r.log.WithFields(logrus.Fields{
			"network": clientDef.Snapshot.Network,
			"path":    clientDef.Snapshot.SnapshotContainerPath(),
		}).Info("Using overlay snapshot from client config")

		snapshotOpt := hivesim.WithClientSnapshot(clientDef)
		if snapshotOpt != nil {
			opts = append(opts, snapshotOpt)
		}
	}

	// Start the client.
	client := r.t.StartClient(clientDef.Name, opts...)
	if client == nil {
		return nil, fmt.Errorf("failed to start client %s", clientDef.Name)
	}

	return client, nil
}

// getSnapshotHeight returns the expected chain height from the snapshot.
func (r *runner) getSnapshotHeight(s *scenario.Scenario) uint64 {
	// The snapshot height should be the block before the benchmark starts.
	// This is typically determined by analyzing the benchmark payload.
	if s.BenchmarkPayload == nil {
		return 0
	}

	// Get the first block number from the benchmark payload.
	for i := range s.BenchmarkPayload.Calls {
		call := &s.BenchmarkPayload.Calls[i]
		if call.IsNewPayload() {
			// Parse to get block number.
			// The snapshot should have imported up to blockNumber - 1.
			// For simplicity, return 0 here; the actual implementation
			// would parse the payload to determine the correct height.
			return 0
		}
	}

	return 0
}

// Verify interface compliance.
var _ Runner = (*runner)(nil)
