// Package payload provides types and utilities for handling benchmark payloads.
package payload

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

// RPCCall represents a single JSON-RPC call from a payload file.
type RPCCall struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      int             `json:"id"`
}

// Payload represents a complete benchmark payload loaded from a file.
type Payload struct {
	Name        string    // Derived from filename
	Description string    // Optional description
	Path        string    // File path
	Calls       []RPCCall // Parsed RPC calls
	TotalGas    uint64    // Total gas in payload (calculated)
}

// ExecutablePayload contains parsed Engine API parameters ready for execution.
type ExecutablePayload struct {
	// For engine_newPayloadV3/V4
	ExecutionPayload *ExecutionPayloadV3
	VersionedHashes  []common.Hash
	ParentBeaconRoot *common.Hash

	// For engine_forkchoiceUpdatedV3/V4
	ForkchoiceState   *ForkchoiceStateV1
	PayloadAttributes *PayloadAttributesV3
}

// ExecutionPayloadV3 matches the Engine API execution payload structure.
type ExecutionPayloadV3 struct {
	ParentHash    common.Hash         `json:"parentHash"`
	FeeRecipient  common.Address      `json:"feeRecipient"`
	StateRoot     common.Hash         `json:"stateRoot"`
	ReceiptsRoot  common.Hash         `json:"receiptsRoot"`
	LogsBloom     hexutil.Bytes       `json:"logsBloom"`
	PrevRandao    common.Hash         `json:"prevRandao"`
	BlockNumber   hexutil.Uint64      `json:"blockNumber"`
	GasLimit      hexutil.Uint64      `json:"gasLimit"`
	GasUsed       hexutil.Uint64      `json:"gasUsed"`
	Timestamp     hexutil.Uint64      `json:"timestamp"`
	ExtraData     hexutil.Bytes       `json:"extraData"`
	BaseFeePerGas *hexutil.Big        `json:"baseFeePerGas"`
	BlockHash     common.Hash         `json:"blockHash"`
	Transactions  []hexutil.Bytes     `json:"transactions"`
	Withdrawals   []*types.Withdrawal `json:"withdrawals"`
	BlobGasUsed   *hexutil.Uint64     `json:"blobGasUsed"`
	ExcessBlobGas *hexutil.Uint64     `json:"excessBlobGas"`
}

// ForkchoiceStateV1 represents the forkchoice state for Engine API calls.
type ForkchoiceStateV1 struct {
	HeadBlockHash      common.Hash `json:"headBlockHash"`
	SafeBlockHash      common.Hash `json:"safeBlockHash"`
	FinalizedBlockHash common.Hash `json:"finalizedBlockHash"`
}

// PayloadAttributesV3 represents payload attributes for Engine API calls.
type PayloadAttributesV3 struct {
	Timestamp             hexutil.Uint64      `json:"timestamp"`
	PrevRandao            common.Hash         `json:"prevRandao"`
	SuggestedFeeRecipient common.Address      `json:"suggestedFeeRecipient"`
	Withdrawals           []*types.Withdrawal `json:"withdrawals"`
	ParentBeaconBlockRoot *common.Hash        `json:"parentBeaconBlockRoot"`
}

// PayloadStatusV1 represents the response from engine_newPayload.
type PayloadStatusV1 struct {
	Status          string       `json:"status"`
	LatestValidHash *common.Hash `json:"latestValidHash"`
	ValidationError *string      `json:"validationError"`
}

// ForkchoiceResponse represents the response from engine_forkchoiceUpdated.
type ForkchoiceResponse struct {
	PayloadStatus PayloadStatusV1 `json:"payloadStatus"`
	PayloadID     *hexutil.Bytes  `json:"payloadId"`
}

// IsNewPayload returns true if this is a newPayload method call.
func (c *RPCCall) IsNewPayload() bool {
	return c.Method == "engine_newPayloadV3" || c.Method == "engine_newPayloadV4"
}

// IsForkchoiceUpdated returns true if this is a forkchoiceUpdated method call.
func (c *RPCCall) IsForkchoiceUpdated() bool {
	return c.Method == "engine_forkchoiceUpdatedV3" || c.Method == "engine_forkchoiceUpdatedV4"
}

// BlockCount returns the number of blocks in the payload.
func (p *Payload) BlockCount() int {
	count := 0
	for _, call := range p.Calls {
		if call.IsNewPayload() {
			count++
		}
	}
	return count
}
