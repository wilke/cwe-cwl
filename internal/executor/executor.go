// Package executor provides step execution implementations.
package executor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/BV-BRC/cwe-cwl/internal/config"
	"github.com/BV-BRC/cwe-cwl/internal/cwl"
	"github.com/BV-BRC/cwe-cwl/internal/dag"
	"github.com/redis/go-redis/v9"
)

// BVBRCExecutor executes CWL steps as BV-BRC Tasks.
type BVBRCExecutor struct {
	config      *config.Config
	db          *sql.DB
	redis       *redis.Client
	containerMap map[string]string // Docker image -> BV-BRC container ID
}

// NewBVBRCExecutor creates a new BV-BRC executor.
func NewBVBRCExecutor(cfg *config.Config, db *sql.DB, redisClient *redis.Client) *BVBRCExecutor {
	return &BVBRCExecutor{
		config:       cfg,
		db:           db,
		redis:        redisClient,
		containerMap: make(map[string]string),
	}
}

// Execute starts execution of a DAG node by creating a BV-BRC Task.
func (e *BVBRCExecutor) Execute(ctx context.Context, node *dag.Node) error {
	if node.Tool == nil {
		return fmt.Errorf("node %s has no resolved tool", node.ID)
	}

	// Build task parameters
	params, err := e.buildTaskParams(node)
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

	// Resolve container
	containerID, err := e.resolveContainer(ctx, node.Tool)
	if err != nil {
		return fmt.Errorf("failed to resolve container: %w", err)
	}

	// Create BV-BRC Task
	taskID, err := e.createTask(ctx, TaskParams{
		ApplicationID: e.config.BVBRC.CWLStepRunnerID,
		Owner:         "", // Will be set from workflow run
		Params:        params,
		ReqCPU:        cores,
		ReqMemory:     ramMB,
		ReqRuntime:    e.config.Executor.DefaultRuntime,
		ContainerID:   containerID,
	})
	if err != nil {
		return fmt.Errorf("failed to create task: %w", err)
	}

	// Store task ID in node
	node.SetTaskID(fmt.Sprintf("%d", taskID))

	// Publish to Redis to trigger scheduler
	if err := e.publishTaskSubmission(ctx, taskID); err != nil {
		// Log but don't fail - scheduler will pick it up
		fmt.Printf("Warning: failed to publish task submission: %v\n", err)
	}

	return nil
}

// TaskParams holds parameters for creating a BV-BRC Task.
type TaskParams struct {
	ApplicationID string
	Owner         string
	Params        map[string]interface{}
	ReqCPU        int
	ReqMemory     int
	ReqRuntime    int
	ContainerID   string
	OutputPath    string
	OutputFile    string
}

// buildTaskParams builds the parameters for CWLStepRunner.
func (e *BVBRCExecutor) buildTaskParams(node *dag.Node) (map[string]interface{}, error) {
	tool := node.Tool

	// Build command line
	builder := cwl.NewCommandBuilder(tool, node.Inputs)
	command, err := builder.BuildCommand()
	if err != nil {
		return nil, fmt.Errorf("failed to build command: %w", err)
	}

	// Collect output bindings
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
		"cwl_command":       command,
		"cwl_inputs":        node.Inputs,
		"cwl_outputs":       outputBindings,
		"cwl_step_id":       node.StepID,
		"cwl_node_id":       node.ID,
	}

	// Add stdin/stdout/stderr if specified
	if tool.Stdin != "" {
		params["cwl_stdin"] = tool.Stdin
	}
	if tool.Stdout != "" {
		params["cwl_stdout"] = tool.Stdout
	}
	if tool.Stderr != "" {
		params["cwl_stderr"] = tool.Stderr
	}

	// Add environment variables
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

// resolveContainer resolves a Docker image to a BV-BRC container ID.
func (e *BVBRCExecutor) resolveContainer(ctx context.Context, tool *cwl.Document) (string, error) {
	dockerImage := tool.GetDockerImage()
	if dockerImage == "" {
		// Use default container
		return "default", nil
	}

	// Check cache
	if containerID, ok := e.containerMap[dockerImage]; ok {
		return containerID, nil
	}

	// Query container mapping from database
	var containerID string
	err := e.db.QueryRowContext(ctx,
		"SELECT bvbrc_container_id FROM container_mappings WHERE docker_image = $1",
		dockerImage,
	).Scan(&containerID)

	if err == sql.ErrNoRows {
		// No mapping found - return the Docker image as container ID
		// BV-BRC may be able to pull it directly
		return dockerImage, nil
	}
	if err != nil {
		return "", err
	}

	// Cache the mapping
	e.containerMap[dockerImage] = containerID

	return containerID, nil
}

// createTask creates a BV-BRC Task in the database.
func (e *BVBRCExecutor) createTask(ctx context.Context, params TaskParams) (int64, error) {
	paramsJSON, err := json.Marshal(params.Params)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal params: %w", err)
	}

	var taskID int64
	err = e.db.QueryRowContext(ctx, `
		INSERT INTO Task (
			owner, state_code, application_id, submit_time,
			params, req_memory, req_cpu, req_runtime,
			container_id, output_path, output_file
		) VALUES ($1, 'Q', $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id
	`,
		params.Owner,
		params.ApplicationID,
		time.Now(),
		string(paramsJSON),
		params.ReqMemory,
		params.ReqCPU,
		params.ReqRuntime,
		params.ContainerID,
		params.OutputPath,
		params.OutputFile,
	).Scan(&taskID)

	if err != nil {
		return 0, fmt.Errorf("failed to insert task: %w", err)
	}

	return taskID, nil
}

// publishTaskSubmission publishes a task submission event to Redis.
func (e *BVBRCExecutor) publishTaskSubmission(ctx context.Context, taskID int64) error {
	event := map[string]interface{}{
		"type":    "task_submitted",
		"task_id": taskID,
		"time":    time.Now().Unix(),
	}

	eventJSON, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return e.redis.Publish(ctx, "task_submission", string(eventJSON)).Err()
}

// GetStatus gets the status of a BV-BRC Task.
func (e *BVBRCExecutor) GetStatus(ctx context.Context, taskID string) (dag.NodeStatus, error) {
	var stateCode string
	err := e.db.QueryRowContext(ctx,
		"SELECT state_code FROM Task WHERE id = $1",
		taskID,
	).Scan(&stateCode)

	if err == sql.ErrNoRows {
		return dag.StatusFailed, fmt.Errorf("task not found: %s", taskID)
	}
	if err != nil {
		return dag.StatusFailed, err
	}

	// Map BV-BRC state codes to DAG node status
	switch stateCode {
	case "Q", "S": // Queued, Scheduled
		return dag.StatusPending, nil
	case "R": // Running
		return dag.StatusRunning, nil
	case "C": // Completed
		return dag.StatusCompleted, nil
	case "F", "E": // Failed, Error
		return dag.StatusFailed, nil
	default:
		return dag.StatusPending, nil
	}
}

// GetOutputs retrieves outputs from a completed BV-BRC Task.
func (e *BVBRCExecutor) GetOutputs(ctx context.Context, taskID string) (map[string]interface{}, error) {
	var outputPath string
	err := e.db.QueryRowContext(ctx,
		"SELECT output_path FROM Task WHERE id = $1",
		taskID,
	).Scan(&outputPath)

	if err != nil {
		return nil, fmt.Errorf("failed to get task output path: %w", err)
	}

	// Read outputs from cwl_outputs.json in the task output directory
	// This would typically be in Workspace
	outputs := make(map[string]interface{})

	// TODO: Implement reading from Workspace
	// For now, return empty outputs
	return outputs, nil
}

// Cancel cancels a running BV-BRC Task.
func (e *BVBRCExecutor) Cancel(ctx context.Context, taskID string) error {
	_, err := e.db.ExecContext(ctx,
		"UPDATE Task SET state_code = 'K' WHERE id = $1 AND state_code IN ('Q', 'S', 'R')",
		taskID,
	)
	return err
}

// SetOwner sets the owner for tasks created by this executor.
func (e *BVBRCExecutor) SetOwner(owner string) {
	// Owner is set per-task, not globally
}

// SetOutputPath sets the output path for tasks.
func (e *BVBRCExecutor) SetOutputPath(path string) {
	// Output path is set per-task
}
