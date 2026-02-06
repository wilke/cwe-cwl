package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// ContainerSandbox runs expressions in isolated containers.
// This provides maximum security at the cost of higher latency (~100-500ms).
// Use for untrusted expressions or high-security environments.
type ContainerSandbox struct {
	config ContainerConfig
}

// ContainerConfig configures the container sandbox.
type ContainerConfig struct {
	// Runtime is the container runtime (docker, podman, apptainer)
	Runtime string `mapstructure:"runtime"`

	// Image is the sandbox container image
	Image string `mapstructure:"image"`

	// Timeout for expression evaluation
	Timeout time.Duration `mapstructure:"timeout"`

	// MaxMemoryMB limits container memory
	MaxMemoryMB int `mapstructure:"max_memory_mb"`

	// NetworkDisabled prevents network access
	NetworkDisabled bool `mapstructure:"network_disabled"`

	// ReadOnlyRootfs makes the filesystem read-only
	ReadOnlyRootfs bool `mapstructure:"read_only_rootfs"`

	// DropCapabilities removes all Linux capabilities
	DropCapabilities bool `mapstructure:"drop_capabilities"`

	// RuntimePath is the path to the container runtime binary
	RuntimePath string `mapstructure:"runtime_path"`
}

// DefaultContainerConfig returns secure defaults.
func DefaultContainerConfig() ContainerConfig {
	return ContainerConfig{
		Runtime:          "docker",
		Image:            "ghcr.io/bv-brc/cwl-expression-sandbox:latest",
		Timeout:          10 * time.Second,
		MaxMemoryMB:      64,
		NetworkDisabled:  true,
		ReadOnlyRootfs:   true,
		DropCapabilities: true,
		RuntimePath:      "docker",
	}
}

// NewContainerSandbox creates a container-based sandbox.
func NewContainerSandbox(cfg ContainerConfig) *ContainerSandbox {
	return &ContainerSandbox{config: cfg}
}

// Evaluate runs an expression in an isolated container.
func (s *ContainerSandbox) Evaluate(ctx context.Context, req Request) (interface{}, error) {
	// Serialize request to JSON
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request: %w", err)
	}

	// Build container command
	args := s.buildContainerArgs(reqJSON)

	// Create command with timeout
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, s.config.RuntimePath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the container
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, ErrTimeout
		}
		return nil, fmt.Errorf("container execution failed: %w: %s", err, stderr.String())
	}

	// Parse response
	var resp Response
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("expression error: %s", resp.Error)
	}

	return resp.Result, nil
}

// buildContainerArgs constructs the container runtime arguments.
func (s *ContainerSandbox) buildContainerArgs(requestJSON []byte) []string {
	switch s.config.Runtime {
	case "docker", "podman":
		return s.buildDockerArgs(requestJSON)
	case "apptainer", "singularity":
		return s.buildApptainerArgs(requestJSON)
	default:
		return s.buildDockerArgs(requestJSON)
	}
}

func (s *ContainerSandbox) buildDockerArgs(requestJSON []byte) []string {
	args := []string{"run", "--rm"}

	// Resource limits
	args = append(args, "--memory", fmt.Sprintf("%dm", s.config.MaxMemoryMB))
	args = append(args, "--memory-swap", fmt.Sprintf("%dm", s.config.MaxMemoryMB)) // No swap
	args = append(args, "--cpus", "0.5")    // Half a CPU max
	args = append(args, "--pids-limit", "5") // Prevent fork bombs

	// Security hardening
	if s.config.NetworkDisabled {
		args = append(args, "--network", "none")
	}

	if s.config.ReadOnlyRootfs {
		args = append(args, "--read-only")
	}

	if s.config.DropCapabilities {
		args = append(args, "--cap-drop", "ALL")
	}

	// No privilege escalation
	args = append(args, "--security-opt", "no-new-privileges")

	// Seccomp profile (use default or custom restricted profile)
	// args = append(args, "--security-opt", "seccomp=/path/to/profile.json")

	// Run as non-root user
	args = append(args, "--user", "65534:65534") // nobody:nogroup

	// Pass request via environment (avoid shell injection)
	args = append(args, "--env", fmt.Sprintf("REQUEST=%s", string(requestJSON)))

	// Image and command
	args = append(args, s.config.Image)
	args = append(args, "/sandbox-eval") // Entrypoint in container

	return args
}

func (s *ContainerSandbox) buildApptainerArgs(requestJSON []byte) []string {
	args := []string{"exec"}

	// Security hardening
	args = append(args, "--containall") // Isolate from host
	args = append(args, "--cleanenv")   // Clean environment
	args = append(args, "--no-home")    // No home directory access
	args = append(args, "--no-init")    // No init process

	if s.config.NetworkDisabled {
		args = append(args, "--net", "--network", "none")
	}

	// Pass request via environment
	args = append(args, "--env", fmt.Sprintf("REQUEST=%s", string(requestJSON)))

	// Image and command
	args = append(args, s.config.Image)
	args = append(args, "/sandbox-eval")

	return args
}

// Close is a no-op for container sandbox (containers are ephemeral).
func (s *ContainerSandbox) Close() error {
	return nil
}
