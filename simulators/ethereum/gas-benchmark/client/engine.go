// Package client provides Engine API client functionality for benchmark execution.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/hive/simulators/ethereum/gas-benchmark/metrics"
	"github.com/ethereum/hive/simulators/ethereum/gas-benchmark/payload"
	"github.com/golang-jwt/jwt/v4"
	"github.com/sirupsen/logrus"
)

// EngineClient defines the interface for Engine API interactions.
type EngineClient interface {
	// NewPayload sends an engine_newPayload request.
	NewPayload(ctx context.Context, exec *payload.ExecutablePayload, method string) (*payload.PayloadStatusV1, time.Duration, error)
	// ForkchoiceUpdated sends an engine_forkchoiceUpdated request.
	ForkchoiceUpdated(ctx context.Context, exec *payload.ExecutablePayload, method string) (*payload.ForkchoiceResponse, time.Duration, error)
	// ExecutePayload executes a single RPC call and returns timing.
	ExecutePayload(ctx context.Context, call *payload.RPCCall) (*metrics.CallTiming, error)
	// ExecutePayloads executes all calls in a payload and returns timings.
	ExecutePayloads(ctx context.Context, p *payload.Payload) ([]metrics.CallTiming, error)
}

// engineClient implements EngineClient.
type engineClient struct {
	log        logrus.FieldLogger
	httpClient *http.Client
	endpoint   string
	jwtSecret  []byte
	parser     *payload.Parser
}

// NewEngineClient creates a new Engine API client.
func NewEngineClient(log logrus.FieldLogger, endpoint string, jwtSecret []byte) EngineClient {
	return &engineClient{
		log:        log.WithField("component", "engine-client"),
		httpClient: &http.Client{Timeout: 120 * time.Second},
		endpoint:   endpoint,
		jwtSecret:  jwtSecret,
		parser:     payload.NewParser(log),
	}
}

// jsonRPCRequest represents a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      int             `json:"id"`
}

// jsonRPCResponse represents a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *jsonRPCError   `json:"error"`
	ID      int             `json:"id"`
}

// jsonRPCError represents a JSON-RPC error.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *engineClient) generateJWT() (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iat": time.Now().Unix(),
	})
	return token.SignedString(e.jwtSecret)
}

func (e *engineClient) doRequest(ctx context.Context, req *jsonRPCRequest) (*jsonRPCResponse, time.Duration, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", e.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Add JWT authentication if secret is provided.
	if len(e.jwtSecret) > 0 {
		jwtToken, err := e.generateJWT()
		if err != nil {
			return nil, 0, fmt.Errorf("failed to generate JWT: %w", err)
		}
		httpReq.Header.Set("Authorization", "Bearer "+jwtToken)
	}

	start := time.Now()
	resp, err := e.httpClient.Do(httpReq)
	duration := time.Since(start)

	if err != nil {
		return nil, duration, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, duration, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, duration, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBody))
	}

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, duration, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, duration, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return &rpcResp, duration, nil
}

// NewPayload sends an engine_newPayload request.
func (e *engineClient) NewPayload(ctx context.Context, exec *payload.ExecutablePayload, method string) (*payload.PayloadStatusV1, time.Duration, error) {
	if exec.ExecutionPayload == nil {
		return nil, 0, fmt.Errorf("execution payload is nil")
	}

	// Build params based on method version.
	params := []any{exec.ExecutionPayload}

	// Add versioned hashes for V3/V4.
	if method == "engine_newPayloadV3" || method == "engine_newPayloadV4" {
		if exec.VersionedHashes != nil {
			params = append(params, exec.VersionedHashes)
		} else {
			params = append(params, []common.Hash{})
		}

		if exec.ParentBeaconRoot != nil {
			params = append(params, exec.ParentBeaconRoot)
		} else {
			params = append(params, common.Hash{})
		}
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to marshal params: %w", err)
	}

	req := &jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsJSON,
		ID:      1,
	}

	resp, duration, err := e.doRequest(ctx, req)
	if err != nil {
		return nil, duration, err
	}

	var status payload.PayloadStatusV1
	if err := json.Unmarshal(resp.Result, &status); err != nil {
		return nil, duration, fmt.Errorf("failed to unmarshal payload status: %w", err)
	}

	e.log.WithFields(logrus.Fields{
		"method":   method,
		"status":   status.Status,
		"duration": duration,
	}).Debug("newPayload completed")

	return &status, duration, nil
}

// ForkchoiceUpdated sends an engine_forkchoiceUpdated request.
func (e *engineClient) ForkchoiceUpdated(ctx context.Context, exec *payload.ExecutablePayload, method string) (*payload.ForkchoiceResponse, time.Duration, error) {
	if exec.ForkchoiceState == nil {
		return nil, 0, fmt.Errorf("forkchoice state is nil")
	}

	params := []any{exec.ForkchoiceState}
	if exec.PayloadAttributes != nil {
		params = append(params, exec.PayloadAttributes)
	} else {
		params = append(params, nil)
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to marshal params: %w", err)
	}

	req := &jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsJSON,
		ID:      1,
	}

	resp, duration, err := e.doRequest(ctx, req)
	if err != nil {
		return nil, duration, err
	}

	var fcResponse payload.ForkchoiceResponse
	if err := json.Unmarshal(resp.Result, &fcResponse); err != nil {
		return nil, duration, fmt.Errorf("failed to unmarshal forkchoice response: %w", err)
	}

	e.log.WithFields(logrus.Fields{
		"method":   method,
		"status":   fcResponse.PayloadStatus.Status,
		"duration": duration,
	}).Debug("forkchoiceUpdated completed")

	return &fcResponse, duration, nil
}

// ExecutePayload executes a single RPC call and returns timing.
func (e *engineClient) ExecutePayload(ctx context.Context, call *payload.RPCCall) (*metrics.CallTiming, error) {
	exec, err := e.parser.ParseExecutablePayload(call)
	if err != nil {
		return nil, fmt.Errorf("failed to parse call: %w", err)
	}

	var duration time.Duration
	var gasUsed uint64

	switch {
	case call.IsNewPayload():
		status, d, err := e.NewPayload(ctx, exec, call.Method)
		if err != nil {
			return nil, fmt.Errorf("newPayload failed: %w", err)
		}
		duration = d

		if status.Status != "VALID" {
			return nil, fmt.Errorf("payload rejected with status: %s", status.Status)
		}

		if exec.ExecutionPayload != nil {
			gasUsed = uint64(exec.ExecutionPayload.GasUsed)
		}

	case call.IsForkchoiceUpdated():
		_, d, err := e.ForkchoiceUpdated(ctx, exec, call.Method)
		if err != nil {
			return nil, fmt.Errorf("forkchoiceUpdated failed: %w", err)
		}
		duration = d

	default:
		return nil, fmt.Errorf("unsupported method: %s", call.Method)
	}

	return &metrics.CallTiming{
		Method:   call.Method,
		Duration: duration,
		GasUsed:  gasUsed,
	}, nil
}

// ExecutePayloads executes all calls in a payload and returns timings.
func (e *engineClient) ExecutePayloads(ctx context.Context, p *payload.Payload) ([]metrics.CallTiming, error) {
	timings := make([]metrics.CallTiming, 0, len(p.Calls))

	for i := range p.Calls {
		call := &p.Calls[i]

		e.log.WithFields(logrus.Fields{
			"index":  i,
			"method": call.Method,
		}).Debug("Executing call")

		timing, err := e.ExecutePayload(ctx, call)
		if err != nil {
			return timings, fmt.Errorf("call %d (%s) failed: %w", i, call.Method, err)
		}

		timings = append(timings, *timing)
	}

	return timings, nil
}

// Verify interface compliance.
var _ EngineClient = (*engineClient)(nil)
