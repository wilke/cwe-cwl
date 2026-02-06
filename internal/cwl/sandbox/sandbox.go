// Package sandbox provides secure JavaScript expression evaluation
// using isolated worker processes with resource limits.
package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

var (
	ErrTimeout        = errors.New("expression evaluation timed out")
	ErrMemoryExceeded = errors.New("expression exceeded memory limit")
	ErrWorkerCrashed  = errors.New("sandbox worker crashed")
	ErrPoolExhausted  = errors.New("no available sandbox workers")
)

// Config holds sandbox configuration.
type Config struct {
	// WorkerCount is the number of pre-forked worker processes.
	WorkerCount int `mapstructure:"worker_count"`

	// Timeout is the maximum execution time per expression.
	Timeout time.Duration `mapstructure:"timeout"`

	// MaxMemoryMB is the memory limit per worker in megabytes.
	MaxMemoryMB int `mapstructure:"max_memory_mb"`

	// MaxOutputBytes limits the size of expression results.
	MaxOutputBytes int `mapstructure:"max_output_bytes"`

	// WorkerBinary is the path to the sandbox worker binary.
	// If empty, uses the built-in worker mode.
	WorkerBinary string `mapstructure:"worker_binary"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		WorkerCount:    4,
		Timeout:        5 * time.Second,
		MaxMemoryMB:    50,
		MaxOutputBytes: 1024 * 1024, // 1MB
		WorkerBinary:   "",          // Use self
	}
}

// Request is sent to worker processes.
type Request struct {
	Expression string                 `json:"expression"`
	Inputs     map[string]interface{} `json:"inputs"`
	Self       interface{}            `json:"self"`
	Runtime    map[string]interface{} `json:"runtime"`
}

// Response is returned from worker processes.
type Response struct {
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// Pool manages a pool of sandbox worker processes.
type Pool struct {
	config  Config
	workers chan *worker
	mu      sync.Mutex
	closed  bool
}

// worker represents a single sandbox worker process.
type worker struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	enc    *json.Encoder
	dec    *json.Decoder
	inUse  bool
}

// NewPool creates a new sandbox worker pool.
func NewPool(cfg Config) (*Pool, error) {
	p := &Pool{
		config:  cfg,
		workers: make(chan *worker, cfg.WorkerCount),
	}

	// Pre-fork workers
	for i := 0; i < cfg.WorkerCount; i++ {
		w, err := p.startWorker()
		if err != nil {
			p.Close()
			return nil, fmt.Errorf("failed to start worker %d: %w", i, err)
		}
		p.workers <- w
	}

	return p, nil
}

// startWorker forks a new sandbox worker process.
func (p *Pool) startWorker() (*worker, error) {
	binary := p.config.WorkerBinary
	if binary == "" {
		// Use self with --sandbox-worker flag
		binary = os.Args[0]
	}

	cmd := exec.Command(binary, "--sandbox-worker")

	// Set resource limits via environment (worker enforces via rlimit)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("SANDBOX_MEMORY_MB=%d", p.config.MaxMemoryMB),
		fmt.Sprintf("SANDBOX_TIMEOUT_SEC=%d", int(p.config.Timeout.Seconds())),
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &worker{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
		enc:    json.NewEncoder(stdin),
		dec:    json.NewDecoder(stdout),
	}, nil
}

// Evaluate executes a JavaScript expression in a sandboxed worker.
func (p *Pool) Evaluate(ctx context.Context, req Request) (interface{}, error) {
	// Get a worker from the pool
	var w *worker
	select {
	case w = <-p.workers:
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, ErrPoolExhausted
	}

	// Ensure worker is returned to pool (or replaced if crashed)
	defer func() {
		if w.cmd.ProcessState != nil && w.cmd.ProcessState.Exited() {
			// Worker crashed, start a new one
			if newW, err := p.startWorker(); err == nil {
				p.workers <- newW
			}
		} else {
			p.workers <- w
		}
	}()

	// Send request with timeout
	resultCh := make(chan Response, 1)
	errCh := make(chan error, 1)

	go func() {
		if err := w.enc.Encode(req); err != nil {
			errCh <- fmt.Errorf("failed to send request: %w", err)
			return
		}

		var resp Response
		if err := w.dec.Decode(&resp); err != nil {
			errCh <- fmt.Errorf("failed to read response: %w", err)
			return
		}
		resultCh <- resp
	}()

	// Wait for result or timeout
	select {
	case resp := <-resultCh:
		if resp.Error != "" {
			return nil, errors.New(resp.Error)
		}
		return resp.Result, nil

	case err := <-errCh:
		// Kill the worker - it may be hung
		w.cmd.Process.Kill()
		return nil, err

	case <-ctx.Done():
		w.cmd.Process.Kill()
		return nil, ErrTimeout

	case <-time.After(p.config.Timeout):
		w.cmd.Process.Kill()
		return nil, ErrTimeout
	}
}

// Close shuts down all workers.
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true

	close(p.workers)
	for w := range p.workers {
		w.stdin.Close()
		w.cmd.Process.Kill()
		w.cmd.Wait()
	}

	return nil
}
