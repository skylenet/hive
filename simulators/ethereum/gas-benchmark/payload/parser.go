package payload

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sirupsen/logrus"
)

// Parser handles loading and parsing payload files.
type Parser struct {
	log logrus.FieldLogger
}

// NewParser creates a new payload parser.
func NewParser(log logrus.FieldLogger) *Parser {
	return &Parser{log: log.WithField("component", "parser")}
}

// ParseFile loads and parses a payload file.
func (p *Parser) ParseFile(path string) (*Payload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read payload file: %w", err)
	}

	var calls []RPCCall
	if err := json.Unmarshal(data, &calls); err != nil {
		return nil, fmt.Errorf("failed to parse payload JSON: %w", err)
	}

	payload := &Payload{
		Name:  extractName(path),
		Path:  path,
		Calls: calls,
	}

	// Calculate total gas from newPayload calls
	payload.TotalGas = p.calculateTotalGas(calls)

	return payload, nil
}

// ParseExecutablePayload parses RPC params into an executable format.
func (p *Parser) ParseExecutablePayload(call *RPCCall) (*ExecutablePayload, error) {
	exec := &ExecutablePayload{}

	switch call.Method {
	case "engine_newPayloadV3", "engine_newPayloadV4":
		if err := p.parseNewPayload(call.Params, exec); err != nil {
			return nil, fmt.Errorf("failed to parse newPayload: %w", err)
		}
	case "engine_forkchoiceUpdatedV3", "engine_forkchoiceUpdatedV4":
		if err := p.parseForkchoiceUpdated(call.Params, exec); err != nil {
			return nil, fmt.Errorf("failed to parse forkchoiceUpdated: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported method: %s", call.Method)
	}

	return exec, nil
}

func (p *Parser) parseNewPayload(params json.RawMessage, exec *ExecutablePayload) error {
	// Parse array of [payload, versionedHashes, parentBeaconRoot]
	var rawParams []json.RawMessage
	if err := json.Unmarshal(params, &rawParams); err != nil {
		return fmt.Errorf("failed to unmarshal params array: %w", err)
	}

	if len(rawParams) < 1 {
		return fmt.Errorf("newPayload requires at least 1 parameter")
	}

	// Parse execution payload
	exec.ExecutionPayload = &ExecutionPayloadV3{}
	if err := json.Unmarshal(rawParams[0], exec.ExecutionPayload); err != nil {
		return fmt.Errorf("failed to parse execution payload: %w", err)
	}

	// Parse versioned hashes (optional)
	if len(rawParams) >= 2 && string(rawParams[1]) != "null" {
		if err := json.Unmarshal(rawParams[1], &exec.VersionedHashes); err != nil {
			p.log.WithError(err).Debug("Failed to parse versioned hashes, using empty array")
			exec.VersionedHashes = []common.Hash{}
		}
	}

	// Parse parent beacon root (optional)
	if len(rawParams) >= 3 && string(rawParams[2]) != "null" {
		exec.ParentBeaconRoot = new(common.Hash)
		if err := json.Unmarshal(rawParams[2], exec.ParentBeaconRoot); err != nil {
			p.log.WithError(err).Debug("Failed to parse parent beacon root")
			exec.ParentBeaconRoot = nil
		}
	}

	return nil
}

func (p *Parser) parseForkchoiceUpdated(params json.RawMessage, exec *ExecutablePayload) error {
	var rawParams []json.RawMessage
	if err := json.Unmarshal(params, &rawParams); err != nil {
		return fmt.Errorf("failed to unmarshal params array: %w", err)
	}

	if len(rawParams) < 1 {
		return fmt.Errorf("forkchoiceUpdated requires at least 1 parameter")
	}

	// Parse forkchoice state
	exec.ForkchoiceState = &ForkchoiceStateV1{}
	if err := json.Unmarshal(rawParams[0], exec.ForkchoiceState); err != nil {
		return fmt.Errorf("failed to parse forkchoice state: %w", err)
	}

	// Parse payload attributes (optional)
	if len(rawParams) >= 2 && string(rawParams[1]) != "null" {
		exec.PayloadAttributes = &PayloadAttributesV3{}
		if err := json.Unmarshal(rawParams[1], exec.PayloadAttributes); err != nil {
			p.log.WithError(err).Debug("Failed to parse payload attributes")
			exec.PayloadAttributes = nil
		}
	}

	return nil
}

func (p *Parser) calculateTotalGas(calls []RPCCall) uint64 {
	var total uint64
	for i := range calls {
		call := &calls[i]
		if call.IsNewPayload() {
			exec, err := p.ParseExecutablePayload(call)
			if err == nil && exec.ExecutionPayload != nil {
				total += uint64(exec.ExecutionPayload.GasUsed)
			}
		}
	}
	return total
}

func extractName(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
