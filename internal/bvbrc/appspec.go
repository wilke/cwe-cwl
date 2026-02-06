// Package bvbrc provides BV-BRC integration utilities.
package bvbrc

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/BV-BRC/cwe-cwl/internal/cwl"
)

// AppSpec represents a BV-BRC application specification.
type AppSpec struct {
	ID          string         `json:"id"`
	Script      string         `json:"script,omitempty"`
	Label       string         `json:"label"`
	Description string         `json:"description,omitempty"`
	Parameters  []AppParameter `json:"parameters"`
}

// AppParameter represents a parameter in a BV-BRC app spec.
type AppParameter struct {
	ID       string      `json:"id"`
	Label    string      `json:"label,omitempty"`
	Required int         `json:"required"` // 0 = optional, 1 = required
	Default  interface{} `json:"default,omitempty"`
	Desc     string      `json:"desc,omitempty"`
	Type     string      `json:"type"`
	Enum     string      `json:"enum,omitempty"`  // Comma-separated enum values
	WSType   string      `json:"wstype,omitempty"` // Workspace object type
}

// JobSpec represents the parameters submitted to start_app2.
type JobSpec struct {
	OutputPath string            `json:"output_path,omitempty"`
	OutputFile string            `json:"output_file,omitempty"`
	Params     map[string]string `json:"params"`
}

// StartParams are the optional parameters for start_app2.
type StartParams struct {
	ParentID        string            `json:"parent_id,omitempty"`
	Workspace       string            `json:"workspace,omitempty"`
	BaseURL         string            `json:"base_url,omitempty"`
	ContainerID     string            `json:"container_id,omitempty"`
	UserMetadata    string            `json:"user_metadata,omitempty"`
	Reservation     string            `json:"reservation,omitempty"`
	DataContainerID string            `json:"data_container_id,omitempty"`
	DisablePreflight int              `json:"disable_preflight,omitempty"`
	PreflightData   map[string]string `json:"preflight_data,omitempty"`
}

// CWLToAppSpec converts a CWL CommandLineTool to a BV-BRC AppSpec.
func CWLToAppSpec(doc *cwl.Document) (*AppSpec, error) {
	if doc.Class != "CommandLineTool" {
		return nil, fmt.Errorf("expected CommandLineTool, got %s", doc.Class)
	}

	spec := &AppSpec{
		ID:          sanitizeAppID(doc.ID),
		Label:       doc.Label,
		Description: doc.Doc,
		Parameters:  make([]AppParameter, 0, len(doc.Inputs)+2), // +2 for output_path, output_file
	}

	// Add standard output parameters first
	spec.Parameters = append(spec.Parameters, AppParameter{
		ID:       "output_path",
		Label:    "Output Folder",
		Required: 1,
		Desc:     "Workspace path for output files",
		Type:     "folder",
	})
	spec.Parameters = append(spec.Parameters, AppParameter{
		ID:       "output_file",
		Label:    "Output File Basename",
		Required: 0,
		Desc:     "Basename for output files",
		Type:     "wsid",
	})

	// Convert CWL inputs to BV-BRC parameters
	for _, input := range doc.Inputs {
		param, err := cwlInputToParameter(input)
		if err != nil {
			return nil, fmt.Errorf("failed to convert input %s: %w", input.ID, err)
		}
		spec.Parameters = append(spec.Parameters, param)
	}

	return spec, nil
}

// cwlInputToParameter converts a CWL input to a BV-BRC parameter.
func cwlInputToParameter(input cwl.Input) (AppParameter, error) {
	param := AppParameter{
		ID:    input.ID,
		Label: input.Label,
		Desc:  input.Doc,
	}

	// Parse type and determine if required
	typeStr, isOptional := parseTypeString(input.Type)
	if isOptional || input.Default != nil {
		param.Required = 0
	} else {
		param.Required = 1
	}

	// Set default value
	if input.Default != nil {
		param.Default = input.Default
	}

	// Map CWL type to BV-BRC type
	switch typeStr {
	case "File":
		param.Type = "wsid"
		param.WSType = "file"
	case "Directory":
		param.Type = "folder"
	case "string":
		param.Type = "string"
	case "int", "long":
		param.Type = "int"
	case "float", "double":
		param.Type = "float"
	case "boolean":
		param.Type = "bool"
	case "File[]":
		param.Type = "wsid"
		param.WSType = "file_list"
	default:
		// Check if it's an enum type
		if strings.HasPrefix(typeStr, "enum:") {
			param.Type = "enum"
			param.Enum = strings.TrimPrefix(typeStr, "enum:")
		} else {
			// Default to string for unknown types
			param.Type = "string"
		}
	}

	return param, nil
}

// parseTypeString parses a CWL type and returns the base type and whether it's optional.
func parseTypeString(typeVal interface{}) (string, bool) {
	switch t := typeVal.(type) {
	case string:
		// Handle shorthand optional syntax: "string?"
		if strings.HasSuffix(t, "?") {
			return strings.TrimSuffix(t, "?"), true
		}
		// Handle array shorthand: "File[]"
		return t, false

	case []interface{}:
		// Union type like ["null", "File"]
		hasNull := false
		var baseType string
		for _, item := range t {
			if s, ok := item.(string); ok {
				if s == "null" {
					hasNull = true
				} else {
					baseType = s
				}
			} else if m, ok := item.(map[string]interface{}); ok {
				// Complex type like {type: enum, symbols: [...]}
				if typeType, ok := m["type"].(string); ok {
					if typeType == "enum" {
						if symbols, ok := m["symbols"].([]interface{}); ok {
							var enumVals []string
							for _, s := range symbols {
								if sv, ok := s.(string); ok {
									enumVals = append(enumVals, sv)
								}
							}
							baseType = "enum:" + strings.Join(enumVals, ",")
						}
					} else if typeType == "array" {
						if items, ok := m["items"].(string); ok {
							baseType = items + "[]"
						}
					} else {
						baseType = typeType
					}
				}
			}
		}
		return baseType, hasNull

	case map[string]interface{}:
		// Complex type definition
		if typeType, ok := t["type"].(string); ok {
			if typeType == "enum" {
				if symbols, ok := t["symbols"].([]interface{}); ok {
					var enumVals []string
					for _, s := range symbols {
						if sv, ok := s.(string); ok {
							enumVals = append(enumVals, sv)
						}
					}
					return "enum:" + strings.Join(enumVals, ","), false
				}
			} else if typeType == "array" {
				if items, ok := t["items"].(string); ok {
					return items + "[]", false
				}
			}
			return typeType, false
		}
	}

	return "string", false
}

// sanitizeAppID converts a CWL tool ID to a valid BV-BRC app ID.
func sanitizeAppID(id string) string {
	// Remove file path components
	if idx := strings.LastIndex(id, "/"); idx >= 0 {
		id = id[idx+1:]
	}
	// Remove .cwl extension
	id = strings.TrimSuffix(id, ".cwl")
	// Replace invalid characters
	id = strings.ReplaceAll(id, "-", "_")
	id = strings.ReplaceAll(id, ".", "_")
	return id
}

// BuildJobSpec builds a BV-BRC job spec from CWL inputs.
func BuildJobSpec(doc *cwl.Document, inputs map[string]interface{}, outputPath string) (map[string]string, error) {
	params := make(map[string]string)

	// Set output path
	params["output_path"] = outputPath

	// Convert each input value to string format
	for _, input := range doc.Inputs {
		value, ok := inputs[input.ID]
		if !ok {
			// Check if required
			typeStr, isOptional := parseTypeString(input.Type)
			if !isOptional && input.Default == nil && typeStr != "" {
				return nil, fmt.Errorf("missing required input: %s", input.ID)
			}
			continue
		}

		strValue, err := valueToString(value)
		if err != nil {
			return nil, fmt.Errorf("failed to convert input %s: %w", input.ID, err)
		}
		params[input.ID] = strValue
	}

	return params, nil
}

// valueToString converts a CWL value to a string for BV-BRC job spec.
func valueToString(value interface{}) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case int, int64, float64:
		return fmt.Sprintf("%v", v), nil
	case bool:
		if v {
			return "1", nil
		}
		return "0", nil
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
		// Array of values - convert to JSON array of paths or values
		var values []string
		for _, item := range v {
			str, err := valueToString(item)
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
