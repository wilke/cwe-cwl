// sandbox-worker is a minimal binary for evaluating CWL JavaScript expressions
// in an isolated environment. It can run as:
// 1. A child process worker (stdin/stdout JSON communication)
// 2. A container entrypoint (REQUEST env var input, stdout output)
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/BV-BRC/cwe-cwl/internal/cwl/sandbox"
)

func main() {
	// Check if running in container mode (REQUEST env var)
	if reqJSON := os.Getenv("REQUEST"); reqJSON != "" {
		runContainerMode(reqJSON)
		return
	}

	// Otherwise run as worker process
	sandbox.RunWorker()
}

// runContainerMode handles single-shot execution in a container.
func runContainerMode(reqJSON string) {
	var req sandbox.Request
	if err := json.Unmarshal([]byte(reqJSON), &req); err != nil {
		outputError(fmt.Sprintf("failed to parse request: %v", err))
		return
	}

	// Apply resource limits
	sandbox.RunWorker() // This will read from env and apply limits

	// Create evaluator and run
	eval := sandbox.NewInProcessEvaluator()
	result, err := eval.Evaluate(nil, req)
	if err != nil {
		outputError(err.Error())
		return
	}

	resp := sandbox.Response{Result: result}
	json.NewEncoder(os.Stdout).Encode(resp)
}

func outputError(msg string) {
	resp := sandbox.Response{Error: msg}
	json.NewEncoder(os.Stdout).Encode(resp)
}
