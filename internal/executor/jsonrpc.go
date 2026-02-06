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

	// Result is a Task hash
	var task BVBRCTask
	if err := json.Unmarshal(result, &task); err != nil {
		return nil, fmt.Errorf("failed to parse task: %w", err)
	}

	return &task, nil
}

// StartApp submits a job via AppService.start_app (simpler interface).
func (c *JSONRPCClient) StartApp(ctx context.Context, token, appID string, params map[string]string, workspace string) (*BVBRCTask, error) {
	rpcParams := []interface{}{
		appID,
		params,
		workspace,
	}

	result, err := c.Call(ctx, token, "AppService.start_app", rpcParams)
	if err != nil {
		return nil, err
	}

	var task BVBRCTask
	if err := json.Unmarshal(result, &task); err != nil {
		return nil, fmt.Errorf("failed to parse task: %w", err)
	}

	return &task, nil
}

// QueryTasks gets the status of one or more tasks.
// Returns a map of task_id -> Task.
func (c *JSONRPCClient) QueryTasks(ctx context.Context, token string, taskIDs []string) (map[string]*BVBRCTask, error) {
	result, err := c.Call(ctx, token, "AppService.query_tasks", []interface{}{taskIDs})
	if err != nil {
		return nil, err
	}

	var tasks map[string]*BVBRCTask
	if err := json.Unmarshal(result, &tasks); err != nil {
		return nil, fmt.Errorf("failed to parse tasks: %w", err)
	}

	return tasks, nil
}

// QueryTaskStatus gets the status of a single task (convenience wrapper).
func (c *JSONRPCClient) QueryTaskStatus(ctx context.Context, token, taskID string) (*BVBRCTask, error) {
	tasks, err := c.QueryTasks(ctx, token, []string{taskID})
	if err != nil {
		return nil, err
	}

	task, ok := tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("task %s not found", taskID)
	}

	return task, nil
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

// TaskDetails contains detailed execution information.
type TaskDetails struct {
	StdoutURL string `json:"stdout_url,omitempty"`
	StderrURL string `json:"stderr_url,omitempty"`
	PID       int    `json:"pid,omitempty"`
	Hostname  string `json:"hostname,omitempty"`
	ExitCode  int    `json:"exitcode,omitempty"`
}

// GetTaskDetails gets detailed execution information about a task.
func (c *JSONRPCClient) GetTaskDetails(ctx context.Context, token, taskID string) (*TaskDetails, error) {
	result, err := c.Call(ctx, token, "AppService.query_task_details", []interface{}{taskID})
	if err != nil {
		return nil, err
	}

	var details TaskDetails
	if err := json.Unmarshal(result, &details); err != nil {
		return nil, fmt.Errorf("failed to parse task details: %w", err)
	}

	return &details, nil
}

// QueryTaskSummary returns task counts by status for the authenticated user.
func (c *JSONRPCClient) QueryTaskSummary(ctx context.Context, token string) (map[string]int, error) {
	result, err := c.Call(ctx, token, "AppService.query_task_summary", []interface{}{})
	if err != nil {
		return nil, err
	}

	var summary map[string]int
	if err := json.Unmarshal(result, &summary); err != nil {
		return nil, fmt.Errorf("failed to parse summary: %w", err)
	}

	return summary, nil
}

// KillTaskResult contains the result of killing a task.
type KillTaskResult struct {
	Killed int    `json:"killed"`
	Msg    string `json:"msg"`
}

// KillTask cancels a single task.
func (c *JSONRPCClient) KillTask(ctx context.Context, token, taskID string) (*KillTaskResult, error) {
	result, err := c.Call(ctx, token, "AppService.kill_task", []interface{}{taskID})
	if err != nil {
		return nil, err
	}

	// Result is [killed, msg] tuple
	var tuple []interface{}
	if err := json.Unmarshal(result, &tuple); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	if len(tuple) < 2 {
		return nil, fmt.Errorf("unexpected result format")
	}

	killed, _ := tuple[0].(float64)
	msg, _ := tuple[1].(string)

	return &KillTaskResult{
		Killed: int(killed),
		Msg:    msg,
	}, nil
}

// KillTasks cancels multiple tasks.
func (c *JSONRPCClient) KillTasks(ctx context.Context, token string, taskIDs []string) (map[string]*KillTaskResult, error) {
	result, err := c.Call(ctx, token, "AppService.kill_tasks", []interface{}{taskIDs})
	if err != nil {
		return nil, err
	}

	var results map[string]*KillTaskResult
	if err := json.Unmarshal(result, &results); err != nil {
		return nil, fmt.Errorf("failed to parse results: %w", err)
	}

	return results, nil
}

// RerunTask resubmits a failed task.
func (c *JSONRPCClient) RerunTask(ctx context.Context, token, taskID string) (*BVBRCTask, error) {
	result, err := c.Call(ctx, token, "AppService.rerun_task", []interface{}{taskID})
	if err != nil {
		return nil, err
	}

	var task BVBRCTask
	if err := json.Unmarshal(result, &task); err != nil {
		return nil, fmt.Errorf("failed to parse task: %w", err)
	}

	return &task, nil
}

// BVBRCApp represents an application definition.
type BVBRCApp struct {
	ID          string         `json:"id"`
	Script      string         `json:"script,omitempty"`
	Label       string         `json:"label"`
	Description string         `json:"description,omitempty"`
	Parameters  []AppParameter `json:"parameters,omitempty"`
}

// AppParameter represents an application parameter definition.
type AppParameter struct {
	ID       string `json:"id"`
	Label    string `json:"label,omitempty"`
	Required int    `json:"required"`
	Default  string `json:"default,omitempty"`
	Desc     string `json:"desc,omitempty"`
	Type     string `json:"type"`
	Enum     string `json:"enum,omitempty"`
	WSType   string `json:"wstype,omitempty"`
}

// EnumerateApps lists all available applications.
func (c *JSONRPCClient) EnumerateApps(ctx context.Context, token string) ([]*BVBRCApp, error) {
	result, err := c.Call(ctx, token, "AppService.enumerate_apps", []interface{}{})
	if err != nil {
		return nil, err
	}

	var apps []*BVBRCApp
	if err := json.Unmarshal(result, &apps); err != nil {
		return nil, fmt.Errorf("failed to parse apps: %w", err)
	}

	return apps, nil
}

// ServiceStatus returns the service availability status.
func (c *JSONRPCClient) ServiceStatus(ctx context.Context, token string) (bool, string, error) {
	result, err := c.Call(ctx, token, "AppService.service_status", []interface{}{})
	if err != nil {
		return false, "", err
	}

	// Result is [submission_enabled, status_message] tuple
	var tuple []interface{}
	if err := json.Unmarshal(result, &tuple); err != nil {
		return false, "", fmt.Errorf("failed to parse result: %w", err)
	}

	if len(tuple) < 2 {
		return false, "", fmt.Errorf("unexpected result format")
	}

	enabled, _ := tuple[0].(float64)
	msg, _ := tuple[1].(string)

	return enabled == 1, msg, nil
}
