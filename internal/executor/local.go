package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/BV-BRC/cwe-cwl/internal/cwl"
	"github.com/BV-BRC/cwe-cwl/internal/dag"
)

// LocalExecutor executes CWL steps locally for development/testing.
type LocalExecutor struct {
	workDir string
	tasks   map[string]*localTask
	mu      sync.RWMutex
}

type localTask struct {
	cmd     *exec.Cmd
	status  dag.NodeStatus
	outputs map[string]interface{}
	err     error
}

// NewLocalExecutor creates a new local executor.
func NewLocalExecutor(workDir string) *LocalExecutor {
	return &LocalExecutor{
		workDir: workDir,
		tasks:   make(map[string]*localTask),
	}
}

// Execute starts execution of a DAG node locally.
func (e *LocalExecutor) Execute(ctx context.Context, node *dag.Node) error {
	if node.Tool == nil {
		return fmt.Errorf("node %s has no resolved tool", node.ID)
	}

	// Build command line
	builder := cwl.NewCommandBuilder(node.Tool, node.Inputs)
	command, err := builder.BuildCommand()
	if err != nil {
		return fmt.Errorf("failed to build command: %w", err)
	}

	if len(command) == 0 {
		return fmt.Errorf("empty command for node %s", node.ID)
	}

	// Create work directory for this task
	taskDir := filepath.Join(e.workDir, node.ID)
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		return fmt.Errorf("failed to create task directory: %w", err)
	}

	// Create the command
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = taskDir

	// Set up environment
	cmd.Env = os.Environ()
	for _, req := range node.Tool.Requirements {
		if req.Class == "EnvVarRequirement" {
			for _, env := range req.EnvDef {
				cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", env.EnvName, env.EnvValue))
			}
		}
	}

	// Handle stdin/stdout/stderr
	if node.Tool.Stdout != "" {
		outFile, err := os.Create(filepath.Join(taskDir, node.Tool.Stdout))
		if err != nil {
			return fmt.Errorf("failed to create stdout file: %w", err)
		}
		cmd.Stdout = outFile
	}

	if node.Tool.Stderr != "" {
		errFile, err := os.Create(filepath.Join(taskDir, node.Tool.Stderr))
		if err != nil {
			return fmt.Errorf("failed to create stderr file: %w", err)
		}
		cmd.Stderr = errFile
	}

	// Store task info
	e.mu.Lock()
	task := &localTask{
		cmd:    cmd,
		status: dag.StatusRunning,
	}
	e.tasks[node.ID] = task
	e.mu.Unlock()

	// Set task ID
	node.SetTaskID(node.ID)

	// Start command asynchronously
	go func() {
		err := cmd.Run()

		e.mu.Lock()
		defer e.mu.Unlock()

		if err != nil {
			task.status = dag.StatusFailed
			task.err = err
		} else {
			task.status = dag.StatusCompleted
			// Collect outputs
			task.outputs, _ = e.collectOutputs(taskDir, node.Tool)
		}
	}()

	return nil
}

// collectOutputs collects outputs from a completed task.
func (e *LocalExecutor) collectOutputs(taskDir string, tool *cwl.Document) (map[string]interface{}, error) {
	outputs := make(map[string]interface{})
	evaluator := cwl.NewExpressionEvaluator()

	for _, out := range tool.Outputs {
		if out.OutputBinding == nil {
			continue
		}

		// Evaluate glob patterns
		patterns, err := evaluator.EvaluateGlob(out.OutputBinding.Glob)
		if err != nil {
			continue
		}

		var files []interface{}
		for _, pattern := range patterns {
			matches, err := filepath.Glob(filepath.Join(taskDir, pattern))
			if err != nil {
				continue
			}

			for _, match := range matches {
				info, err := os.Stat(match)
				if err != nil {
					continue
				}

				basename := filepath.Base(match)
				nameroot := strings.TrimSuffix(basename, filepath.Ext(basename))
				nameext := filepath.Ext(basename)

				fileValue := cwl.FileValue{
					Class:    cwl.TypeFile,
					Location: match,
					Path:     match,
					Basename: basename,
					Dirname:  filepath.Dir(match),
					Nameroot: nameroot,
					Nameext:  nameext,
					Size:     info.Size(),
				}

				// Load contents if requested
				if out.OutputBinding.LoadContents {
					contents, err := os.ReadFile(match)
					if err == nil && len(contents) <= 64*1024 { // Max 64KB
						fileValue.Contents = string(contents)
					}
				}

				files = append(files, fileValue)
			}
		}

		// Return single file or array
		parsedType, _ := cwl.ParseType(out.Type)
		if parsedType != nil && parsedType.BaseType() == cwl.TypeArray {
			outputs[out.ID] = files
		} else if len(files) > 0 {
			outputs[out.ID] = files[0]
		}
	}

	return outputs, nil
}

// GetStatus gets the status of a local task.
func (e *LocalExecutor) GetStatus(ctx context.Context, taskID string) (dag.NodeStatus, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	task, ok := e.tasks[taskID]
	if !ok {
		return dag.StatusFailed, fmt.Errorf("task not found: %s", taskID)
	}

	return task.status, nil
}

// GetOutputs retrieves outputs from a completed local task.
func (e *LocalExecutor) GetOutputs(ctx context.Context, taskID string) (map[string]interface{}, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	task, ok := e.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}

	if task.status != dag.StatusCompleted {
		return nil, fmt.Errorf("task not completed: %s", taskID)
	}

	return task.outputs, nil
}

// Cancel cancels a running local task.
func (e *LocalExecutor) Cancel(ctx context.Context, taskID string) error {
	e.mu.RLock()
	task, ok := e.tasks[taskID]
	e.mu.RUnlock()

	if !ok {
		return fmt.Errorf("task not found: %s", taskID)
	}

	if task.cmd != nil && task.cmd.Process != nil {
		return task.cmd.Process.Kill()
	}

	return nil
}
