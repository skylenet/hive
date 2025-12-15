// Package scenario provides types and utilities for managing benchmark scenarios.
package scenario

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/ethereum/hive/simulators/ethereum/gas-benchmark/payload"
)

// Scenario represents a complete benchmark scenario with state and payloads.
type Scenario struct {
	Name        string `json:"name"`
	Description string `json:"description"`

	// Paths (relative to scenario directory)
	GenesisPath   string `json:"genesis_path"`
	ChainRLPPath  string `json:"chain_rlp_path"`
	BenchmarkPath string `json:"benchmark_path"`
	WarmupPath    string `json:"warmup_path"`

	// Configuration
	Config Config `json:"config"`

	// Loaded payloads (populated at runtime)
	BenchmarkPayload *payload.Payload `json:"-"`
	WarmupPayload    *payload.Payload `json:"-"`

	// Computed values
	TotalGas uint64 `json:"total_gas"`

	// Base directory
	BaseDir string `json:"-"`
}

// Config contains benchmark configuration for a scenario.
type Config struct {
	// Warmup configuration
	WarmupEnabled    bool `json:"warmup_enabled"`
	WarmupIterations int  `json:"warmup_iterations"`

	// Timing configuration
	TimeoutSeconds int `json:"timeout_seconds"`

	// Client configuration
	ClientParams map[string]string `json:"client_params"`
}

// DefaultConfig returns a default scenario configuration.
func DefaultConfig() Config {
	return Config{
		WarmupEnabled:    true,
		WarmupIterations: 3,
		TimeoutSeconds:   600, // 10 minutes
		ClientParams:     make(map[string]string),
	}
}

// Load loads a scenario from a directory.
func Load(dir string) (*Scenario, error) {
	configPath := filepath.Join(dir, "config.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		// Try to infer scenario from directory contents
		return inferScenario(dir)
	}

	var s Scenario
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}

	s.BaseDir = dir

	// Set defaults for missing config values
	if s.Config.TimeoutSeconds == 0 {
		s.Config.TimeoutSeconds = 600
	}
	if s.Config.ClientParams == nil {
		s.Config.ClientParams = make(map[string]string)
	}

	return &s, nil
}

// inferScenario creates a scenario from directory contents when no config.json exists.
func inferScenario(dir string) (*Scenario, error) {
	s := &Scenario{
		Name:    filepath.Base(dir),
		BaseDir: dir,
		Config:  DefaultConfig(),
	}

	// Look for standard files
	if exists(filepath.Join(dir, "chain.rlp")) {
		s.ChainRLPPath = "chain.rlp"
	}
	if exists(filepath.Join(dir, "benchmark.json")) {
		s.BenchmarkPath = "benchmark.json"
	}
	if exists(filepath.Join(dir, "warmup.json")) {
		s.WarmupPath = "warmup.json"
	}
	if exists(filepath.Join(dir, "genesis.json")) {
		s.GenesisPath = "genesis.json"
	}

	return s, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// FullPath returns the full path for a scenario file.
func (s *Scenario) FullPath(relativePath string) string {
	if relativePath == "" {
		return ""
	}
	return filepath.Join(s.BaseDir, relativePath)
}

// HasSnapshot returns true if the scenario has a chain.rlp snapshot.
func (s *Scenario) HasSnapshot() bool {
	return s.ChainRLPPath != "" && exists(s.FullPath(s.ChainRLPPath))
}

// HasWarmup returns true if the scenario has a warmup payload.
func (s *Scenario) HasWarmup() bool {
	return s.WarmupPath != "" && exists(s.FullPath(s.WarmupPath))
}

// HasCustomGenesis returns true if the scenario has a custom genesis file.
func (s *Scenario) HasCustomGenesis() bool {
	return s.GenesisPath != "" && exists(s.FullPath(s.GenesisPath))
}

// BlockCount returns the number of blocks in the benchmark payload.
func (s *Scenario) BlockCount() int {
	if s.BenchmarkPayload == nil {
		return 0
	}
	return s.BenchmarkPayload.BlockCount()
}
