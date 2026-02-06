// Package cwl provides CWL v1.2 parsing and type definitions.
package cwl

import (
	"encoding/json"
	"fmt"
)

// CWLVersion represents supported CWL versions.
const (
	CWLVersion12 = "v1.2"
	CWLVersion11 = "v1.1"
	CWLVersion10 = "v1.0"
)

// Class constants for CWL document types.
const (
	ClassWorkflow       = "Workflow"
	ClassCommandLineTool = "CommandLineTool"
	ClassExpressionTool = "ExpressionTool"
)

// Type constants for CWL types.
const (
	TypeNull      = "null"
	TypeBoolean   = "boolean"
	TypeInt       = "int"
	TypeLong      = "long"
	TypeFloat     = "float"
	TypeDouble    = "double"
	TypeString    = "string"
	TypeFile      = "File"
	TypeDirectory = "Directory"
	TypeArray     = "array"
	TypeRecord    = "record"
	TypeEnum      = "enum"
	TypeAny       = "Any"
)

// Document represents a CWL document (Workflow, CommandLineTool, or ExpressionTool).
type Document struct {
	CWLVersion   string        `json:"cwlVersion" yaml:"cwlVersion"`
	Class        string        `json:"class" yaml:"class"`
	ID           string        `json:"id,omitempty" yaml:"id,omitempty"`
	Label        string        `json:"label,omitempty" yaml:"label,omitempty"`
	Doc          string        `json:"doc,omitempty" yaml:"doc,omitempty"`
	Inputs       []Input       `json:"inputs" yaml:"inputs"`
	Outputs      []Output      `json:"outputs" yaml:"outputs"`
	Requirements []Requirement `json:"requirements,omitempty" yaml:"requirements,omitempty"`
	Hints        []Requirement `json:"hints,omitempty" yaml:"hints,omitempty"`

	// CommandLineTool specific
	BaseCommand interface{}       `json:"baseCommand,omitempty" yaml:"baseCommand,omitempty"`
	Arguments   []CommandLineArg  `json:"arguments,omitempty" yaml:"arguments,omitempty"`
	Stdin       string            `json:"stdin,omitempty" yaml:"stdin,omitempty"`
	Stdout      string            `json:"stdout,omitempty" yaml:"stdout,omitempty"`
	Stderr      string            `json:"stderr,omitempty" yaml:"stderr,omitempty"`
	SuccessCodes []int            `json:"successCodes,omitempty" yaml:"successCodes,omitempty"`

	// Workflow specific
	Steps []WorkflowStep `json:"steps,omitempty" yaml:"steps,omitempty"`

	// ExpressionTool specific
	Expression string `json:"expression,omitempty" yaml:"expression,omitempty"`
}

// Input represents a CWL input parameter.
type Input struct {
	ID             string              `json:"id" yaml:"id"`
	Type           interface{}         `json:"type" yaml:"type"`
	Label          string              `json:"label,omitempty" yaml:"label,omitempty"`
	Doc            string              `json:"doc,omitempty" yaml:"doc,omitempty"`
	Default        interface{}         `json:"default,omitempty" yaml:"default,omitempty"`
	SecondaryFiles []SecondaryFileSpec `json:"secondaryFiles,omitempty" yaml:"secondaryFiles,omitempty"`
	Streamable     bool                `json:"streamable,omitempty" yaml:"streamable,omitempty"`
	Format         interface{}         `json:"format,omitempty" yaml:"format,omitempty"`
	LoadContents   bool                `json:"loadContents,omitempty" yaml:"loadContents,omitempty"`
	LoadListing    string              `json:"loadListing,omitempty" yaml:"loadListing,omitempty"`

	// CommandLineTool specific
	InputBinding *CommandLineBinding `json:"inputBinding,omitempty" yaml:"inputBinding,omitempty"`
}

// Output represents a CWL output parameter.
type Output struct {
	ID             string              `json:"id" yaml:"id"`
	Type           interface{}         `json:"type" yaml:"type"`
	Label          string              `json:"label,omitempty" yaml:"label,omitempty"`
	Doc            string              `json:"doc,omitempty" yaml:"doc,omitempty"`
	SecondaryFiles []SecondaryFileSpec `json:"secondaryFiles,omitempty" yaml:"secondaryFiles,omitempty"`
	Streamable     bool                `json:"streamable,omitempty" yaml:"streamable,omitempty"`
	Format         interface{}         `json:"format,omitempty" yaml:"format,omitempty"`

	// CommandLineTool specific
	OutputBinding *CommandOutputBinding `json:"outputBinding,omitempty" yaml:"outputBinding,omitempty"`

	// Workflow specific
	OutputSource interface{} `json:"outputSource,omitempty" yaml:"outputSource,omitempty"`
	LinkMerge    string      `json:"linkMerge,omitempty" yaml:"linkMerge,omitempty"`
	PickValue    string      `json:"pickValue,omitempty" yaml:"pickValue,omitempty"`
}

// SecondaryFileSpec specifies secondary files for an input/output.
type SecondaryFileSpec struct {
	Pattern  string `json:"pattern" yaml:"pattern"`
	Required interface{} `json:"required,omitempty" yaml:"required,omitempty"`
}

// CommandLineBinding describes how to build a command line argument.
type CommandLineBinding struct {
	Position        int         `json:"position,omitempty" yaml:"position,omitempty"`
	Prefix          string      `json:"prefix,omitempty" yaml:"prefix,omitempty"`
	Separate        *bool       `json:"separate,omitempty" yaml:"separate,omitempty"`
	ItemSeparator   string      `json:"itemSeparator,omitempty" yaml:"itemSeparator,omitempty"`
	ValueFrom       string      `json:"valueFrom,omitempty" yaml:"valueFrom,omitempty"`
	ShellQuote      *bool       `json:"shellQuote,omitempty" yaml:"shellQuote,omitempty"`
}

// CommandOutputBinding describes how to capture output.
type CommandOutputBinding struct {
	Glob         interface{} `json:"glob,omitempty" yaml:"glob,omitempty"`
	LoadContents bool        `json:"loadContents,omitempty" yaml:"loadContents,omitempty"`
	LoadListing  string      `json:"loadListing,omitempty" yaml:"loadListing,omitempty"`
	OutputEval   string      `json:"outputEval,omitempty" yaml:"outputEval,omitempty"`
}

// CommandLineArg represents a command line argument.
type CommandLineArg struct {
	Position      int    `json:"position,omitempty" yaml:"position,omitempty"`
	Prefix        string `json:"prefix,omitempty" yaml:"prefix,omitempty"`
	Separate      *bool  `json:"separate,omitempty" yaml:"separate,omitempty"`
	ItemSeparator string `json:"itemSeparator,omitempty" yaml:"itemSeparator,omitempty"`
	ValueFrom     string `json:"valueFrom,omitempty" yaml:"valueFrom,omitempty"`
	ShellQuote    *bool  `json:"shellQuote,omitempty" yaml:"shellQuote,omitempty"`
}

// WorkflowStep represents a step in a CWL workflow.
type WorkflowStep struct {
	ID           string              `json:"id" yaml:"id"`
	In           []WorkflowStepInput `json:"in" yaml:"in"`
	Out          []interface{}       `json:"out" yaml:"out"`
	Run          interface{}         `json:"run" yaml:"run"`
	Requirements []Requirement       `json:"requirements,omitempty" yaml:"requirements,omitempty"`
	Hints        []Requirement       `json:"hints,omitempty" yaml:"hints,omitempty"`
	When         string              `json:"when,omitempty" yaml:"when,omitempty"`
	Scatter      interface{}         `json:"scatter,omitempty" yaml:"scatter,omitempty"`
	ScatterMethod string             `json:"scatterMethod,omitempty" yaml:"scatterMethod,omitempty"`
}

// WorkflowStepInput represents an input to a workflow step.
type WorkflowStepInput struct {
	ID        string      `json:"id" yaml:"id"`
	Source    interface{} `json:"source,omitempty" yaml:"source,omitempty"`
	Default   interface{} `json:"default,omitempty" yaml:"default,omitempty"`
	ValueFrom string      `json:"valueFrom,omitempty" yaml:"valueFrom,omitempty"`
	LinkMerge string      `json:"linkMerge,omitempty" yaml:"linkMerge,omitempty"`
	PickValue string      `json:"pickValue,omitempty" yaml:"pickValue,omitempty"`
}

// Requirement represents a CWL requirement or hint.
type Requirement struct {
	Class string `json:"class" yaml:"class"`

	// DockerRequirement
	DockerPull      string `json:"dockerPull,omitempty" yaml:"dockerPull,omitempty"`
	DockerLoad      string `json:"dockerLoad,omitempty" yaml:"dockerLoad,omitempty"`
	DockerFile      string `json:"dockerFile,omitempty" yaml:"dockerFile,omitempty"`
	DockerImport    string `json:"dockerImport,omitempty" yaml:"dockerImport,omitempty"`
	DockerImageID   string `json:"dockerImageId,omitempty" yaml:"dockerImageId,omitempty"`
	DockerOutputDir string `json:"dockerOutputDirectory,omitempty" yaml:"dockerOutputDirectory,omitempty"`

	// ApptainerRequirement (BV-BRC extension for native Apptainer/Singularity)
	ApptainerPull  string `json:"apptainerPull,omitempty" yaml:"apptainerPull,omitempty"`   // library://user/collection/container:tag
	ApptainerFile  string `json:"apptainerFile,omitempty" yaml:"apptainerFile,omitempty"`   // /path/to/container.sif
	ApptainerBuild string `json:"apptainerBuild,omitempty" yaml:"apptainerBuild,omitempty"` // Definition file to build from

	// CUDARequirement (cwltool extension for GPU)
	CUDAVersionMin        string `json:"cudaVersionMin,omitempty" yaml:"cudaVersionMin,omitempty"`
	CUDAComputeCapability string `json:"cudaComputeCapability,omitempty" yaml:"cudaComputeCapability,omitempty"`
	CUDADeviceCountMin    int    `json:"cudaDeviceCountMin,omitempty" yaml:"cudaDeviceCountMin,omitempty"`
	CUDADeviceCountMax    int    `json:"cudaDeviceCountMax,omitempty" yaml:"cudaDeviceCountMax,omitempty"`

	// ResourceRequirement
	CoresMin  interface{} `json:"coresMin,omitempty" yaml:"coresMin,omitempty"`
	CoresMax  interface{} `json:"coresMax,omitempty" yaml:"coresMax,omitempty"`
	RAMMin    interface{} `json:"ramMin,omitempty" yaml:"ramMin,omitempty"`
	RAMMax    interface{} `json:"ramMax,omitempty" yaml:"ramMax,omitempty"`
	TmpdirMin interface{} `json:"tmpdirMin,omitempty" yaml:"tmpdirMin,omitempty"`
	TmpdirMax interface{} `json:"tmpdirMax,omitempty" yaml:"tmpdirMax,omitempty"`
	OutdirMin interface{} `json:"outdirMin,omitempty" yaml:"outdirMin,omitempty"`
	OutdirMax interface{} `json:"outdirMax,omitempty" yaml:"outdirMax,omitempty"`

	// InlineJavascriptRequirement
	ExpressionLib []string `json:"expressionLib,omitempty" yaml:"expressionLib,omitempty"`
	// SchemaDefRequirement
	Types []interface{} `json:"types,omitempty" yaml:"types,omitempty"`
	// InitialWorkDirRequirement
	Listing interface{} `json:"listing,omitempty" yaml:"listing,omitempty"`
	// EnvVarRequirement
	EnvDef []EnvVarDef `json:"envDef,omitempty" yaml:"envDef,omitempty"`
	// ShellCommandRequirement (no additional fields)
	// NetworkAccess
	NetworkAccess interface{} `json:"networkAccess,omitempty" yaml:"networkAccess,omitempty"`
	// WorkReuse
	EnableReuse interface{} `json:"enableReuse,omitempty" yaml:"enableReuse,omitempty"`
	// ToolTimeLimit
	TimeLimit interface{} `json:"timelimit,omitempty" yaml:"timelimit,omitempty"`
	// SubworkflowFeatureRequirement, ScatterFeatureRequirement,
	// MultipleInputFeatureRequirement, StepInputExpressionRequirement
	// (no additional fields - just presence indicates feature is enabled)
}

// EnvVarDef defines an environment variable.
type EnvVarDef struct {
	EnvName  string `json:"envName" yaml:"envName"`
	EnvValue string `json:"envValue" yaml:"envValue"`
}

// FileValue represents a CWL File object.
type FileValue struct {
	Class          string            `json:"class"`
	Location       string            `json:"location,omitempty"`
	Path           string            `json:"path,omitempty"`
	Basename       string            `json:"basename,omitempty"`
	Dirname        string            `json:"dirname,omitempty"`
	Nameroot       string            `json:"nameroot,omitempty"`
	Nameext        string            `json:"nameext,omitempty"`
	Checksum       string            `json:"checksum,omitempty"`
	Size           int64             `json:"size,omitempty"`
	SecondaryFiles []interface{}     `json:"secondaryFiles,omitempty"`
	Format         string            `json:"format,omitempty"`
	Contents       string            `json:"contents,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// DirectoryValue represents a CWL Directory object.
type DirectoryValue struct {
	Class    string        `json:"class"`
	Location string        `json:"location,omitempty"`
	Path     string        `json:"path,omitempty"`
	Basename string        `json:"basename,omitempty"`
	Listing  []interface{} `json:"listing,omitempty"`
}

// CWLType represents a parsed CWL type.
type CWLType struct {
	Type     string     // base type: null, boolean, int, etc.
	Items    *CWLType   // for array types
	Fields   []CWLField // for record types
	Symbols  []string   // for enum types
	Name     string     // for named types
	Nullable bool       // true if type includes null
}

// CWLField represents a field in a record type.
type CWLField struct {
	Name string
	Type *CWLType
	Doc  string
}

// ParseType parses a CWL type specification into a structured CWLType.
func ParseType(t interface{}) (*CWLType, error) {
	switch v := t.(type) {
	case string:
		// Check for nullable shorthand (type?)
		if len(v) > 1 && v[len(v)-1] == '?' {
			innerType, err := ParseType(v[:len(v)-1])
			if err != nil {
				return nil, err
			}
			innerType.Nullable = true
			return innerType, nil
		}
		// Check for array shorthand (type[])
		if len(v) > 2 && v[len(v)-2:] == "[]" {
			itemType, err := ParseType(v[:len(v)-2])
			if err != nil {
				return nil, err
			}
			return &CWLType{Type: TypeArray, Items: itemType}, nil
		}
		return &CWLType{Type: v}, nil

	case []interface{}:
		// Union type
		hasNull := false
		var nonNullTypes []*CWLType
		for _, item := range v {
			if str, ok := item.(string); ok && str == TypeNull {
				hasNull = true
				continue
			}
			parsed, err := ParseType(item)
			if err != nil {
				return nil, err
			}
			nonNullTypes = append(nonNullTypes, parsed)
		}
		if len(nonNullTypes) == 1 {
			nonNullTypes[0].Nullable = hasNull
			return nonNullTypes[0], nil
		}
		// Multiple non-null types - return first with nullable flag
		if len(nonNullTypes) > 0 {
			nonNullTypes[0].Nullable = hasNull
			return nonNullTypes[0], nil
		}
		return &CWLType{Type: TypeNull}, nil

	case map[string]interface{}:
		typeStr, ok := v["type"].(string)
		if !ok {
			return nil, fmt.Errorf("type map missing 'type' field")
		}

		switch typeStr {
		case TypeArray:
			items, ok := v["items"]
			if !ok {
				return nil, fmt.Errorf("array type missing 'items' field")
			}
			itemType, err := ParseType(items)
			if err != nil {
				return nil, err
			}
			return &CWLType{Type: TypeArray, Items: itemType}, nil

		case TypeRecord:
			fields, _ := v["fields"].([]interface{})
			var cwlFields []CWLField
			for _, f := range fields {
				fm, ok := f.(map[string]interface{})
				if !ok {
					continue
				}
				fieldName, _ := fm["name"].(string)
				fieldType, err := ParseType(fm["type"])
				if err != nil {
					return nil, err
				}
				fieldDoc, _ := fm["doc"].(string)
				cwlFields = append(cwlFields, CWLField{
					Name: fieldName,
					Type: fieldType,
					Doc:  fieldDoc,
				})
			}
			name, _ := v["name"].(string)
			return &CWLType{Type: TypeRecord, Fields: cwlFields, Name: name}, nil

		case TypeEnum:
			symbols, _ := v["symbols"].([]interface{})
			var symStrs []string
			for _, s := range symbols {
				if str, ok := s.(string); ok {
					symStrs = append(symStrs, str)
				}
			}
			name, _ := v["name"].(string)
			return &CWLType{Type: TypeEnum, Symbols: symStrs, Name: name}, nil

		default:
			return &CWLType{Type: typeStr}, nil
		}

	default:
		return nil, fmt.Errorf("unsupported type specification: %T", t)
	}
}

// IsOptional returns true if the type allows null values.
func (t *CWLType) IsOptional() bool {
	return t.Nullable || t.Type == TypeNull
}

// BaseType returns the non-null base type.
func (t *CWLType) BaseType() string {
	return t.Type
}

// String returns a string representation of the type.
func (t *CWLType) String() string {
	base := t.Type
	if t.Type == TypeArray && t.Items != nil {
		base = t.Items.String() + "[]"
	}
	if t.Nullable {
		base += "?"
	}
	return base
}

// MarshalJSON implements json.Marshaler.
func (t *CWLType) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}
