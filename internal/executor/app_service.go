// Package executor provides step execution implementations.
package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/BV-BRC/cwe-cwl/internal/config"
	"github.com/BV-BRC/cwe-cwl/internal/cwl"
	"github.com/BV-BRC/cwe-cwl/internal/dag"
)

const defaultOutputFile = "cwl_outputs.json"

// AppServiceExecutor executes CWL steps via BV-BRC app_service API.
type AppServiceExecutor struct {
	config *config.Config
	client *AppServiceClient
}

// NewAppServiceExecutor creates a new app_service-based executor.
func NewAppServiceExecutor(cfg *config.Config) *AppServiceExecutor {
	return &AppServiceExecutor{
		config: cfg,
		client: NewAppServiceClient(cfg),
	}
}

// Execute starts execution of a DAG node by submitting a BV-BRC Task via app_service.
func (e *AppServiceExecutor) Execute(ctx context.Context, node *dag.Node) error {
	if node.Tool == nil {
		return fmt.Errorf("node %s has no resolved tool", node.ID)
	}

	params, err := buildTaskParamsForNode(node)
	if err != nil {
		return fmt.Errorf("failed to build task params: %w", err)
	}

	// Get resource requirements
	cores, ramMB, _ := node.Tool.GetResourceRequirements()
	if cores == 0 {
		cores = e.config.Executor.DefaultCPU
	}
	if ramMB == 0 {
		ramMB = e.config.Executor.DefaultMemory
	}

	containerID := resolveContainerID(node.Tool)

	req := SubmitTaskRequest{
		ApplicationID: e.config.BVBRC.CWLStepRunnerID,
		Params:        params,
		ReqCPU:        cores,
		ReqMemory:     ramMB,
		ReqRuntime:    e.config.Executor.DefaultRuntime,
		ContainerID:   containerID,
		OutputPath:    node.OutputPath,
		OutputFile:    defaultOutputFile,
		Owner:         node.Owner,
	}

	taskID, err := e.client.SubmitTask(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to submit task: %w", err)
	}

	node.SetTaskID(fmt.Sprintf("%d", taskID))
	return nil
}

// GetStatus gets the status of a BV-BRC Task via app_service.
func (e *AppServiceExecutor) GetStatus(ctx context.Context, taskID string) (dag.NodeStatus, error) {
	resp, err := e.client.GetTaskStatus(ctx, taskID)
	if err != nil {
		return dag.StatusFailed, err
	}

	return mapStateCode(resp.StateCode), nil
}

// GetOutputs retrieves outputs from a completed BV-BRC Task via app_service.
func (e *AppServiceExecutor) GetOutputs(ctx context.Context, taskID string) (map[string]interface{}, error) {
	resp, err := e.client.GetTaskOutputs(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if resp.Outputs == nil {
		return map[string]interface{}{}, nil
	}
	return resp.Outputs, nil
}

// Cancel cancels a running BV-BRC Task via app_service.
func (e *AppServiceExecutor) Cancel(ctx context.Context, taskID string) error {
	return e.client.CancelTask(ctx, taskID)
}

// buildTaskParamsForNode builds the parameters for CWLStepRunner.
func buildTaskParamsForNode(node *dag.Node) (map[string]interface{}, error) {
	tool := node.Tool

	builder := cwl.NewCommandBuilder(tool, node.Inputs)
	command, err := builder.BuildCommand()
	if err != nil {
		return nil, fmt.Errorf("failed to build command: %w", err)
	}

	outputBindings := make(map[string]interface{})
	for _, out := range tool.Outputs {
		if out.OutputBinding != nil {
			outputBindings[out.ID] = map[string]interface{}{
				"glob":         out.OutputBinding.Glob,
				"loadContents": out.OutputBinding.LoadContents,
				"loadListing":  out.OutputBinding.LoadListing,
				"outputEval":   out.OutputBinding.OutputEval,
			}
		}
	}

	params := map[string]interface{}{
		"cwl_command": command,
		"cwl_inputs":  node.Inputs,
		"cwl_outputs": outputBindings,
		"cwl_step_id": node.StepID,
		"cwl_node_id": node.ID,
	}

	if tool.Stdin != "" {
		params["cwl_stdin"] = tool.Stdin
	}
	if tool.Stdout != "" {
		params["cwl_stdout"] = tool.Stdout
	}
	if tool.Stderr != "" {
		params["cwl_stderr"] = tool.Stderr
	}

	for _, req := range tool.Requirements {
		if req.Class == "EnvVarRequirement" {
			envVars := make(map[string]string)
			for _, env := range req.EnvDef {
				envVars[env.EnvName] = env.EnvValue
			}
			params["cwl_env"] = envVars
		}
	}

	return params, nil
}

func resolveContainerID(tool *cwl.Document) string {
	dockerImage := tool.GetDockerImage()
	if dockerImage == "" {
		return "default"
	}
	return dockerImage
}

func mapStateCode(code string) dag.NodeStatus {
	switch code {
	case "Q", "S":
		return dag.StatusPending
	case "R":
		return dag.StatusRunning
	case "C":
		return dag.StatusCompleted
	case "F", "E", "K":
		return dag.StatusFailed
	default:
		return dag.StatusPending
	}
}

// AppServiceClient is a minimal client for BV-BRC app_service.
type AppServiceClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewAppServiceClient creates a new app_service client.
func NewAppServiceClient(cfg *config.Config) *AppServiceClient {
	timeout := cfg.BVBRC.AppServiceTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &AppServiceClient{
		baseURL: strings.TrimRight(cfg.BVBRC.AppServiceURL, "/"),
		token:   cfg.Auth.ServiceToken,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// SubmitTask submits a task via app_service.
func (c *AppServiceClient) SubmitTask(ctx context.Context, req SubmitTaskRequest) (int64, error) {
	var resp SubmitTaskResponse
	if err := c.doJSON(ctx, http.MethodPost, "/tasks", req, &resp); err != nil {
		return 0, err
	}
	if resp.TaskID == 0 {
		return 0, errors.New("missing task_id in response")
	}
	return resp.TaskID, nil
}

// GetTaskStatus retrieves task status via app_service.
func (c *AppServiceClient) GetTaskStatus(ctx context.Context, taskID string) (*TaskStatusResponse, error) {
	var resp TaskStatusResponse
	if err := c.doJSON(ctx, http.MethodGet, "/tasks/"+taskID, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CancelTask cancels a task via app_service.
func (c *AppServiceClient) CancelTask(ctx context.Context, taskID string) error {
	return c.doJSON(ctx, http.MethodPost, "/tasks/"+taskID+"/cancel", nil, nil)
}

// GetTaskOutputs retrieves task outputs via app_service.
func (c *AppServiceClient) GetTaskOutputs(ctx context.Context, taskID string) (*TaskOutputsResponse, error) {
	var resp TaskOutputsResponse
	if err := c.doJSON(ctx, http.MethodGet, "/tasks/"+taskID+"/outputs", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *AppServiceClient) doJSON(ctx context.Context, method, path string, reqBody interface{}, respBody interface{}) error {
	var body io.Reader
	if reqBody != nil {
		buf, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}

	if c.token != "" {
		req.Header.Set("Authorization", c.token)
	}
	req.Header.Set("Accept", "application/json")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.parseError(resp)
	}

	if respBody == nil {
		return nil
	}

	return json.NewDecoder(resp.Body).Decode(respBody)
}

func (c *AppServiceClient) parseError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	var errResp ErrorResponse
	if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
		return fmt.Errorf("%s: %s", resp.Status, errResp.Error)
	}

	if len(body) > 0 {
		return fmt.Errorf("%s: %s", resp.Status, string(body))
	}
	return fmt.Errorf("%s", resp.Status)
}

// SubmitTaskRequest is the app_service task submission request.
type SubmitTaskRequest struct {
	ApplicationID string                 `json:"application_id"`
	Params        map[string]interface{} `json:"params"`
	ReqCPU        int                    `json:"req_cpu"`
	ReqMemory     int                    `json:"req_memory"`
	ReqRuntime    int                    `json:"req_runtime"`
	ContainerID   string                 `json:"container_id"`
	OutputPath    string                 `json:"output_path,omitempty"`
	OutputFile    string                 `json:"output_file,omitempty"`
	Owner         string                 `json:"owner,omitempty"`
}

// SubmitTaskResponse is the app_service submission response.
type SubmitTaskResponse struct {
	TaskID    int64  `json:"task_id"`
	StateCode string `json:"state_code"`
	Status    string `json:"status"`
}

// TaskStatusResponse is the app_service task status response.
type TaskStatusResponse struct {
	TaskID     int64  `json:"task_id"`
	StateCode  string `json:"state_code"`
	Status     string `json:"status"`
	Owner      string `json:"owner,omitempty"`
	SubmitTime string `json:"submit_time,omitempty"`
	StartTime  string `json:"start_time,omitempty"`
	EndTime    string `json:"end_time,omitempty"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	Error      string `json:"error,omitempty"`
}

// TaskOutputsResponse is the app_service task outputs response.
type TaskOutputsResponse struct {
	TaskID     int64                  `json:"task_id"`
	OutputPath string                 `json:"output_path,omitempty"`
	OutputFile string                 `json:"output_file,omitempty"`
	Outputs    map[string]interface{} `json:"outputs,omitempty"`
}

// ErrorResponse is the app_service error response.
type ErrorResponse struct {
	Error     string `json:"error"`
	Code      string `json:"code,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}
