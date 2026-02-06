// cwl-step-runner executes a single CWL CommandLineTool step.
// This is the registered BV-BRC application that runs inside SLURM jobs.
//
// It receives parameters from the BV-BRC task system and:
// 1. Executes the specified command
// 2. Collects outputs per CWL output bindings
// 3. Writes results to cwl_outputs.json
//
// Parameters are passed via environment variables or a JSON params file.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BV-BRC/cwe-cwl/internal/cwl"
)

// StepParams are the parameters passed to cwl-step-runner.
type StepParams struct {
	// Command is the command line to execute.
	Command []string `json:"cwl_command"`

	// Inputs are the resolved input values.
	Inputs map[string]interface{} `json:"cwl_inputs"`

	// Outputs are the output binding specifications.
	Outputs []OutputBinding `json:"cwl_outputs"`

	// Environment variables to set.
	Environment map[string]string `json:"cwl_environment"`

	// Stdin file path (optional).
	Stdin string `json:"cwl_stdin,omitempty"`

	// Stdout file path (optional).
	Stdout string `json:"cwl_stdout,omitempty"`

	// Stderr file path (optional).
	Stderr string `json:"cwl_stderr,omitempty"`

	// WorkDir is the working directory.
	WorkDir string `json:"cwl_workdir,omitempty"`

	// StepID for logging.
	StepID string `json:"cwl_step_id,omitempty"`

	// NodeID for logging.
	NodeID string `json:"cwl_node_id,omitempty"`
}

// OutputBinding specifies how to collect an output.
type OutputBinding struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Glob         string `json:"glob,omitempty"`
	LoadContents bool   `json:"loadContents,omitempty"`
	LoadListing  string `json:"loadListing,omitempty"`
	OutputEval   string `json:"outputEval,omitempty"`
}

// StepResult is written to cwl_outputs.json.
type StepResult struct {
	Status   string                 `json:"status"`
	ExitCode int                    `json:"exit_code"`
	Outputs  map[string]interface{} `json:"outputs"`
	Error    string                 `json:"error,omitempty"`
}

func main() {
	// Load parameters
	params, err := loadParams()
	if err != nil {
		writeError(fmt.Sprintf("failed to load parameters: %v", err))
		os.Exit(1)
	}

	// Validate parameters
	if len(params.Command) == 0 {
		writeError("no command specified")
		os.Exit(1)
	}

	// Set up working directory
	workDir := params.WorkDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	// Execute the command
	exitCode, err := executeCommand(params, workDir)
	if err != nil {
		writeResult(StepResult{
			Status:   "failed",
			ExitCode: exitCode,
			Error:    err.Error(),
		})
		os.Exit(exitCode)
	}

	// Collect outputs
	outputs, err := collectOutputs(params.Outputs, params.Inputs, workDir)
	if err != nil {
		writeResult(StepResult{
			Status:   "failed",
			ExitCode: exitCode,
			Error:    fmt.Sprintf("failed to collect outputs: %v", err),
		})
		os.Exit(1)
	}

	// Write success result
	writeResult(StepResult{
		Status:   "completed",
		ExitCode: exitCode,
		Outputs:  outputs,
	})
}

// loadParams loads step parameters from environment or file.
func loadParams() (*StepParams, error) {
	var params StepParams

	// Try loading from params file first (BV-BRC standard)
	paramsFile := os.Getenv("CWL_PARAMS_FILE")
	if paramsFile == "" {
		paramsFile = "cwl_params.json"
	}

	if data, err := os.ReadFile(paramsFile); err == nil {
		if err := json.Unmarshal(data, &params); err != nil {
			return nil, fmt.Errorf("failed to parse params file: %w", err)
		}
		return &params, nil
	}

	// Fall back to environment variables
	if cmdJSON := os.Getenv("CWL_COMMAND"); cmdJSON != "" {
		if err := json.Unmarshal([]byte(cmdJSON), &params.Command); err != nil {
			return nil, fmt.Errorf("failed to parse CWL_COMMAND: %w", err)
		}
	}

	if inputsJSON := os.Getenv("CWL_INPUTS"); inputsJSON != "" {
		if err := json.Unmarshal([]byte(inputsJSON), &params.Inputs); err != nil {
			return nil, fmt.Errorf("failed to parse CWL_INPUTS: %w", err)
		}
	}

	if outputsJSON := os.Getenv("CWL_OUTPUTS"); outputsJSON != "" {
		if err := json.Unmarshal([]byte(outputsJSON), &params.Outputs); err != nil {
			return nil, fmt.Errorf("failed to parse CWL_OUTPUTS: %w", err)
		}
	}

	if envJSON := os.Getenv("CWL_ENVIRONMENT"); envJSON != "" {
		if err := json.Unmarshal([]byte(envJSON), &params.Environment); err != nil {
			return nil, fmt.Errorf("failed to parse CWL_ENVIRONMENT: %w", err)
		}
	}

	params.Stdin = os.Getenv("CWL_STDIN")
	params.Stdout = os.Getenv("CWL_STDOUT")
	params.Stderr = os.Getenv("CWL_STDERR")
	params.WorkDir = os.Getenv("CWL_WORKDIR")
	params.StepID = os.Getenv("CWL_STEP_ID")
	params.NodeID = os.Getenv("CWL_NODE_ID")

	return &params, nil
}

// executeCommand runs the CWL command.
func executeCommand(params *StepParams, workDir string) (int, error) {
	cmd := exec.Command(params.Command[0], params.Command[1:]...)
	cmd.Dir = workDir

	// Set up environment
	cmd.Env = os.Environ()
	for k, v := range params.Environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Set up stdin
	if params.Stdin != "" {
		stdinFile, err := os.Open(filepath.Join(workDir, params.Stdin))
		if err != nil {
			return 1, fmt.Errorf("failed to open stdin file: %w", err)
		}
		defer stdinFile.Close()
		cmd.Stdin = stdinFile
	}

	// Set up stdout
	var stdoutFile *os.File
	if params.Stdout != "" {
		var err error
		stdoutFile, err = os.Create(filepath.Join(workDir, params.Stdout))
		if err != nil {
			return 1, fmt.Errorf("failed to create stdout file: %w", err)
		}
		defer stdoutFile.Close()
		cmd.Stdout = stdoutFile
	} else {
		cmd.Stdout = os.Stdout
	}

	// Set up stderr
	var stderrFile *os.File
	if params.Stderr != "" {
		var err error
		stderrFile, err = os.Create(filepath.Join(workDir, params.Stderr))
		if err != nil {
			return 1, fmt.Errorf("failed to create stderr file: %w", err)
		}
		defer stderrFile.Close()
		cmd.Stderr = stderrFile
	} else {
		cmd.Stderr = os.Stderr
	}

	// Run the command
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return 1, err
		}
	}

	return exitCode, nil
}

// collectOutputs collects outputs according to CWL output bindings.
func collectOutputs(bindings []OutputBinding, inputs map[string]interface{}, workDir string) (map[string]interface{}, error) {
	outputs := make(map[string]interface{})

	for _, binding := range bindings {
		value, err := collectOutput(binding, inputs, workDir)
		if err != nil {
			return nil, fmt.Errorf("failed to collect output %s: %w", binding.ID, err)
		}
		outputs[binding.ID] = value
	}

	return outputs, nil
}

// collectOutput collects a single output.
func collectOutput(binding OutputBinding, inputs map[string]interface{}, workDir string) (interface{}, error) {
	if binding.Glob == "" {
		return nil, nil
	}

	// Evaluate glob pattern (may contain expressions)
	globPattern := binding.Glob
	if strings.Contains(globPattern, "$(") || strings.Contains(globPattern, "${") {
		// Evaluate expression
		evaluator := cwl.NewExpressionEvaluator()
		evaluator.SetInputs(inputs)
		result, err := evaluator.Evaluate(globPattern)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate glob expression: %w", err)
		}
		globPattern = fmt.Sprintf("%v", result)
	}

	// Find matching files
	pattern := filepath.Join(workDir, globPattern)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid glob pattern: %w", err)
	}

	// Build file objects
	var files []interface{}
	for _, match := range matches {
		fileObj, err := buildFileObject(match, binding.LoadContents, binding.LoadListing, workDir)
		if err != nil {
			return nil, err
		}
		files = append(files, fileObj)
	}

	// Return based on type
	if binding.Type == "File" {
		if len(files) == 0 {
			return nil, nil
		}
		return files[0], nil
	}

	// Array of files
	return files, nil
}

// buildFileObject creates a CWL File object.
func buildFileObject(path string, loadContents bool, loadListing string, workDir string) (map[string]interface{}, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	// Get path relative to workDir for location
	relPath, _ := filepath.Rel(workDir, path)

	obj := map[string]interface{}{
		"class":    "File",
		"location": relPath,
		"path":     path,
		"basename": filepath.Base(path),
		"size":     info.Size(),
	}

	// Extract nameroot and nameext
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	obj["nameext"] = ext
	obj["nameroot"] = strings.TrimSuffix(base, ext)

	// Load contents if requested
	if loadContents {
		// Only load if file is reasonably small (< 64KB per CWL spec)
		if info.Size() <= 65536 {
			content, err := os.ReadFile(path)
			if err == nil {
				obj["contents"] = string(content)
			}
		}
	}

	// Handle directory listing
	if info.IsDir() {
		obj["class"] = "Directory"
		delete(obj, "size")
		delete(obj, "nameext")
		delete(obj, "nameroot")

		if loadListing == "shallow_listing" || loadListing == "deep_listing" {
			listing, err := buildDirectoryListing(path, loadListing == "deep_listing", workDir)
			if err == nil {
				obj["listing"] = listing
			}
		}
	}

	return obj, nil
}

// buildDirectoryListing creates a listing of directory contents.
func buildDirectoryListing(dirPath string, deep bool, workDir string) ([]interface{}, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	var listing []interface{}
	for _, entry := range entries {
		entryPath := filepath.Join(dirPath, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		relPath, _ := filepath.Rel(workDir, entryPath)

		if entry.IsDir() {
			dirObj := map[string]interface{}{
				"class":    "Directory",
				"location": relPath,
				"path":     entryPath,
				"basename": entry.Name(),
			}
			if deep {
				subListing, err := buildDirectoryListing(entryPath, true, workDir)
				if err == nil {
					dirObj["listing"] = subListing
				}
			}
			listing = append(listing, dirObj)
		} else {
			fileObj := map[string]interface{}{
				"class":    "File",
				"location": relPath,
				"path":     entryPath,
				"basename": entry.Name(),
				"size":     info.Size(),
			}
			ext := filepath.Ext(entry.Name())
			fileObj["nameext"] = ext
			fileObj["nameroot"] = strings.TrimSuffix(entry.Name(), ext)
			listing = append(listing, fileObj)
		}
	}

	return listing, nil
}

// writeResult writes the step result to cwl_outputs.json.
func writeResult(result StepResult) {
	outputFile := os.Getenv("CWL_OUTPUT_FILE")
	if outputFile == "" {
		outputFile = "cwl_outputs.json"
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal result: %v\n", err)
		return
	}

	if err := os.WriteFile(outputFile, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write result: %v\n", err)
		// Also write to stdout as fallback
		io.Copy(os.Stdout, strings.NewReader(string(data)))
	}
}

// writeError writes an error result.
func writeError(msg string) {
	writeResult(StepResult{
		Status:   "failed",
		ExitCode: 1,
		Error:    msg,
	})
}
