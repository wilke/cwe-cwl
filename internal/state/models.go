// Package state provides MongoDB state management for CWL workflows.
package state

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// WorkflowStatus represents the status of a workflow run.
type WorkflowStatus string

const (
	WorkflowPending   WorkflowStatus = "pending"
	WorkflowRunning   WorkflowStatus = "running"
	WorkflowCompleted WorkflowStatus = "completed"
	WorkflowFailed    WorkflowStatus = "failed"
	WorkflowCancelled WorkflowStatus = "cancelled"
)

// StepStatus represents the status of a step execution.
type StepStatus string

const (
	StepPending   StepStatus = "pending"
	StepRunning   StepStatus = "running"
	StepCompleted StepStatus = "completed"
	StepFailed    StepStatus = "failed"
	StepSkipped   StepStatus = "skipped"
)

// Workflow represents a cached CWL document.
type Workflow struct {
	ID          primitive.ObjectID     `bson:"_id,omitempty" json:"id"`
	WorkflowID  string                 `bson:"workflow_id" json:"workflow_id"`
	ContentHash string                 `bson:"content_hash" json:"content_hash"`
	CWLVersion  string                 `bson:"cwl_version" json:"cwl_version"`
	Document    map[string]interface{} `bson:"document" json:"document"`
	CreatedAt   time.Time              `bson:"created_at" json:"created_at"`
}

// WorkflowRun represents a workflow execution instance.
type WorkflowRun struct {
	ID           string                 `bson:"_id" json:"id"`
	WorkflowID   string                 `bson:"workflow_id" json:"workflow_id"`
	Owner        string                 `bson:"owner" json:"owner"`
	Status       WorkflowStatus         `bson:"status" json:"status"`
	Inputs       map[string]interface{} `bson:"inputs" json:"inputs"`
	Outputs      map[string]interface{} `bson:"outputs,omitempty" json:"outputs,omitempty"`
	OutputPath   string                 `bson:"output_path" json:"output_path"`
	DAGState     *DAGState              `bson:"dag_state,omitempty" json:"dag_state,omitempty"`
	ErrorMessage string                 `bson:"error_message,omitempty" json:"error_message,omitempty"`
	CreatedAt    time.Time              `bson:"created_at" json:"created_at"`
	StartedAt    *time.Time             `bson:"started_at,omitempty" json:"started_at,omitempty"`
	CompletedAt  *time.Time             `bson:"completed_at,omitempty" json:"completed_at,omitempty"`
}

// DAGState represents the serialized DAG state for recovery.
type DAGState struct {
	Nodes map[string]NodeState `bson:"nodes" json:"nodes"`
}

// NodeState represents the serialized state of a DAG node.
type NodeState struct {
	ID           string                 `bson:"id" json:"id"`
	StepID       string                 `bson:"step_id" json:"step_id"`
	ScatterIndex []int                  `bson:"scatter_index,omitempty" json:"scatter_index,omitempty"`
	Status       string                 `bson:"status" json:"status"`
	TaskID       string                 `bson:"task_id,omitempty" json:"task_id,omitempty"`
	Inputs       map[string]interface{} `bson:"inputs,omitempty" json:"inputs,omitempty"`
	Outputs      map[string]interface{} `bson:"outputs,omitempty" json:"outputs,omitempty"`
	Error        string                 `bson:"error,omitempty" json:"error,omitempty"`
}

// StepExecution represents a single step execution (links to BV-BRC Task).
type StepExecution struct {
	ID            primitive.ObjectID     `bson:"_id,omitempty" json:"id"`
	WorkflowRunID string                 `bson:"workflow_run_id" json:"workflow_run_id"`
	StepID        string                 `bson:"step_id" json:"step_id"`
	ScatterIndex  []int                  `bson:"scatter_index,omitempty" json:"scatter_index,omitempty"`
	Status        StepStatus             `bson:"status" json:"status"`
	BVBRCTaskID   int64                  `bson:"bvbrc_task_id,omitempty" json:"bvbrc_task_id,omitempty"`
	Inputs        map[string]interface{} `bson:"inputs,omitempty" json:"inputs,omitempty"`
	Outputs       map[string]interface{} `bson:"outputs,omitempty" json:"outputs,omitempty"`
	ErrorMessage  string                 `bson:"error_message,omitempty" json:"error_message,omitempty"`
	CreatedAt     time.Time              `bson:"created_at" json:"created_at"`
	StartedAt     *time.Time             `bson:"started_at,omitempty" json:"started_at,omitempty"`
	CompletedAt   *time.Time             `bson:"completed_at,omitempty" json:"completed_at,omitempty"`
	RetryCount    int                    `bson:"retry_count" json:"retry_count"`
}

// ContainerMapping maps Docker images to BV-BRC container IDs.
type ContainerMapping struct {
	ID               string    `bson:"_id" json:"id"` // Docker image URI
	BVBRCContainerID string    `bson:"bvbrc_container_id" json:"bvbrc_container_id"`
	Verified         bool      `bson:"verified" json:"verified"`
	CreatedAt        time.Time `bson:"created_at" json:"created_at"`
}

// WorkflowRunSummary is a lightweight summary of a workflow run.
type WorkflowRunSummary struct {
	ID          string         `bson:"_id" json:"id"`
	WorkflowID  string         `bson:"workflow_id" json:"workflow_id"`
	Status      WorkflowStatus `bson:"status" json:"status"`
	Owner       string         `bson:"owner" json:"owner"`
	OutputPath  string         `bson:"output_path" json:"output_path"`
	CreatedAt   time.Time      `bson:"created_at" json:"created_at"`
	CompletedAt *time.Time     `bson:"completed_at,omitempty" json:"completed_at,omitempty"`
	StepCount   int            `bson:"step_count,omitempty" json:"step_count,omitempty"`
	Progress    *RunProgress   `bson:"progress,omitempty" json:"progress,omitempty"`
}

// RunProgress represents workflow execution progress.
type RunProgress struct {
	Total     int `bson:"total" json:"total"`
	Pending   int `bson:"pending" json:"pending"`
	Running   int `bson:"running" json:"running"`
	Completed int `bson:"completed" json:"completed"`
	Failed    int `bson:"failed" json:"failed"`
	Skipped   int `bson:"skipped" json:"skipped"`
}

// StepExecutionSummary is a lightweight summary of a step execution.
type StepExecutionSummary struct {
	StepID       string     `bson:"step_id" json:"step_id"`
	ScatterIndex []int      `bson:"scatter_index,omitempty" json:"scatter_index,omitempty"`
	Status       StepStatus `bson:"status" json:"status"`
	BVBRCTaskID  int64      `bson:"bvbrc_task_id,omitempty" json:"bvbrc_task_id,omitempty"`
	StartedAt    *time.Time `bson:"started_at,omitempty" json:"started_at,omitempty"`
	CompletedAt  *time.Time `bson:"completed_at,omitempty" json:"completed_at,omitempty"`
}

// ValidationResult represents CWL document validation results.
type ValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// FileInfo represents information about a file reference.
type FileInfo struct {
	Class    string `json:"class"`
	Location string `json:"location"`
	Path     string `json:"path,omitempty"`
	Basename string `json:"basename,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Checksum string `json:"checksum,omitempty"`
}

// SubmitRequest represents a workflow submission request.
type SubmitRequest struct {
	Workflow   interface{}            `json:"workflow"`             // CWL document or workflow_id
	Inputs     map[string]interface{} `json:"inputs"`               // Job inputs
	OutputPath string                 `json:"output_path"`          // Output path in Workspace
	Name       string                 `json:"name,omitempty"`       // Optional workflow name
	Tags       []string               `json:"tags,omitempty"`       // Optional tags
}

// SubmitResponse represents the response to a workflow submission.
type SubmitResponse struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
}
