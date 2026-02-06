// Package bvbrc provides BV-BRC integration utilities.
package bvbrc

import (
	"encoding/json"
	"fmt"

	"github.com/BV-BRC/cwe-cwl/internal/cwl"
)

// CWLJobSpec is the job specification submitted to the BV-BRC scheduler.
// The CWL tool document itself serves as the tool specification.
type CWLJobSpec struct {
	// Tool is the CWL CommandLineTool document.
	Tool *cwl.Document `json:"tool"`

	// Inputs are the resolved input values for this job.
	Inputs map[string]interface{} `json:"inputs"`

	// OutputPath is the workspace path for output files.
	OutputPath string `json:"output_path"`

	// OutputFile is the optional basename for output files.
	OutputFile string `json:"output_file,omitempty"`

	// Owner is the user submitting the job.
	Owner string `json:"owner,omitempty"`
}

// NewCWLJobSpec creates a job spec from a CWL document and inputs.
func NewCWLJobSpec(doc *cwl.Document, inputs map[string]interface{}, outputPath string) (*CWLJobSpec, error) {
	if doc.Class != "CommandLineTool" {
		return nil, fmt.Errorf("expected CommandLineTool, got %s", doc.Class)
	}

	// Validate required inputs are provided
	if err := validateInputs(doc, inputs); err != nil {
		return nil, err
	}

	return &CWLJobSpec{
		Tool:       doc,
		Inputs:     inputs,
		OutputPath: outputPath,
	}, nil
}

// validateInputs checks that all required inputs are provided.
func validateInputs(doc *cwl.Document, inputs map[string]interface{}) error {
	for _, input := range doc.Inputs {
		_, provided := inputs[input.ID]
		if !provided {
			// Check if optional or has default
			if !isOptional(input) && input.Default == nil {
				return fmt.Errorf("missing required input: %s", input.ID)
			}
		}
	}
	return nil
}

// isOptional checks if a CWL input is optional.
func isOptional(input cwl.Input) bool {
	switch t := input.Type.(type) {
	case string:
		// Shorthand: "string?"
		if len(t) > 0 && t[len(t)-1] == '?' {
			return true
		}
	case []interface{}:
		// Union type: ["null", "string"]
		for _, item := range t {
			if s, ok := item.(string); ok && s == "null" {
				return true
			}
		}
	}
	return false
}

// GetContainerID extracts the container ID from the CWL tool requirements.
func (j *CWLJobSpec) GetContainerID() string {
	return GetContainerID(j.Tool)
}

// GetResourceRequirements extracts CPU and memory requirements.
func (j *CWLJobSpec) GetResourceRequirements() (cpu int, memoryMB int) {
	return GetResourceRequirements(j.Tool)
}

// GetContainerID extracts the container ID from a CWL document's requirements.
func GetContainerID(doc *cwl.Document) string {
	// Check DockerRequirement
	if req := doc.GetDockerRequirement(); req != nil {
		if req.DockerPull != "" {
			return req.DockerPull
		}
		if req.DockerImageID != "" {
			return req.DockerImageID
		}
	}

	// Check ApptainerRequirement
	if req := doc.GetApptainerRequirement(); req != nil {
		if req.ApptainerPull != "" {
			return req.ApptainerPull
		}
		if req.ApptainerFile != "" {
			return req.ApptainerFile
		}
	}

	return ""
}

// GetResourceRequirements extracts resource requirements from a CWL document.
func GetResourceRequirements(doc *cwl.Document) (cpu int, memoryMB int) {
	cpu, memoryMB, _ = doc.GetResourceRequirements()
	return
}

// ToJSON serializes the job spec to JSON.
func (j *CWLJobSpec) ToJSON() ([]byte, error) {
	return json.MarshalIndent(j, "", "  ")
}

// FromJSON deserializes a job spec from JSON.
func FromJSON(data []byte) (*CWLJobSpec, error) {
	var spec CWLJobSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse job spec: %w", err)
	}
	return &spec, nil
}

// ResolveInputValue converts a CWL input value to a string for command-line use.
func ResolveInputValue(value interface{}) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case int, int64, float64:
		return fmt.Sprintf("%v", v), nil
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	case map[string]interface{}:
		// File or Directory object - extract path
		if class, ok := v["class"].(string); ok {
			if class == "File" || class == "Directory" {
				if path, ok := v["path"].(string); ok {
					return path, nil
				}
				if location, ok := v["location"].(string); ok {
					return location, nil
				}
			}
		}
		// Fall back to JSON
		data, err := json.Marshal(v)
		return string(data), err
	case []interface{}:
		// Array of values - convert each
		var values []string
		for _, item := range v {
			str, err := ResolveInputValue(item)
			if err != nil {
				return "", err
			}
			values = append(values, str)
		}
		data, err := json.Marshal(values)
		return string(data), err
	default:
		return fmt.Sprintf("%v", v), nil
	}
}
