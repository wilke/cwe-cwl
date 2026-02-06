// Package executor provides workflow step execution via BV-BRC App Service.
package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// JSONRPCClient is a client for the BV-BRC App Service JSON-RPC API.
type JSONRPCClient struct {
	endpoint   string
	httpClient *http.Client
	requestID  uint64
}

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      uint64        `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error.
type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *JSONRPCError) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// NewJSONRPCClient creates a new JSON-RPC client for the App Service.
func NewJSONRPCClient(endpoint string) *JSONRPCClient {
	return &JSONRPCClient{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 20 * time.Minute, // Match BV-BRC web client timeout
		},
	}
}

// Call invokes a JSON-RPC method.
func (c *JSONRPCClient) Call(ctx context.Context, token, method string, params []interface{}) (json.RawMessage, error) {
	// Build request
	reqID := atomic.AddUint64(&c.requestID, 1)
	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/jsonrpc+json")
	req.Header.Set("Authorization", token)
	req.Header.Set("Accept", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse JSON-RPC response
	var rpcResp JSONRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for RPC error
	if rpcResp.Error != nil {
		return nil, rpcResp.Error
	}

	return rpcResp.Result, nil
}

// StartParams are optional parameters for starting an app.
type StartParams struct {
	Workspace        string            `json:"workspace,omitempty"`
	BaseURL          string            `json:"base_url,omitempty"`
	ParentID         string            `json:"parent_id,omitempty"`
	ContainerID      string            `json:"container_id,omitempty"`
	UserMetadata     string            `json:"user_metadata,omitempty"`
	Reservation      string            `json:"reservation,omitempty"`
	DataContainerID  string            `json:"data_container_id,omitempty"`
	DisablePreflight int               `json:"disable_preflight,omitempty"`
	PreflightData    map[string]string `json:"preflight_data,omitempty"`
}

// BVBRCTask represents a task returned by the App Service.
type BVBRCTask struct {
	ID             string            `json:"id"`
	ParentID       string            `json:"parent_id,omitempty"`
	App            string            `json:"app"`
	Workspace      string            `json:"workspace"`
	Parameters     map[string]string `json:"parameters"`
	UserID         string            `json:"user_id"`
	Status         string            `json:"status"`
	AWEStatus      string            `json:"awe_status,omitempty"`
	SubmitTime     string            `json:"submit_time"`
	StartTime      string            `json:"start_time,omitempty"`
	CompletedTime  string            `json:"completed_time,omitempty"`
	ElapsedTime    string            `json:"elapsed_time,omitempty"`
	StdoutShockNode string           `json:"stdout_shock_node,omitempty"`
	StderrShockNode string           `json:"stderr_shock_node,omitempty"`
}

// StartApp2 submits a job via AppService.start_app2.
func (c *JSONRPCClient) StartApp2(ctx context.Context, token, appID string, params map[string]string, startParams StartParams) (*BVBRCTask, error) {
	// Build parameters array: [app_id, task_parameters, start_params]
	rpcParams := []interface{}{
		appID,
		params,
		startParams,
	}

	result, err := c.Call(ctx, token, "AppService.start_app2", rpcParams)
	if err != nil {
		return nil, err
	}

	// Result is an array with the task as first element
	var tasks []*BVBRCTask
	if err := json.Unmarshal(result, &tasks); err != nil {
		return nil, fmt.Errorf("failed to parse task: %w", err)
	}

	if len(tasks) == 0 {
		return nil, fmt.Errorf("no task returned")
	}

	return tasks[0], nil
}

// QueryTaskStatus gets the status of a task.
func (c *JSONRPCClient) QueryTaskStatus(ctx context.Context, token, taskID string) (*BVBRCTask, error) {
	result, err := c.Call(ctx, token, "AppService.query_task_status", []interface{}{taskID})
	if err != nil {
		return nil, err
	}

	var task BVBRCTask
	if err := json.Unmarshal(result, &task); err != nil {
		return nil, fmt.Errorf("failed to parse task: %w", err)
	}

	return &task, nil
}

// QueryTaskDetails gets detailed information about a task.
func (c *JSONRPCClient) QueryTaskDetails(ctx context.Context, token, taskID string) (*BVBRCTask, error) {
	result, err := c.Call(ctx, token, "AppService.query_task_details", []interface{}{taskID})
	if err != nil {
		return nil, err
	}

	var task BVBRCTask
	if err := json.Unmarshal(result, &task); err != nil {
		return nil, fmt.Errorf("failed to parse task: %w", err)
	}

	return &task, nil
}

// EnumerateTasks lists tasks for the authenticated user.
func (c *JSONRPCClient) EnumerateTasks(ctx context.Context, token string, offset, count int) ([]*BVBRCTask, error) {
	result, err := c.Call(ctx, token, "AppService.enumerate_tasks", []interface{}{offset, count})
	if err != nil {
		return nil, err
	}

	var tasks []*BVBRCTask
	if err := json.Unmarshal(result, &tasks); err != nil {
		return nil, fmt.Errorf("failed to parse tasks: %w", err)
	}

	return tasks, nil
}
