package scenario

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ethereum/hive/simulators/ethereum/gas-benchmark/payload"
	"github.com/sirupsen/logrus"
)

// Discovery finds and loads benchmark scenarios.
type Discovery struct {
	log    logrus.FieldLogger
	parser *payload.Parser
}

// NewDiscovery creates a new scenario discovery service.
func NewDiscovery(log logrus.FieldLogger) *Discovery {
	return &Discovery{
		log:    log.WithField("component", "scenario-discovery"),
		parser: payload.NewParser(log),
	}
}

// DiscoverScenarios finds all scenarios in a directory.
func (d *Discovery) DiscoverScenarios(baseDir string) ([]*Scenario, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read scenarios directory: %w", err)
	}

	scenarios := make([]*Scenario, 0, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		scenarioDir := filepath.Join(baseDir, entry.Name())
		scenario, err := Load(scenarioDir)
		if err != nil {
			d.log.WithError(err).WithField("dir", scenarioDir).Warn("Failed to load scenario")
			continue
		}

		// Load payloads
		if err := d.loadPayloads(scenario); err != nil {
			d.log.WithError(err).WithField("scenario", scenario.Name).Warn("Failed to load payloads")
			continue
		}

		// Skip scenarios without benchmark payloads
		if scenario.BenchmarkPayload == nil {
			d.log.WithField("scenario", scenario.Name).Debug("Skipping scenario without benchmark payload")
			continue
		}

		d.log.WithFields(logrus.Fields{
			"name":        scenario.Name,
			"hasSnapshot": scenario.HasSnapshot(),
			"hasWarmup":   scenario.HasWarmup(),
			"totalGas":    scenario.TotalGas,
			"blocks":      scenario.BlockCount(),
		}).Info("Discovered scenario")

		scenarios = append(scenarios, scenario)
	}

	// Sort by name for consistent ordering
	sort.Slice(scenarios, func(i, j int) bool {
		return scenarios[i].Name < scenarios[j].Name
	})

	return scenarios, nil
}

func (d *Discovery) loadPayloads(s *Scenario) error {
	// Load benchmark payload (required)
	if s.BenchmarkPath != "" {
		benchmarkPath := s.FullPath(s.BenchmarkPath)
		p, err := d.parser.ParseFile(benchmarkPath)
		if err != nil {
			return fmt.Errorf("failed to parse benchmark payload: %w", err)
		}
		s.BenchmarkPayload = p
		s.TotalGas = p.TotalGas
	}

	// Load warmup payload (optional)
	if s.HasWarmup() {
		warmupPath := s.FullPath(s.WarmupPath)
		p, err := d.parser.ParseFile(warmupPath)
		if err != nil {
			d.log.WithError(err).Warn("Failed to load warmup payload, warmup disabled")
		} else {
			s.WarmupPayload = p
		}
	}

	return nil
}
