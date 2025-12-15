package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// defaultHTTPClient returns a shared HTTP client for eth_ calls.
func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// newHTTPRequest creates a new JSON-RPC HTTP request.
func newHTTPRequest(ctx context.Context, endpoint string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// Waiter handles waiting for client readiness.
type Waiter interface {
	// WaitForReady waits until the client is ready to accept requests.
	WaitForReady(ctx context.Context) error
	// WaitForChainImport waits until the client has imported the chain to the expected height.
	WaitForChainImport(ctx context.Context, expectedHeight uint64) error
}

// waiter implements Waiter.
type waiter struct {
	log      logrus.FieldLogger
	client   EngineClient
	endpoint string
}

// WaiterConfig contains configuration for the waiter.
type WaiterConfig struct {
	PollInterval   time.Duration
	MaxWaitTime    time.Duration
	ExpectedHeight uint64
}

// DefaultWaiterConfig returns sensible defaults for waiting.
func DefaultWaiterConfig() WaiterConfig {
	return WaiterConfig{
		PollInterval: 500 * time.Millisecond,
		MaxWaitTime:  120 * time.Second,
	}
}

// NewWaiter creates a new client waiter.
func NewWaiter(log logrus.FieldLogger, client EngineClient, endpoint string) Waiter {
	return &waiter{
		log:      log.WithField("component", "waiter"),
		client:   client,
		endpoint: endpoint,
	}
}

// WaitForReady waits until the client is ready to accept requests.
func (w *waiter) WaitForReady(ctx context.Context) error {
	cfg := DefaultWaiterConfig()

	deadline := time.Now().Add(cfg.MaxWaitTime)
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for client readiness after %v", cfg.MaxWaitTime)
			}

			// Try to get the current block number.
			_, err := w.getBlockNumber(ctx)
			if err == nil {
				w.log.Info("Client is ready")
				return nil
			}

			w.log.WithError(err).Debug("Client not ready yet, retrying...")
		}
	}
}

// WaitForChainImport waits until the client has imported the chain to the expected height.
func (w *waiter) WaitForChainImport(ctx context.Context, expectedHeight uint64) error {
	if expectedHeight == 0 {
		return nil
	}

	cfg := DefaultWaiterConfig()
	cfg.MaxWaitTime = 300 * time.Second // Chain import can take longer.

	deadline := time.Now().Add(cfg.MaxWaitTime)
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	w.log.WithField("expectedHeight", expectedHeight).Info("Waiting for chain import")

	var lastHeight uint64

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for chain import (expected: %d, got: %d)", expectedHeight, lastHeight)
			}

			height, err := w.getBlockNumber(ctx)
			if err != nil {
				w.log.WithError(err).Debug("Failed to get block number")
				continue
			}

			lastHeight = height

			if height >= expectedHeight {
				w.log.WithFields(logrus.Fields{
					"expectedHeight": expectedHeight,
					"actualHeight":   height,
				}).Info("Chain import complete")
				return nil
			}

			w.log.WithFields(logrus.Fields{
				"currentHeight":  height,
				"expectedHeight": expectedHeight,
			}).Debug("Chain import in progress")
		}
	}
}

// getBlockNumber retrieves the current block number using eth_blockNumber.
func (w *waiter) getBlockNumber(ctx context.Context) (uint64, error) {
	resp, err := w.doEthCall(ctx, "eth_blockNumber", []any{})
	if err != nil {
		return 0, err
	}

	var result string
	if err := json.Unmarshal(resp, &result); err != nil {
		return 0, fmt.Errorf("failed to unmarshal block number: %w", err)
	}

	var blockNumber uint64
	if _, err := fmt.Sscanf(result, "0x%x", &blockNumber); err != nil {
		return 0, fmt.Errorf("failed to parse block number %s: %w", result, err)
	}

	return blockNumber, nil
}

// doEthCall makes a generic eth_ namespace RPC call.
func (w *waiter) doEthCall(ctx context.Context, method string, params []any) (json.RawMessage, error) {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal params: %w", err)
	}

	req := &jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsJSON,
		ID:      1,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := newHTTPRequest(ctx, w.endpoint, body)
	if err != nil {
		return nil, err
	}

	resp, err := defaultHTTPClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	var rpcResp jsonRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// Verify interface compliance.
var _ Waiter = (*waiter)(nil)
