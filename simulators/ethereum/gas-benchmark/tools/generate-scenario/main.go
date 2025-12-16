// Package main generates gas benchmark scenarios from a running Ethereum node.
//
// This tool connects to an Ethereum node's RPC endpoint and extracts block data
// to create benchmark scenarios with real Engine API payloads.
//
// Usage:
//
//	go run ./tools/generate-scenario \
//	    --rpc http://localhost:8545 \
//	    --start 19000000 \
//	    --count 10 \
//	    --output scenarios/mainnet-19m
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/sirupsen/logrus"
)

var (
	rpcURL     = flag.String("rpc", "http://localhost:8545", "Ethereum RPC endpoint")
	startBlock = flag.Int64("start", 0, "Starting block number (0 = latest - count)")
	blockCount = flag.Int("count", 10, "Number of blocks to include")
	outputDir  = flag.String("output", "scenarios/generated", "Output directory for scenario")
	name       = flag.String("name", "", "Scenario name (defaults to output dir name)")
	verbose    = flag.Bool("verbose", false, "Enable verbose logging")
)

// EnginePayload represents an execution payload for the Engine API.
type EnginePayload struct {
	ParentHash    string   `json:"parentHash"`
	FeeRecipient  string   `json:"feeRecipient"`
	StateRoot     string   `json:"stateRoot"`
	ReceiptsRoot  string   `json:"receiptsRoot"`
	LogsBloom     string   `json:"logsBloom"`
	PrevRandao    string   `json:"prevRandao"`
	BlockNumber   string   `json:"blockNumber"`
	GasLimit      string   `json:"gasLimit"`
	GasUsed       string   `json:"gasUsed"`
	Timestamp     string   `json:"timestamp"`
	ExtraData     string   `json:"extraData"`
	BaseFeePerGas string   `json:"baseFeePerGas"`
	BlockHash     string   `json:"blockHash"`
	Transactions  []string `json:"transactions"`
	Withdrawals   []any    `json:"withdrawals"`
	BlobGasUsed   string   `json:"blobGasUsed,omitempty"`
	ExcessBlobGas string   `json:"excessBlobGas,omitempty"`
}

// RPCCall represents a JSON-RPC call.
type RPCCall struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

// ScenarioConfig represents the config.json structure.
type ScenarioConfig struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	BenchmarkPath string `json:"benchmark_path"`
	Config        struct {
		WarmupEnabled    bool `json:"warmup_enabled"`
		WarmupIterations int  `json:"warmup_iterations"`
		TimeoutSeconds   int  `json:"timeout_seconds"`
	} `json:"config"`
}

func main() {
	flag.Parse()

	log := logrus.New()
	log.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})
	if *verbose {
		log.SetLevel(logrus.DebugLevel)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Connect to node
	log.WithField("rpc", *rpcURL).Info("Connecting to Ethereum node")
	client, err := ethclient.DialContext(ctx, *rpcURL)
	if err != nil {
		log.WithError(err).Fatal("Failed to connect to node")
	}
	defer client.Close()

	// Get chain ID for description
	chainID, err := client.ChainID(ctx)
	if err != nil {
		log.WithError(err).Warn("Failed to get chain ID")
		chainID = big.NewInt(0)
	}

	// Determine start block
	start := *startBlock
	if start == 0 {
		latest, err := client.BlockNumber(ctx)
		if err != nil {
			log.WithError(err).Fatal("Failed to get latest block")
		}
		start = int64(latest) - int64(*blockCount)
	}

	log.WithFields(logrus.Fields{
		"start": start,
		"count": *blockCount,
		"chain": chainID,
	}).Info("Generating scenario")

	// Fetch blocks and create payloads
	var calls []RPCCall
	var totalGas uint64
	callID := 1

	for i := 0; i < *blockCount; i++ {
		blockNum := big.NewInt(start + int64(i))

		block, err := client.BlockByNumber(ctx, blockNum)
		if err != nil {
			log.WithError(err).WithField("block", blockNum).Fatal("Failed to fetch block")
		}

		payload, err := blockToPayload(block)
		if err != nil {
			log.WithError(err).WithField("block", blockNum).Fatal("Failed to convert block")
		}

		totalGas += block.GasUsed()

		// Add newPayloadV3 call
		calls = append(calls, RPCCall{
			JSONRPC: "2.0",
			ID:      callID,
			Method:  "engine_newPayloadV3",
			Params: []any{
				payload,
				[]string{},               // versioned hashes
				block.ParentHash().Hex(), // parent beacon block root
			},
		})
		callID++

		// Add forkchoiceUpdated call
		calls = append(calls, RPCCall{
			JSONRPC: "2.0",
			ID:      callID,
			Method:  "engine_forkchoiceUpdatedV3",
			Params: []any{
				map[string]string{
					"headBlockHash":      block.Hash().Hex(),
					"safeBlockHash":      block.Hash().Hex(),
					"finalizedBlockHash": block.ParentHash().Hex(),
				},
				nil,
			},
		})
		callID++

		log.WithFields(logrus.Fields{
			"block": block.NumberU64(),
			"txs":   len(block.Transactions()),
			"gas":   block.GasUsed(),
			"hash":  block.Hash().Hex()[:10] + "...",
		}).Debug("Processed block")
	}

	// Create output directory
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		log.WithError(err).Fatal("Failed to create output directory")
	}

	// Write benchmark.json
	benchmarkPath := filepath.Join(*outputDir, "benchmark.json")
	benchmarkData, err := json.MarshalIndent(calls, "", "  ")
	if err != nil {
		log.WithError(err).Fatal("Failed to marshal benchmark")
	}
	if err := os.WriteFile(benchmarkPath, benchmarkData, 0644); err != nil {
		log.WithError(err).Fatal("Failed to write benchmark.json")
	}

	// Write config.json
	scenarioName := *name
	if scenarioName == "" {
		scenarioName = filepath.Base(*outputDir)
	}

	config := ScenarioConfig{
		Name:          scenarioName,
		Description:   fmt.Sprintf("Benchmark scenario from chain %s, blocks %d-%d", chainID, start, start+int64(*blockCount)-1),
		BenchmarkPath: "benchmark.json",
	}
	config.Config.WarmupEnabled = true
	config.Config.WarmupIterations = 3
	config.Config.TimeoutSeconds = 600

	configPath := filepath.Join(*outputDir, "config.json")
	configData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		log.WithError(err).Fatal("Failed to marshal config")
	}
	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		log.WithError(err).Fatal("Failed to write config.json")
	}

	log.WithFields(logrus.Fields{
		"output":   *outputDir,
		"blocks":   *blockCount,
		"calls":    len(calls),
		"totalGas": totalGas,
		"mgasAvg":  float64(totalGas) / float64(*blockCount) / 1e6,
	}).Info("Scenario generated successfully")

	fmt.Printf("\nScenario created at: %s\n", *outputDir)
	fmt.Printf("Total gas: %d (%.2f Mgas avg per block)\n", totalGas, float64(totalGas)/float64(*blockCount)/1e6)
	fmt.Printf("\nTo use with overlay snapshots, ensure your client-config.yaml has:\n")
	fmt.Printf("  snapshot:\n")
	fmt.Printf("    network: <network-name>\n")
}

func blockToPayload(block *types.Block) (*EnginePayload, error) {
	// Encode transactions as raw RLP hex strings
	txs := make([]string, len(block.Transactions()))
	for i, tx := range block.Transactions() {
		data, err := rlp.EncodeToBytes(tx)
		if err != nil {
			return nil, fmt.Errorf("failed to encode tx %d: %w", i, err)
		}
		txs[i] = hexutil.Encode(data)
	}

	// Encode withdrawals
	withdrawals := make([]any, len(block.Withdrawals()))
	for i, w := range block.Withdrawals() {
		withdrawals[i] = map[string]string{
			"index":          hexutil.EncodeUint64(w.Index),
			"validatorIndex": hexutil.EncodeUint64(w.Validator),
			"address":        w.Address.Hex(),
			"amount":         hexutil.EncodeUint64(w.Amount),
		}
	}

	payload := &EnginePayload{
		ParentHash:    block.ParentHash().Hex(),
		FeeRecipient:  block.Coinbase().Hex(),
		StateRoot:     block.Root().Hex(),
		ReceiptsRoot:  block.ReceiptHash().Hex(),
		LogsBloom:     hexutil.Encode(block.Bloom().Bytes()),
		PrevRandao:    common.BytesToHash(block.MixDigest().Bytes()).Hex(),
		BlockNumber:   hexutil.EncodeUint64(block.NumberU64()),
		GasLimit:      hexutil.EncodeUint64(block.GasLimit()),
		GasUsed:       hexutil.EncodeUint64(block.GasUsed()),
		Timestamp:     hexutil.EncodeUint64(block.Time()),
		ExtraData:     hexutil.Encode(block.Extra()),
		BaseFeePerGas: hexutil.EncodeBig(block.BaseFee()),
		BlockHash:     block.Hash().Hex(),
		Transactions:  txs,
		Withdrawals:   withdrawals,
	}

	// Add blob gas fields if present (post-Cancun)
	if block.BlobGasUsed() != nil {
		payload.BlobGasUsed = hexutil.EncodeUint64(*block.BlobGasUsed())
	}
	if block.ExcessBlobGas() != nil {
		payload.ExcessBlobGas = hexutil.EncodeUint64(*block.ExcessBlobGas())
	}

	return payload, nil
}
