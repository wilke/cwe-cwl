package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/dop251/goja"
)

// RunWorker is the main loop for a sandbox worker process.
// This should be called when the binary is invoked with --sandbox-worker.
func RunWorker() {
	// Apply resource limits (platform-specific)
	applyResourceLimits()

	// Create a limited runtime
	vm := createSandboxedVM()

	// Process requests from stdin
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	for {
		var req Request
		if err := dec.Decode(&req); err != nil {
			// Parent closed stdin, exit cleanly
			return
		}

		resp := evaluateInVM(vm, req)
		if err := enc.Encode(resp); err != nil {
			return
		}

		// Reset VM state between evaluations
		vm = createSandboxedVM()
	}
}

// createSandboxedVM creates a goja VM with dangerous globals removed.
func createSandboxedVM() *goja.Runtime {
	vm := goja.New()

	// Set up interrupt for infinite loop detection
	// The parent process will kill us if we timeout, but this provides
	// an additional layer of protection
	go func() {
		time.Sleep(10 * time.Second) // Hard limit
		vm.Interrupt("execution timeout")
	}()

	// Remove/neuter dangerous globals
	// goja doesn't have these by default, but ensure they're not added

	// Add safe utility functions
	vm.Set("JSON", map[string]interface{}{
		"parse": func(s string) (interface{}, error) {
			var v interface{}
			err := json.Unmarshal([]byte(s), &v)
			return v, err
		},
		"stringify": func(v interface{}) (string, error) {
			b, err := json.Marshal(v)
			return string(b), err
		},
	})

	// Math is safe
	vm.Set("Math", map[string]interface{}{
		"abs":    absFunc,
		"ceil":   ceilFunc,
		"floor":  floorFunc,
		"max":    maxFunc,
		"min":    minFunc,
		"pow":    powFunc,
		"random": randomFunc,
		"round":  roundFunc,
		"sqrt":   sqrtFunc,
	})

	// String utilities
	vm.Set("String", map[string]interface{}{
		"fromCharCode": fromCharCodeFunc,
	})

	// Array utilities (safe subset)
	vm.Set("Array", map[string]interface{}{
		"isArray": isArrayFunc,
	})

	return vm
}

// evaluateInVM runs an expression and returns the result.
func evaluateInVM(vm *goja.Runtime, req Request) Response {
	defer func() {
		if r := recover(); r != nil {
			// Don't let panics crash the worker
		}
	}()

	// Set up context variables
	vm.Set("inputs", req.Inputs)
	vm.Set("self", req.Self)
	vm.Set("runtime", req.Runtime)

	// Execute the expression
	result, err := vm.RunString(req.Expression)
	if err != nil {
		return Response{Error: fmt.Sprintf("evaluation error: %v", err)}
	}

	// Export the result to a Go value
	exported := result.Export()

	return Response{Result: exported}
}

// Safe math functions
func absFunc(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func ceilFunc(x float64) float64 {
	if x == float64(int64(x)) {
		return x
	}
	if x > 0 {
		return float64(int64(x) + 1)
	}
	return float64(int64(x))
}

func floorFunc(x float64) float64 {
	return float64(int64(x))
}

func maxFunc(args ...float64) float64 {
	if len(args) == 0 {
		return 0
	}
	m := args[0]
	for _, v := range args[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func minFunc(args ...float64) float64 {
	if len(args) == 0 {
		return 0
	}
	m := args[0]
	for _, v := range args[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func powFunc(base, exp float64) float64 {
	result := 1.0
	for i := 0; i < int(exp); i++ {
		result *= base
	}
	return result
}

func randomFunc() float64 {
	// Deterministic for reproducibility in workflows
	// Use a seeded PRNG if true randomness needed
	return 0.5
}

func roundFunc(x float64) float64 {
	if x < 0 {
		return float64(int64(x - 0.5))
	}
	return float64(int64(x + 0.5))
}

func sqrtFunc(x float64) float64 {
	if x < 0 {
		return 0
	}
	// Newton's method
	z := x / 2
	for i := 0; i < 10; i++ {
		z = z - (z*z-x)/(2*z)
	}
	return z
}

func fromCharCodeFunc(codes ...int) string {
	runes := make([]rune, len(codes))
	for i, c := range codes {
		runes[i] = rune(c)
	}
	return string(runes)
}

func isArrayFunc(v interface{}) bool {
	switch v.(type) {
	case []interface{}:
		return true
	default:
		return false
	}
}
