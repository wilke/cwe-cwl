package cwl

import (
	"fmt"
	"sort"
	"strings"
)

// CommandBuilder builds command lines from CommandLineTool definitions.
type CommandBuilder struct {
	doc    *Document
	inputs map[string]interface{}
}

// NewCommandBuilder creates a new command builder.
func NewCommandBuilder(doc *Document, inputs map[string]interface{}) *CommandBuilder {
	return &CommandBuilder{
		doc:    doc,
		inputs: inputs,
	}
}

// BuildCommand builds the command line for execution.
func (cb *CommandBuilder) BuildCommand() ([]string, error) {
	if cb.doc.Class != ClassCommandLineTool {
		return nil, fmt.Errorf("document is not a CommandLineTool")
	}

	var cmdParts []commandPart

	// Add base command
	baseCmd := cb.getBaseCommand()
	for i, cmd := range baseCmd {
		cmdParts = append(cmdParts, commandPart{
			position: -1000000 + i, // Base command comes first
			value:    []string{cmd},
		})
	}

	// Add arguments
	for i, arg := range cb.doc.Arguments {
		parts, err := cb.buildArgument(arg, i)
		if err != nil {
			return nil, fmt.Errorf("failed to build argument %d: %w", i, err)
		}
		cmdParts = append(cmdParts, parts...)
	}

	// Add inputs with bindings
	for _, input := range cb.doc.Inputs {
		if input.InputBinding == nil {
			continue
		}
		parts, err := cb.buildInputBinding(input)
		if err != nil {
			return nil, fmt.Errorf("failed to build input binding for %s: %w", input.ID, err)
		}
		cmdParts = append(cmdParts, parts...)
	}

	// Sort by position
	sort.Slice(cmdParts, func(i, j int) bool {
		return cmdParts[i].position < cmdParts[j].position
	})

	// Flatten to string slice
	var result []string
	for _, part := range cmdParts {
		result = append(result, part.value...)
	}

	return result, nil
}

// commandPart represents a part of the command line with its position.
type commandPart struct {
	position int
	value    []string
}

// getBaseCommand extracts the base command as a string slice.
func (cb *CommandBuilder) getBaseCommand() []string {
	switch v := cb.doc.BaseCommand.(type) {
	case string:
		return []string{v}
	case []interface{}:
		var result []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

// buildArgument builds command parts from an argument.
func (cb *CommandBuilder) buildArgument(arg CommandLineArg, index int) ([]commandPart, error) {
	position := arg.Position
	if position == 0 {
		position = index
	}

	var value string
	if arg.ValueFrom != "" {
		// Evaluate expression
		evaluated, err := cb.evaluateExpression(arg.ValueFrom)
		if err != nil {
			return nil, err
		}
		value = fmt.Sprintf("%v", evaluated)
	}

	return cb.buildBindingParts(position, arg.Prefix, arg.Separate, value, arg.ShellQuote), nil
}

// buildInputBinding builds command parts from an input binding.
func (cb *CommandBuilder) buildInputBinding(input Input) ([]commandPart, error) {
	binding := input.InputBinding

	// Get input value
	var value interface{}
	if v, ok := cb.inputs[input.ID]; ok {
		value = v
	} else if input.Default != nil {
		value = input.Default
	} else {
		// Check if type is optional
		parsedType, err := ParseType(input.Type)
		if err == nil && parsedType.IsOptional() {
			return nil, nil // Skip optional inputs with no value
		}
		return nil, fmt.Errorf("missing required input: %s", input.ID)
	}

	// Check for null value
	if value == nil {
		return nil, nil
	}

	// Handle valueFrom expression
	if binding.ValueFrom != "" {
		evaluated, err := cb.evaluateExpression(binding.ValueFrom)
		if err != nil {
			return nil, err
		}
		value = evaluated
	}

	// Convert value to string representation
	strValue := cb.formatValue(value, input.Type, binding)

	return cb.buildBindingParts(binding.Position, binding.Prefix, binding.Separate, strValue, binding.ShellQuote), nil
}

// buildBindingParts creates command parts from binding components.
func (cb *CommandBuilder) buildBindingParts(position int, prefix string, separate *bool, value string, shellQuote *bool) []commandPart {
	var parts []commandPart

	sep := true
	if separate != nil {
		sep = *separate
	}

	if prefix != "" {
		if sep && value != "" {
			parts = append(parts, commandPart{
				position: position,
				value:    []string{prefix, value},
			})
		} else if value != "" {
			parts = append(parts, commandPart{
				position: position,
				value:    []string{prefix + value},
			})
		} else {
			parts = append(parts, commandPart{
				position: position,
				value:    []string{prefix},
			})
		}
	} else if value != "" {
		parts = append(parts, commandPart{
			position: position,
			value:    []string{value},
		})
	}

	return parts
}

// formatValue formats an input value for the command line.
func (cb *CommandBuilder) formatValue(value interface{}, typeSpec interface{}, binding *CommandLineBinding) string {
	switch v := value.(type) {
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int, int32, int64, float32, float64:
		return fmt.Sprintf("%v", v)
	case string:
		return v
	case map[string]interface{}:
		// Check if it's a File or Directory
		if class, ok := v["class"].(string); ok {
			switch class {
			case TypeFile, TypeDirectory:
				if path, ok := v["path"].(string); ok {
					return path
				}
				if loc, ok := v["location"].(string); ok {
					return loc
				}
			}
		}
		return fmt.Sprintf("%v", v)
	case []interface{}:
		// Array type
		itemSep := " "
		if binding != nil && binding.ItemSeparator != "" {
			itemSep = binding.ItemSeparator
		}
		var items []string
		for _, item := range v {
			items = append(items, cb.formatValue(item, nil, nil))
		}
		return strings.Join(items, itemSep)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// evaluateExpression evaluates a CWL expression.
func (cb *CommandBuilder) evaluateExpression(expr string) (interface{}, error) {
	// Check if it's a parameter reference $(inputs.foo)
	if strings.HasPrefix(expr, "$(") && strings.HasSuffix(expr, ")") {
		ref := expr[2 : len(expr)-1]
		return cb.resolveReference(ref)
	}

	// Check if it's a JavaScript expression ${...}
	if strings.HasPrefix(expr, "${") && strings.HasSuffix(expr, "}") {
		// JavaScript evaluation would go here
		// For now, return the expression as-is
		return expr, nil
	}

	// Literal value
	return expr, nil
}

// resolveReference resolves a parameter reference like inputs.foo.
func (cb *CommandBuilder) resolveReference(ref string) (interface{}, error) {
	parts := strings.Split(ref, ".")
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty reference")
	}

	var current interface{}

	switch parts[0] {
	case "inputs":
		current = cb.inputs
	case "self":
		// self refers to current input value in certain contexts
		return nil, fmt.Errorf("self reference not supported in this context")
	case "runtime":
		// runtime references need special handling
		return cb.resolveRuntimeReference(parts[1:])
	default:
		return nil, fmt.Errorf("unknown reference root: %s", parts[0])
	}

	// Navigate through the reference path
	for i := 1; i < len(parts); i++ {
		switch c := current.(type) {
		case map[string]interface{}:
			var ok bool
			current, ok = c[parts[i]]
			if !ok {
				return nil, fmt.Errorf("reference path not found: %s", strings.Join(parts[:i+1], "."))
			}
		default:
			return nil, fmt.Errorf("cannot navigate into non-object at: %s", strings.Join(parts[:i], "."))
		}
	}

	return current, nil
}

// resolveRuntimeReference resolves runtime.* references.
func (cb *CommandBuilder) resolveRuntimeReference(parts []string) (interface{}, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty runtime reference")
	}

	// These would be populated with actual runtime values during execution
	runtimeValues := map[string]interface{}{
		"cores":      1,
		"ram":        4096,
		"tmpdirSize": 1024,
		"outdirSize": 1024,
		"tmpdir":     "/tmp",
		"outdir":     "/output",
	}

	if val, ok := runtimeValues[parts[0]]; ok {
		return val, nil
	}

	return nil, fmt.Errorf("unknown runtime property: %s", parts[0])
}

// GetDockerRequirement returns the DockerRequirement if present.
func (doc *Document) GetDockerRequirement() *Requirement {
	for i := range doc.Requirements {
		if doc.Requirements[i].Class == "DockerRequirement" {
			return &doc.Requirements[i]
		}
	}
	for i := range doc.Hints {
		if doc.Hints[i].Class == "DockerRequirement" {
			return &doc.Hints[i]
		}
	}
	return nil
}

// GetResourceRequirement returns the ResourceRequirement if present.
func (doc *Document) GetResourceRequirement() *Requirement {
	for i := range doc.Requirements {
		if doc.Requirements[i].Class == "ResourceRequirement" {
			return &doc.Requirements[i]
		}
	}
	for i := range doc.Hints {
		if doc.Hints[i].Class == "ResourceRequirement" {
			return &doc.Hints[i]
		}
	}
	return nil
}

// HasRequirement checks if a requirement class is present.
func (doc *Document) HasRequirement(class string) bool {
	for _, req := range doc.Requirements {
		if req.Class == class {
			return true
		}
	}
	for _, hint := range doc.Hints {
		if hint.Class == class {
			return true
		}
	}
	return false
}

// GetDockerImage returns the Docker image to use.
func (doc *Document) GetDockerImage() string {
	req := doc.GetDockerRequirement()
	if req == nil {
		return ""
	}
	if req.DockerPull != "" {
		return req.DockerPull
	}
	if req.DockerImageID != "" {
		return req.DockerImageID
	}
	return ""
}

// GetApptainerRequirement returns the ApptainerRequirement if present.
func (doc *Document) GetApptainerRequirement() *Requirement {
	for i := range doc.Requirements {
		if doc.Requirements[i].Class == "ApptainerRequirement" {
			return &doc.Requirements[i]
		}
	}
	for i := range doc.Hints {
		if doc.Hints[i].Class == "ApptainerRequirement" {
			return &doc.Hints[i]
		}
	}
	return nil
}

// GetCUDARequirement returns the CUDARequirement if present.
func (doc *Document) GetCUDARequirement() *Requirement {
	// Check for cwltool:CUDARequirement (with namespace prefix)
	for i := range doc.Requirements {
		if doc.Requirements[i].Class == "cwltool:CUDARequirement" ||
			doc.Requirements[i].Class == "CUDARequirement" {
			return &doc.Requirements[i]
		}
	}
	for i := range doc.Hints {
		if doc.Hints[i].Class == "cwltool:CUDARequirement" ||
			doc.Hints[i].Class == "CUDARequirement" {
			return &doc.Hints[i]
		}
	}
	return nil
}

// ContainerRuntime represents the container runtime type.
type ContainerRuntime string

const (
	RuntimeDocker    ContainerRuntime = "docker"
	RuntimePodman    ContainerRuntime = "podman"
	RuntimeApptainer ContainerRuntime = "apptainer"
	RuntimeNone      ContainerRuntime = "none"
)

// ContainerSpec holds container specification for execution.
type ContainerSpec struct {
	Runtime   ContainerRuntime
	Image     string // Docker image or Apptainer SIF path
	Pull      string // Pull reference (docker://, library://, etc.)
	NeedsGPU  bool
	GPUCount  int
	CUDAMinVersion string
}

// GetContainerSpec returns the container specification for this document.
func (doc *Document) GetContainerSpec(preferredRuntime ContainerRuntime) *ContainerSpec {
	spec := &ContainerSpec{}

	// Check for CUDA requirement
	cudaReq := doc.GetCUDARequirement()
	if cudaReq != nil {
		spec.NeedsGPU = true
		spec.GPUCount = cudaReq.CUDADeviceCountMin
		if spec.GPUCount == 0 {
			spec.GPUCount = 1
		}
		spec.CUDAMinVersion = cudaReq.CUDAVersionMin
	}

	// Check for ApptainerRequirement first if Apptainer is preferred
	if preferredRuntime == RuntimeApptainer {
		appReq := doc.GetApptainerRequirement()
		if appReq != nil {
			spec.Runtime = RuntimeApptainer
			if appReq.ApptainerFile != "" {
				spec.Image = appReq.ApptainerFile
			} else if appReq.ApptainerPull != "" {
				spec.Pull = appReq.ApptainerPull
			}
			return spec
		}
	}

	// Fall back to DockerRequirement
	dockerReq := doc.GetDockerRequirement()
	if dockerReq != nil {
		spec.Runtime = preferredRuntime
		if spec.Runtime == "" {
			spec.Runtime = RuntimeDocker
		}

		if dockerReq.DockerPull != "" {
			spec.Image = dockerReq.DockerPull
			// For Apptainer, convert to docker:// URI
			if preferredRuntime == RuntimeApptainer {
				spec.Pull = "docker://" + dockerReq.DockerPull
			} else {
				spec.Pull = dockerReq.DockerPull
			}
		} else if dockerReq.DockerImageID != "" {
			spec.Image = dockerReq.DockerImageID
		}
		return spec
	}

	// No container requirement
	spec.Runtime = RuntimeNone
	return spec
}

// RequiresContainer returns true if a container is required.
func (doc *Document) RequiresContainer() bool {
	return doc.GetDockerRequirement() != nil || doc.GetApptainerRequirement() != nil
}

// GetResourceRequirements extracts resource requirements.
func (doc *Document) GetResourceRequirements() (cores int, ramMB int, err error) {
	req := doc.GetResourceRequirement()
	if req == nil {
		return 1, 4096, nil // defaults
	}

	cores = toInt(req.CoresMin, 1)
	ramMB = toInt(req.RAMMin, 4096)

	return cores, ramMB, nil
}

// toInt converts an interface to int with a default value.
func toInt(v interface{}, def int) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case string:
		// Could be an expression, return default
		return def
	default:
		return def
	}
}
