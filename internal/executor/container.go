package executor

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BV-BRC/cwe-cwl/internal/config"
	"github.com/BV-BRC/cwe-cwl/internal/cwl"
)

// ContainerRunner executes commands inside containers.
type ContainerRunner struct {
	config *config.ContainerConfig
}

// NewContainerRunner creates a new container runner.
func NewContainerRunner(cfg *config.ContainerConfig) *ContainerRunner {
	return &ContainerRunner{config: cfg}
}

// RunCommand builds the container execution command.
func (cr *ContainerRunner) RunCommand(ctx context.Context, spec *cwl.ContainerSpec, workDir string, command []string, envVars map[string]string) *exec.Cmd {
	switch cwl.ContainerRuntime(cr.config.Runtime) {
	case cwl.RuntimeApptainer:
		return cr.buildApptainerCommand(ctx, spec, workDir, command, envVars)
	case cwl.RuntimePodman:
		return cr.buildPodmanCommand(ctx, spec, workDir, command, envVars)
	case cwl.RuntimeDocker:
		return cr.buildDockerCommand(ctx, spec, workDir, command, envVars)
	default:
		// No container - run directly
		return exec.CommandContext(ctx, command[0], command[1:]...)
	}
}

// buildApptainerCommand builds an Apptainer/Singularity command.
func (cr *ContainerRunner) buildApptainerCommand(ctx context.Context, spec *cwl.ContainerSpec, workDir string, command []string, envVars map[string]string) *exec.Cmd {
	args := []string{"exec"}

	// Bind mounts
	args = append(args, "--bind", fmt.Sprintf("%s:/work", workDir))
	args = append(args, "--pwd", "/work")

	// GPU support
	if spec.NeedsGPU && cr.config.GPUEnabled {
		args = append(args, "--nv") // NVIDIA GPU support
	}

	// Clean environment and pass specified vars
	args = append(args, "--cleanenv")
	for k, v := range envVars {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	// Container image
	image := cr.resolveApptainerImage(spec)
	args = append(args, image)

	// Command to run
	args = append(args, command...)

	return exec.CommandContext(ctx, cr.config.ApptainerPath, args...)
}

// buildDockerCommand builds a Docker command.
func (cr *ContainerRunner) buildDockerCommand(ctx context.Context, spec *cwl.ContainerSpec, workDir string, command []string, envVars map[string]string) *exec.Cmd {
	args := []string{"run", "--rm"}

	// Bind mounts
	args = append(args, "-v", fmt.Sprintf("%s:/work", workDir))
	args = append(args, "-w", "/work")

	// GPU support
	if spec.NeedsGPU && cr.config.GPUEnabled {
		args = append(args, "--gpus", fmt.Sprintf("%d", spec.GPUCount))
	}

	// Environment variables
	for k, v := range envVars {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Container image
	args = append(args, spec.Image)

	// Command to run
	args = append(args, command...)

	return exec.CommandContext(ctx, cr.config.DockerPath, args...)
}

// buildPodmanCommand builds a Podman command.
func (cr *ContainerRunner) buildPodmanCommand(ctx context.Context, spec *cwl.ContainerSpec, workDir string, command []string, envVars map[string]string) *exec.Cmd {
	args := []string{"run", "--rm"}

	// Bind mounts
	args = append(args, "-v", fmt.Sprintf("%s:/work:Z", workDir))
	args = append(args, "-w", "/work")

	// GPU support (podman uses --device for GPU)
	if spec.NeedsGPU && cr.config.GPUEnabled {
		args = append(args, "--device", "nvidia.com/gpu=all")
	}

	// Environment variables
	for k, v := range envVars {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Container image
	args = append(args, spec.Image)

	// Command to run
	args = append(args, command...)

	return exec.CommandContext(ctx, cr.config.PodmanPath, args...)
}

// resolveApptainerImage resolves the Apptainer image path/URI.
func (cr *ContainerRunner) resolveApptainerImage(spec *cwl.ContainerSpec) string {
	// If it's already a local SIF file
	if strings.HasSuffix(spec.Image, ".sif") {
		return spec.Image
	}

	// If we have a pull reference, use it
	if spec.Pull != "" {
		return spec.Pull
	}

	// Convert Docker image to docker:// URI
	if !strings.Contains(spec.Image, "://") {
		return "docker://" + spec.Image
	}

	return spec.Image
}

// PullImage pulls/downloads a container image if needed.
func (cr *ContainerRunner) PullImage(ctx context.Context, spec *cwl.ContainerSpec) error {
	if cr.config.PullPolicy == "never" {
		return nil
	}

	switch cwl.ContainerRuntime(cr.config.Runtime) {
	case cwl.RuntimeApptainer:
		return cr.pullApptainerImage(ctx, spec)
	case cwl.RuntimeDocker:
		return cr.pullDockerImage(ctx, spec)
	case cwl.RuntimePodman:
		return cr.pullPodmanImage(ctx, spec)
	}
	return nil
}

// pullApptainerImage pulls an Apptainer image.
func (cr *ContainerRunner) pullApptainerImage(ctx context.Context, spec *cwl.ContainerSpec) error {
	// Determine cache location
	imageName := sanitizeImageName(spec.Image)
	sifPath := filepath.Join(cr.config.CacheDir, imageName+".sif")

	// Check if already cached
	if cr.config.PullPolicy == "if-not-present" {
		if _, err := exec.LookPath(sifPath); err == nil {
			return nil
		}
	}

	// Pull the image
	source := cr.resolveApptainerImage(spec)
	cmd := exec.CommandContext(ctx, cr.config.ApptainerPath, "pull", "--force", sifPath, source)

	return cmd.Run()
}

// pullDockerImage pulls a Docker image.
func (cr *ContainerRunner) pullDockerImage(ctx context.Context, spec *cwl.ContainerSpec) error {
	if cr.config.PullPolicy == "if-not-present" {
		// Check if image exists
		checkCmd := exec.CommandContext(ctx, cr.config.DockerPath, "image", "inspect", spec.Image)
		if checkCmd.Run() == nil {
			return nil
		}
	}

	cmd := exec.CommandContext(ctx, cr.config.DockerPath, "pull", spec.Image)
	return cmd.Run()
}

// pullPodmanImage pulls a Podman image.
func (cr *ContainerRunner) pullPodmanImage(ctx context.Context, spec *cwl.ContainerSpec) error {
	if cr.config.PullPolicy == "if-not-present" {
		// Check if image exists
		checkCmd := exec.CommandContext(ctx, cr.config.PodmanPath, "image", "exists", spec.Image)
		if checkCmd.Run() == nil {
			return nil
		}
	}

	cmd := exec.CommandContext(ctx, cr.config.PodmanPath, "pull", spec.Image)
	return cmd.Run()
}

// CheckExecutable verifies an executable exists (in PATH or container).
func (cr *ContainerRunner) CheckExecutable(ctx context.Context, spec *cwl.ContainerSpec, executable string) error {
	if spec.Runtime == cwl.RuntimeNone {
		// Check in host PATH
		_, err := exec.LookPath(executable)
		return err
	}

	// Check inside container
	cmd := cr.RunCommand(ctx, spec, "/tmp", []string{"which", executable}, nil)
	return cmd.Run()
}

// sanitizeImageName converts an image name to a safe filename.
func sanitizeImageName(image string) string {
	// Remove protocol prefix
	name := strings.TrimPrefix(image, "docker://")
	name = strings.TrimPrefix(name, "library://")

	// Replace unsafe characters
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, ":", "_")

	return name
}

// ValidateContainerRequirement checks that a tool has proper container requirements.
func ValidateContainerRequirement(doc *cwl.Document) error {
	if !doc.RequiresContainer() {
		return fmt.Errorf("tool must specify DockerRequirement or ApptainerRequirement")
	}

	dockerReq := doc.GetDockerRequirement()
	if dockerReq != nil {
		if dockerReq.DockerPull == "" && dockerReq.DockerImageID == "" {
			return fmt.Errorf("DockerRequirement must specify dockerPull or dockerImageId")
		}
	}

	appReq := doc.GetApptainerRequirement()
	if appReq != nil {
		if appReq.ApptainerPull == "" && appReq.ApptainerFile == "" {
			return fmt.Errorf("ApptainerRequirement must specify apptainerPull or apptainerFile")
		}
	}

	return nil
}

// ValidateBaseCommand checks that baseCommand doesn't use paths.
func ValidateBaseCommand(doc *cwl.Document) error {
	switch v := doc.BaseCommand.(type) {
	case string:
		return validateCommandPath(v)
	case []interface{}:
		if len(v) > 0 {
			if cmd, ok := v[0].(string); ok {
				return validateCommandPath(cmd)
			}
		}
	}
	return nil
}

// validateCommandPath ensures a command doesn't contain path separators.
func validateCommandPath(cmd string) error {
	if strings.Contains(cmd, "/") || strings.Contains(cmd, "\\") {
		return fmt.Errorf("baseCommand should not contain paths; use executable name only: %s", cmd)
	}
	return nil
}
