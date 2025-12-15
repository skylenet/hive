// Package main generates chain.rlp snapshot files for benchmark scenarios.
//
// This tool can be used to create pre-built blockchain state from payload files.
// The resulting chain.rlp can be imported by Hive clients at startup.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

var (
	genesisFile = flag.String("genesis", "init/genesis.json", "Genesis file path")
	payloadFile = flag.String("payload", "", "Payload file to process")
	outputFile  = flag.String("output", "chain.rlp", "Output chain.rlp path")
	scenarioDir = flag.String("scenario", "", "Scenario directory to process")
	verbose     = flag.Bool("verbose", false, "Enable verbose logging")
)

// RPCCall represents a JSON-RPC call from the payload file.
type RPCCall struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      int             `json:"id"`
}

func main() {
	flag.Parse()

	log := logrus.New()
	if *verbose {
		log.SetLevel(logrus.DebugLevel)
	} else {
		log.SetLevel(logrus.InfoLevel)
	}

	if *scenarioDir != "" {
		// Process scenario directory
		if err := processScenario(log, *scenarioDir); err != nil {
			log.WithError(err).Fatal("Failed to process scenario")
		}
		return
	}

	if *payloadFile == "" {
		log.Fatal("Either --payload or --scenario is required")
	}

	// Process single payload file
	if err := processPayload(log, *payloadFile, *outputFile); err != nil {
		log.WithError(err).Fatal("Failed to process payload")
	}
}

func processScenario(log *logrus.Logger, scenarioPath string) error {
	log.WithField("scenario", scenarioPath).Info("Processing scenario")

	benchmarkPath := filepath.Join(scenarioPath, "benchmark.json")
	outputPath := filepath.Join(scenarioPath, "chain.rlp")

	if _, err := os.Stat(benchmarkPath); os.IsNotExist(err) {
		return fmt.Errorf("benchmark.json not found in scenario directory")
	}

	return processPayload(log, benchmarkPath, outputPath)
}

func processPayload(log *logrus.Logger, payloadPath, outputPath string) error {
	log.WithFields(logrus.Fields{
		"payload": payloadPath,
		"output":  outputPath,
	}).Info("Processing payload")

	// Read payload file
	data, err := os.ReadFile(payloadPath)
	if err != nil {
		return fmt.Errorf("failed to read payload: %w", err)
	}

	var calls []RPCCall
	if err := json.Unmarshal(data, &calls); err != nil {
		return fmt.Errorf("failed to parse payload: %w", err)
	}

	// Count blocks in payload
	blockCount := 0
	var totalGas uint64
	for _, call := range calls {
		if call.Method == "engine_newPayloadV3" || call.Method == "engine_newPayloadV4" {
			blockCount++

			// Extract gas from payload params
			var params []json.RawMessage
			if err := json.Unmarshal(call.Params, &params); err == nil && len(params) > 0 {
				var payload struct {
					GasUsed string `json:"gasUsed"`
				}
				if err := json.Unmarshal(params[0], &payload); err == nil {
					var gas uint64
					fmt.Sscanf(payload.GasUsed, "0x%x", &gas)
					totalGas += gas
				}
			}
		}
	}

	log.WithFields(logrus.Fields{
		"blocks":   blockCount,
		"totalGas": totalGas,
		"calls":    len(calls),
	}).Info("Payload analysis")

	// For now, create a placeholder chain.rlp
	// In a full implementation, this would:
	// 1. Initialize a blockchain from genesis
	// 2. Execute each block from the payload
	// 3. Export the resulting chain to RLP format
	//
	// This requires running an actual EVM, so we defer to using
	// the hivechain tool or pre-built snapshots for now.

	// Create output directory if needed
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Write placeholder info file
	infoPath := outputPath + ".info"
	info := map[string]interface{}{
		"source":     payloadPath,
		"blocks":     blockCount,
		"total_gas":  totalGas,
		"call_count": len(calls),
		"note":       "chain.rlp generation requires EVM execution - use hivechain tool or pre-built snapshots",
	}

	infoData, _ := json.MarshalIndent(info, "", "  ")
	if err := os.WriteFile(infoPath, infoData, 0644); err != nil {
		return fmt.Errorf("failed to write info file: %w", err)
	}

	log.WithField("info", infoPath).Info("Wrote snapshot info file")
	log.Info("Note: Full chain.rlp generation requires the hivechain tool or pre-built snapshots")

	return nil
}
