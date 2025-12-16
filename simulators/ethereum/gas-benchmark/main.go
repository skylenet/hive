// Package main implements the gas benchmark simulator for Hive.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ethereum/hive/hivesim"
	"github.com/ethereum/hive/simulators/ethereum/gas-benchmark/benchmark"
	"github.com/ethereum/hive/simulators/ethereum/gas-benchmark/scenario"
	"github.com/sirupsen/logrus"
)

const (
	// scenariosDir is the directory containing benchmark scenarios.
	scenariosDir = "/scenarios"
)

func main() {
	// Initialize logging.
	log := logrus.New()
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// Set log level from environment.
	if level := os.Getenv("HIVE_LOGLEVEL"); level != "" {
		lvl, err := logrus.ParseLevel(level)
		if err == nil {
			log.SetLevel(lvl)
		}
	}

	// Create Hive simulator.
	suite := hivesim.Suite{
		Name:        "gas-benchmark",
		Description: "Ethereum execution client gas processing benchmark suite",
	}

	// Add benchmark test.
	suite.Add(hivesim.TestSpec{
		Name:        "benchmark",
		Description: "Run gas benchmark scenarios against execution clients",
		Run:         runBenchmarks(log),
	})

	// Run the suite.
	hivesim.MustRunSuite(hivesim.New(), suite)
}

// runBenchmarks returns a test function that runs all benchmark scenarios.
func runBenchmarks(log logrus.FieldLogger) func(*hivesim.T) {
	return func(t *hivesim.T) {
		log := log.WithField("test", "benchmark")

		// Generate JWT secret.
		jwtSecret, err := generateJWTSecret()
		if err != nil {
			t.Fatal("Failed to generate JWT secret:", err)
		}

		// Discover scenarios.
		discovery := scenario.NewDiscovery(log)
		scenarios, err := discovery.DiscoverScenarios(scenariosDir)
		if err != nil {
			t.Fatal("Failed to discover scenarios:", err)
		}

		if len(scenarios) == 0 {
			t.Fatal("No benchmark scenarios found in", scenariosDir)
		}

		log.WithField("count", len(scenarios)).Info("Discovered scenarios")

		// Get available clients.
		clients, err := t.Sim.ClientTypes()
		if err != nil {
			t.Fatal("Failed to get client types:", err)
		}
		if len(clients) == 0 {
			t.Fatal("No clients available")
		}

		// Filter to supported clients.
		supportedClients := filterSupportedClients(clients)
		if len(supportedClients) == 0 {
			t.Fatal("No supported execution clients found")
		}

		log.WithField("clients", clientNames(supportedClients)).Info("Found supported clients")

		// Create runner config.
		runnerConfig := benchmark.DefaultRunnerConfig()
		runnerConfig.JWTSecret = jwtSecret

		// Run benchmarks for each scenario and client combination.
		for _, s := range scenarios {
			for _, clientDef := range supportedClients {
				runScenarioBenchmark(t, log, s, clientDef, runnerConfig)
			}
		}
	}
}

// runScenarioBenchmark runs a single scenario against a single client.
func runScenarioBenchmark(t *hivesim.T, log logrus.FieldLogger, s *scenario.Scenario, clientDef *hivesim.ClientDefinition, config benchmark.RunnerConfig) {
	testName := fmt.Sprintf("%s/%s", s.Name, clientDef.Name)

	t.Run(hivesim.TestSpec{
		Name:        testName,
		Description: fmt.Sprintf("Benchmark %s with %s", s.Name, clientDef.Name),
		Run: func(t *hivesim.T) {
			log := log.WithFields(logrus.Fields{
				"scenario": s.Name,
				"client":   clientDef.Name,
			})

			// Create runner.
			runner := benchmark.NewRunner(log, t, config)

			// Run benchmark.
			ctx := context.Background()
			result, err := runner.Run(ctx, s, clientDef)
			if err != nil {
				t.Fatalf("Benchmark execution error: %v", err)
			}

			// Check result.
			if !result.IsValid() {
				if result.Error != nil {
					t.Fatalf("Benchmark failed: %v", result.Error)
				}
				t.Fatal("Benchmark produced invalid results")
			}

			// Log results.
			log.WithFields(logrus.Fields{
				"mgasPerSec":   result.Metrics.MGasPerSecond,
				"duration":     result.Metrics.Duration,
				"totalGas":     result.Metrics.TotalGas,
				"callCount":    result.Metrics.CallCount,
				"latencyP50":   result.Metrics.LatencyP50,
				"latencyP95":   result.Metrics.LatencyP95,
				"latencyP99":   result.Metrics.LatencyP99,
				"snapshotUsed": result.SnapshotUsed,
				"warmupIters":  result.WarmupIters,
			}).Info("Benchmark results")

			// Add details to test output.
			t.Logf("Scenario: %s", s.Name)
			t.Logf("Client: %s", clientDef.Name)
			t.Logf("Snapshot Used: %v", result.SnapshotUsed)
			t.Logf("Warmup Iterations: %d", result.WarmupIters)
			t.Logf("%s", result.Metrics.ToDetails())
		},
	})
}

// generateJWTSecret returns the JWT secret used for Engine API authentication.
// Uses the same hardcoded secret that hive clients use when HIVE_TERMINAL_TOTAL_DIFFICULTY is set.
// This is: 0x7365637265747365637265747365637265747365637265747365637265747365
// which is the hex encoding of "secretsecretsecretsecretsecretse".
func generateJWTSecret() ([]byte, error) {
	// Hardcoded secret matching clients/nethermind/nethermind.sh and other hive clients.
	secret := []byte("secretsecretsecretsecretsecretse")
	return secret, nil
}

// filterSupportedClients filters client definitions to supported execution clients.
func filterSupportedClients(clients []*hivesim.ClientDefinition) []*hivesim.ClientDefinition {
	supported := make([]*hivesim.ClientDefinition, 0, len(clients))

	// List of supported execution client prefixes.
	supportedPrefixes := []string{
		"go-ethereum",
		"geth",
		"besu",
		"nethermind",
		"erigon",
		"reth",
	}

	for _, client := range clients {
		name := strings.ToLower(client.Name)
		for _, prefix := range supportedPrefixes {
			if strings.HasPrefix(name, prefix) || strings.Contains(name, prefix) {
				supported = append(supported, client)
				break
			}
		}
	}

	return supported
}

// clientNames extracts names from client definitions.
func clientNames(clients []*hivesim.ClientDefinition) []string {
	names := make([]string, len(clients))
	for i, c := range clients {
		names[i] = c.Name
	}
	return names
}
