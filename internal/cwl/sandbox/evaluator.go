package sandbox

import (
	"context"
	"fmt"
)

// Mode determines the isolation level for expression evaluation.
type Mode string

const (
	// ModeInProcess runs expressions in the same process (fastest, least secure).
	// Only use for trusted expressions or development.
	ModeInProcess Mode = "inprocess"

	// ModeProcess runs expressions in isolated worker processes (recommended).
	// Provides good security with low overhead (~1-5ms).
	ModeProcess Mode = "process"

	// ModeContainer runs expressions in isolated containers (most secure).
	// Higher overhead (~100-500ms) but maximum isolation.
	ModeContainer Mode = "container"
)

// Evaluator is the interface for expression evaluation.
type Evaluator interface {
	// Evaluate executes a JavaScript expression and returns the result.
	Evaluate(ctx context.Context, req Request) (interface{}, error)

	// Close releases resources.
	Close() error
}

// EvaluatorConfig configures the expression evaluator.
type EvaluatorConfig struct {
	// Mode determines the isolation level.
	Mode Mode `mapstructure:"mode"`

	// Process config (used when Mode == ModeProcess)
	Process Config `mapstructure:"process"`

	// Container config (used when Mode == ModeContainer)
	Container ContainerConfig `mapstructure:"container"`
}

// DefaultEvaluatorConfig returns sensible defaults.
func DefaultEvaluatorConfig() EvaluatorConfig {
	return EvaluatorConfig{
		Mode:      ModeProcess,
		Process:   DefaultConfig(),
		Container: DefaultContainerConfig(),
	}
}

// NewEvaluator creates an expression evaluator based on configuration.
func NewEvaluator(cfg EvaluatorConfig) (Evaluator, error) {
	switch cfg.Mode {
	case ModeInProcess:
		return NewInProcessEvaluator(), nil

	case ModeProcess:
		return NewPool(cfg.Process)

	case ModeContainer:
		return NewContainerSandbox(cfg.Container), nil

	default:
		return nil, fmt.Errorf("unknown sandbox mode: %s", cfg.Mode)
	}
}

// InProcessEvaluator runs expressions in the same process.
// This is fast but provides no isolation - use only for trusted expressions.
type InProcessEvaluator struct{}

// NewInProcessEvaluator creates an in-process evaluator.
func NewInProcessEvaluator() *InProcessEvaluator {
	return &InProcessEvaluator{}
}

// Evaluate runs an expression in the current process.
func (e *InProcessEvaluator) Evaluate(ctx context.Context, req Request) (interface{}, error) {
	vm := createSandboxedVM()

	// Set up context variables
	vm.Set("inputs", req.Inputs)
	vm.Set("self", req.Self)
	vm.Set("runtime", req.Runtime)

	// Use a goroutine with timeout
	type result struct {
		value interface{}
		err   error
	}

	resultCh := make(chan result, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				resultCh <- result{err: fmt.Errorf("expression panic: %v", r)}
			}
		}()

		val, err := vm.RunString(req.Expression)
		if err != nil {
			resultCh <- result{err: err}
			return
		}
		resultCh <- result{value: val.Export()}
	}()

	select {
	case r := <-resultCh:
		return r.value, r.err
	case <-ctx.Done():
		vm.Interrupt("timeout")
		return nil, ErrTimeout
	}
}

// Close is a no-op for in-process evaluator.
func (e *InProcessEvaluator) Close() error {
	return nil
}
