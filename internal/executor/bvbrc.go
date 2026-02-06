package executor

import (
	"context"
	"fmt"
	"time"

	"github.com/BV-BRC/cwe-cwl/internal/bvbrc"
)

// BVBRCExecutor executes CWL jobs via BV-BRC App Service.
type BVBRCExecutor struct {
	client       *JSONRPCClient
	appID        string // Application ID for CWL execution (e.g., "CWLRunner")
	baseURL      string
	pollInterval time.Duration
}

// BVBRCExecutorConfig configures the BV-BRC executor.
type BVBRCExecutorConfig struct {
	// AppServiceURL is the BV-BRC App Service endpoint.
	AppServiceURL string `mapstructure:"app_service_url"`

	// AppID is the BV-BRC application ID for CWL job execution.
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
		AppID:         "CWLRunner",
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

// CWLJobResult is the result of a CWL job execution.
type CWLJobResult struct {
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

// SubmitJob submits a CWL job spec for execution.
// The job spec contains the CWL tool document and inputs.
func (e *BVBRCExecutor) SubmitJob(ctx context.Context, token string, jobSpec *bvbrc.CWLJobSpec) (string, error) {
	// Serialize the job spec to JSON
	jobJSON, err := jobSpec.ToJSON()
	if err != nil {
		return "", fmt.Errorf("failed to serialize job spec: %w", err)
	}

	// The CWL tool document IS the spec - pass it as the job document
	taskParams := map[string]string{
		"cwl_job_spec": string(jobJSON),
		"output_path":  jobSpec.OutputPath,
	}

	if jobSpec.OutputFile != "" {
		taskParams["output_file"] = jobSpec.OutputFile
	}

	// Extract resource requirements from the CWL tool
	cpu, memoryMB := jobSpec.GetResourceRequirements()
	if cpu > 0 {
		taskParams["req_cpu"] = fmt.Sprintf("%d", cpu)
	}
	if memoryMB > 0 {
		taskParams["req_memory"] = fmt.Sprintf("%d", memoryMB)
	}

	// Extract container from the CWL tool
	containerID := jobSpec.GetContainerID()
	if containerID != "" {
		taskParams["container_id"] = containerID
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
func (e *BVBRCExecutor) WaitForCompletion(ctx context.Context, token, taskID string) (*CWLJobResult, error) {
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
				return &CWLJobResult{
					TaskID: taskID,
					Status: "completed",
					// Outputs would be fetched from output_path
				}, nil

			case "failed":
				return &CWLJobResult{
					TaskID: taskID,
					Status: "failed",
					Error:  "Task failed",
				}, nil

			case "deleted", "cancelled":
				return &CWLJobResult{
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
