package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/BV-BRC/cwe-cwl/internal/cwl"
)

// BVBRCExecutor executes CWL steps via BV-BRC App Service.
type BVBRCExecutor struct {
	client       *JSONRPCClient
	appID        string // Application ID for CWL execution (e.g., "CWLStepRunner")
	baseURL      string
	pollInterval time.Duration
}

// BVBRCExecutorConfig configures the BV-BRC executor.
type BVBRCExecutorConfig struct {
	// AppServiceURL is the BV-BRC App Service endpoint.
	AppServiceURL string `mapstructure:"app_service_url"`

	// AppID is the BV-BRC application ID for CWL step execution.
	AppID string `mapstructure:"app_id"`

	// BaseURL is the BV-BRC web base URL.
	BaseURL string `mapstructure:"base_url"`

	// PollInterval is how often to check task status.
	PollInterval time.Duration `mapstructure:"poll_interval"`
}

// DefaultBVBRCExecutorConfig returns sensible defaults.
func DefaultBVBRCExecutorConfig() BVBRCExecutorConfig {
	return BVBRCExecutorConfig{
		AppServiceURL: "https://p3.theseed.org/services/app_service",
		AppID:         "CWLStepRunner",
		BaseURL:       "https://www.bv-brc.org",
		PollInterval:  10 * time.Second,
	}
}

// NewBVBRCExecutor creates a new BV-BRC executor.
func NewBVBRCExecutor(cfg BVBRCExecutorConfig) *BVBRCExecutor {
	return &BVBRCExecutor{
		client:       NewJSONRPCClient(cfg.AppServiceURL),
		appID:        cfg.AppID,
		baseURL:      cfg.BaseURL,
		pollInterval: cfg.PollInterval,
	}
}

// CWLStepParams are the parameters for a CWL step execution.
type CWLStepParams struct {
	// WorkflowRunID is the parent workflow run identifier.
	WorkflowRunID string

	// StepID is the CWL step identifier.
	StepID string

	// NodeID is the unique node identifier (includes scatter index).
	NodeID string

	// Command is the command line to execute.
	Command []string

	// Inputs are the resolved CWL inputs.
	Inputs map[string]interface{}

	// Outputs are the expected CWL outputs.
	Outputs []cwl.Output

	// Resources are the resource requirements (cores, memory in MB).
	Cores  int
	Memory int

	// ContainerSpec is the container specification.
	ContainerSpec *cwl.ContainerSpec

	// OutputPath is the Workspace path for outputs.
	OutputPath string

	// Environment variables to set.
	Environment map[string]string
}

// CWLStepResult is the result of a CWL step execution.
type CWLStepResult struct {
	// TaskID is the BV-BRC task identifier.
	TaskID string

	// Status is the final task status.
	Status string

	// Outputs are the collected CWL outputs.
	Outputs map[string]interface{}

	// StartTime is when execution started.
	StartTime time.Time

	// EndTime is when execution completed.
	EndTime time.Time

	// Error message if failed.
	Error string
}

// SubmitStep submits a CWL step for execution.
func (e *BVBRCExecutor) SubmitStep(ctx context.Context, token string, params CWLStepParams) (string, error) {
	// Serialize complex parameters to JSON strings (App Service requires string values)
	commandJSON, err := json.Marshal(params.Command)
	if err != nil {
		return "", fmt.Errorf("failed to serialize command: %w", err)
	}

	inputsJSON, err := json.Marshal(params.Inputs)
	if err != nil {
		return "", fmt.Errorf("failed to serialize inputs: %w", err)
	}

	outputsJSON, err := json.Marshal(params.Outputs)
	if err != nil {
		return "", fmt.Errorf("failed to serialize outputs: %w", err)
	}

	envJSON, err := json.Marshal(params.Environment)
	if err != nil {
		return "", fmt.Errorf("failed to serialize environment: %w", err)
	}

	// Build task parameters (all string values per App Service spec)
	taskParams := map[string]string{
		"cwl_workflow_run_id": params.WorkflowRunID,
		"cwl_step_id":         params.StepID,
		"cwl_node_id":         params.NodeID,
		"cwl_command":         string(commandJSON),
		"cwl_inputs":          string(inputsJSON),
		"cwl_outputs":         string(outputsJSON),
		"cwl_environment":     string(envJSON),
		"output_path":         params.OutputPath,
		"output_file":         "cwl_outputs.json",
	}

	// Add resource requirements
	if params.Cores > 0 {
		taskParams["req_cpu"] = fmt.Sprintf("%d", params.Cores)
	}
	if params.Memory > 0 {
		taskParams["req_memory"] = fmt.Sprintf("%d", params.Memory)
	}

	// Add container specification
	if params.ContainerSpec != nil {
		taskParams["container_image"] = params.ContainerSpec.Image
		if params.ContainerSpec.Pull != "" {
			taskParams["container_pull"] = params.ContainerSpec.Pull
		}
		if params.ContainerSpec.NeedsGPU {
			taskParams["gpu_required"] = "1"
			taskParams["gpu_count"] = fmt.Sprintf("%d", params.ContainerSpec.GPUCount)
			if params.ContainerSpec.CUDAMinVersion != "" {
				taskParams["cuda_version"] = params.ContainerSpec.CUDAMinVersion
			}
		}
	}

	// Build start parameters
	startParams := StartParams{
		BaseURL: e.baseURL,
	}

	// Submit the task
	task, err := e.client.StartApp2(ctx, token, e.appID, taskParams, startParams)
	if err != nil {
		return "", fmt.Errorf("failed to submit task: %w", err)
	}

	return task.ID, nil
}

// WaitForCompletion polls until a task completes.
func (e *BVBRCExecutor) WaitForCompletion(ctx context.Context, token, taskID string) (*CWLStepResult, error) {
	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case <-ticker.C:
			task, err := e.client.QueryTaskStatus(ctx, token, taskID)
			if err != nil {
				// Transient error, keep polling
				continue
			}

			switch task.Status {
			case "completed":
				return &CWLStepResult{
					TaskID: taskID,
					Status: "completed",
					// Outputs would be fetched from output_path/cwl_outputs.json
				}, nil

			case "failed":
				return &CWLStepResult{
					TaskID: taskID,
					Status: "failed",
					Error:  "Task failed", // Would fetch stderr for details
				}, nil

			case "deleted", "cancelled":
				return &CWLStepResult{
					TaskID: taskID,
					Status: task.Status,
					Error:  "Task was cancelled",
				}, nil

			default:
				// Still running, keep polling
				continue
			}
		}
	}
}

// GetTaskStatus returns the current status of a task.
func (e *BVBRCExecutor) GetTaskStatus(ctx context.Context, token, taskID string) (string, error) {
	task, err := e.client.QueryTaskStatus(ctx, token, taskID)
	if err != nil {
		return "", err
	}
	return task.Status, nil
}

// CancelTask attempts to cancel a running task.
func (e *BVBRCExecutor) CancelTask(ctx context.Context, token, taskID string) error {
	_, err := e.client.Call(ctx, token, "AppService.kill_task", []interface{}{taskID})
	return err
}

// TaskStatusToDAGStatus converts BV-BRC task status to DAG node status.
func TaskStatusToDAGStatus(taskStatus string) string {
	switch taskStatus {
	case "queued", "submitted":
		return "pending"
	case "in-progress", "running":
		return "running"
	case "completed":
		return "completed"
	case "failed":
		return "failed"
	case "deleted", "cancelled":
		return "cancelled"
	default:
		return "unknown"
	}
}
