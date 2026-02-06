// Package client provides a Go client library for the CWL API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is the CWL API client.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// Config holds client configuration.
type Config struct {
	BaseURL string
	Token   string
	Timeout time.Duration
}

// NewClient creates a new CWL API client.
func NewClient(cfg Config) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	return &Client{
		baseURL: cfg.BaseURL,
		token:   cfg.Token,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// SubmitWorkflow submits a workflow for execution.
func (c *Client) SubmitWorkflow(ctx context.Context, req SubmitRequest) (*SubmitResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(ctx, "POST", "/api/v1/workflows", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var result SubmitResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetWorkflow gets workflow status.
func (c *Client) GetWorkflow(ctx context.Context, id string) (*WorkflowStatus, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/workflows/"+id, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var result WorkflowStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ListWorkflows lists workflows.
func (c *Client) ListWorkflows(ctx context.Context, filter ListFilter) ([]WorkflowSummary, error) {
	path := "/api/v1/workflows"
	if filter.Status != "" {
		path += "?status=" + filter.Status
	}

	resp, err := c.doRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var result []WorkflowSummary
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// CancelWorkflow cancels a running workflow.
func (c *Client) CancelWorkflow(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, "DELETE", "/api/v1/workflows/"+id, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.parseError(resp)
	}

	return nil
}

// GetWorkflowOutputs gets outputs from a completed workflow.
func (c *Client) GetWorkflowOutputs(ctx context.Context, id string) (map[string]interface{}, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/workflows/"+id+"/outputs", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// GetWorkflowSteps gets step statuses for a workflow.
func (c *Client) GetWorkflowSteps(ctx context.Context, id string) ([]StepStatus, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/workflows/"+id+"/steps", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var result []StepStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// ValidateCWL validates a CWL document.
func (c *Client) ValidateCWL(ctx context.Context, document interface{}) (*ValidationResult, error) {
	body, err := json.Marshal(map[string]interface{}{"document": document})
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(ctx, "POST", "/api/v1/validate", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result ValidationResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// doRequest makes an authenticated HTTP request.
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}

	if c.token != "" {
		req.Header.Set("Authorization", c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	return c.httpClient.Do(req)
}

// parseError parses an error response.
func (c *Client) parseError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	var errResp struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
		return fmt.Errorf("%s: %s", resp.Status, errResp.Error)
	}

	return fmt.Errorf("%s: %s", resp.Status, string(body))
}

// Request/Response types

// SubmitRequest is the request to submit a workflow.
type SubmitRequest struct {
	Workflow   interface{}            `json:"workflow"`
	Inputs     map[string]interface{} `json:"inputs"`
	OutputPath string                 `json:"output_path,omitempty"`
	Name       string                 `json:"name,omitempty"`
}

// SubmitResponse is the response from workflow submission.
type SubmitResponse struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// WorkflowStatus is the full workflow status.
type WorkflowStatus struct {
	ID           string                 `json:"id"`
	WorkflowID   string                 `json:"workflow_id"`
	Status       string                 `json:"status"`
	Owner        string                 `json:"owner"`
	Inputs       map[string]interface{} `json:"inputs,omitempty"`
	Outputs      map[string]interface{} `json:"outputs,omitempty"`
	OutputPath   string                 `json:"output_path"`
	ErrorMessage string                 `json:"error_message,omitempty"`
	Progress     *Progress              `json:"progress,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	StartedAt    *time.Time             `json:"started_at,omitempty"`
	CompletedAt  *time.Time             `json:"completed_at,omitempty"`
}

// Progress represents workflow execution progress.
type Progress struct {
	Total     int `json:"total"`
	Pending   int `json:"pending"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
}

// WorkflowSummary is a summary of a workflow.
type WorkflowSummary struct {
	ID          string     `json:"id"`
	WorkflowID  string     `json:"workflow_id"`
	Status      string     `json:"status"`
	Owner       string     `json:"owner"`
	OutputPath  string     `json:"output_path"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// StepStatus is the status of a workflow step.
type StepStatus struct {
	StepID       string     `json:"step_id"`
	ScatterIndex []int      `json:"scatter_index,omitempty"`
	Status       string     `json:"status"`
	BVBRCTaskID  int64      `json:"bvbrc_task_id,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}

// ListFilter is the filter for listing workflows.
type ListFilter struct {
	Status     string
	WorkflowID string
	Limit      int
	Offset     int
}

// ValidationResult is the result of CWL validation.
type ValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}
