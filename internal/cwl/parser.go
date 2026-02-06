package cwl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parser parses CWL documents.
type Parser struct {
	basePath string
}

// NewParser creates a new CWL parser.
func NewParser() *Parser {
	return &Parser{}
}

// ParseFile parses a CWL document from a file.
func (p *Parser) ParseFile(path string) (*Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open CWL file: %w", err)
	}
	defer f.Close()

	p.basePath = filepath.Dir(path)
	return p.Parse(f)
}

// Parse parses a CWL document from a reader.
func (p *Parser) Parse(r io.Reader) (*Document, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read CWL document: %w", err)
	}

	return p.ParseBytes(data)
}

// ParseBytes parses a CWL document from bytes.
func (p *Parser) ParseBytes(data []byte) (*Document, error) {
	// Try YAML first (which also handles JSON)
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse CWL document: %w", err)
	}

	return p.parseDocument(raw)
}

// ParseString parses a CWL document from a string.
func (p *Parser) ParseString(s string) (*Document, error) {
	return p.ParseBytes([]byte(s))
}

// parseDocument parses a raw map into a Document.
func (p *Parser) parseDocument(raw map[string]interface{}) (*Document, error) {
	doc := &Document{}

	// Parse cwlVersion
	if v, ok := raw["cwlVersion"].(string); ok {
		doc.CWLVersion = v
	} else {
		return nil, fmt.Errorf("missing or invalid cwlVersion")
	}

	// Validate version
	if !isValidVersion(doc.CWLVersion) {
		return nil, fmt.Errorf("unsupported CWL version: %s", doc.CWLVersion)
	}

	// Parse class
	if c, ok := raw["class"].(string); ok {
		doc.Class = c
	} else {
		return nil, fmt.Errorf("missing or invalid class")
	}

	// Parse common fields
	if id, ok := raw["id"].(string); ok {
		doc.ID = id
	}
	if label, ok := raw["label"].(string); ok {
		doc.Label = label
	}
	if docStr, ok := raw["doc"].(string); ok {
		doc.Doc = docStr
	}

	// Parse inputs
	inputs, err := p.parseInputs(raw["inputs"])
	if err != nil {
		return nil, fmt.Errorf("failed to parse inputs: %w", err)
	}
	doc.Inputs = inputs

	// Parse outputs
	outputs, err := p.parseOutputs(raw["outputs"])
	if err != nil {
		return nil, fmt.Errorf("failed to parse outputs: %w", err)
	}
	doc.Outputs = outputs

	// Parse requirements
	if reqs, ok := raw["requirements"]; ok {
		requirements, err := p.parseRequirements(reqs)
		if err != nil {
			return nil, fmt.Errorf("failed to parse requirements: %w", err)
		}
		doc.Requirements = requirements
	}

	// Parse hints
	if hints, ok := raw["hints"]; ok {
		hintsList, err := p.parseRequirements(hints)
		if err != nil {
			return nil, fmt.Errorf("failed to parse hints: %w", err)
		}
		doc.Hints = hintsList
	}

	// Parse class-specific fields
	switch doc.Class {
	case ClassCommandLineTool:
		if err := p.parseCommandLineTool(doc, raw); err != nil {
			return nil, err
		}
	case ClassWorkflow:
		if err := p.parseWorkflow(doc, raw); err != nil {
			return nil, err
		}
	case ClassExpressionTool:
		if expr, ok := raw["expression"].(string); ok {
			doc.Expression = expr
		}
	default:
		return nil, fmt.Errorf("unsupported class: %s", doc.Class)
	}

	return doc, nil
}

// parseInputs parses CWL inputs (handles both array and map formats).
func (p *Parser) parseInputs(raw interface{}) ([]Input, error) {
	if raw == nil {
		return nil, nil
	}

	var inputs []Input

	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("invalid input entry")
			}
			input, err := p.parseInput(m)
			if err != nil {
				return nil, err
			}
			inputs = append(inputs, input)
		}
	case map[string]interface{}:
		for id, val := range v {
			m := make(map[string]interface{})
			m["id"] = id
			switch vv := val.(type) {
			case string:
				m["type"] = vv
			case map[string]interface{}:
				for k, kv := range vv {
					m[k] = kv
				}
				if _, hasID := m["id"]; !hasID {
					m["id"] = id
				}
			default:
				m["type"] = vv
			}
			input, err := p.parseInput(m)
			if err != nil {
				return nil, err
			}
			inputs = append(inputs, input)
		}
	default:
		return nil, fmt.Errorf("invalid inputs format")
	}

	return inputs, nil
}

// parseInput parses a single CWL input.
func (p *Parser) parseInput(m map[string]interface{}) (Input, error) {
	input := Input{}

	if id, ok := m["id"].(string); ok {
		input.ID = id
	} else {
		return input, fmt.Errorf("input missing id")
	}

	if t, ok := m["type"]; ok {
		input.Type = t
	} else {
		return input, fmt.Errorf("input %s missing type", input.ID)
	}

	if label, ok := m["label"].(string); ok {
		input.Label = label
	}
	if doc, ok := m["doc"].(string); ok {
		input.Doc = doc
	}
	if def, ok := m["default"]; ok {
		input.Default = def
	}
	if lc, ok := m["loadContents"].(bool); ok {
		input.LoadContents = lc
	}
	if ll, ok := m["loadListing"].(string); ok {
		input.LoadListing = ll
	}
	if streamable, ok := m["streamable"].(bool); ok {
		input.Streamable = streamable
	}
	if format, ok := m["format"]; ok {
		input.Format = format
	}

	// Parse secondaryFiles
	if sf, ok := m["secondaryFiles"]; ok {
		secondaryFiles, err := p.parseSecondaryFiles(sf)
		if err != nil {
			return input, err
		}
		input.SecondaryFiles = secondaryFiles
	}

	// Parse inputBinding
	if ib, ok := m["inputBinding"].(map[string]interface{}); ok {
		binding, err := p.parseInputBinding(ib)
		if err != nil {
			return input, err
		}
		input.InputBinding = binding
	}

	return input, nil
}

// parseOutputs parses CWL outputs (handles both array and map formats).
func (p *Parser) parseOutputs(raw interface{}) ([]Output, error) {
	if raw == nil {
		return nil, nil
	}

	var outputs []Output

	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("invalid output entry")
			}
			output, err := p.parseOutput(m)
			if err != nil {
				return nil, err
			}
			outputs = append(outputs, output)
		}
	case map[string]interface{}:
		for id, val := range v {
			m := make(map[string]interface{})
			m["id"] = id
			switch vv := val.(type) {
			case string:
				m["type"] = vv
			case map[string]interface{}:
				for k, kv := range vv {
					m[k] = kv
				}
				if _, hasID := m["id"]; !hasID {
					m["id"] = id
				}
			default:
				m["type"] = vv
			}
			output, err := p.parseOutput(m)
			if err != nil {
				return nil, err
			}
			outputs = append(outputs, output)
		}
	default:
		return nil, fmt.Errorf("invalid outputs format")
	}

	return outputs, nil
}

// parseOutput parses a single CWL output.
func (p *Parser) parseOutput(m map[string]interface{}) (Output, error) {
	output := Output{}

	if id, ok := m["id"].(string); ok {
		output.ID = id
	} else {
		return output, fmt.Errorf("output missing id")
	}

	if t, ok := m["type"]; ok {
		output.Type = t
	} else {
		return output, fmt.Errorf("output %s missing type", output.ID)
	}

	if label, ok := m["label"].(string); ok {
		output.Label = label
	}
	if doc, ok := m["doc"].(string); ok {
		output.Doc = doc
	}
	if streamable, ok := m["streamable"].(bool); ok {
		output.Streamable = streamable
	}
	if format, ok := m["format"]; ok {
		output.Format = format
	}
	if os, ok := m["outputSource"]; ok {
		output.OutputSource = os
	}
	if lm, ok := m["linkMerge"].(string); ok {
		output.LinkMerge = lm
	}
	if pv, ok := m["pickValue"].(string); ok {
		output.PickValue = pv
	}

	// Parse secondaryFiles
	if sf, ok := m["secondaryFiles"]; ok {
		secondaryFiles, err := p.parseSecondaryFiles(sf)
		if err != nil {
			return output, err
		}
		output.SecondaryFiles = secondaryFiles
	}

	// Parse outputBinding
	if ob, ok := m["outputBinding"].(map[string]interface{}); ok {
		binding, err := p.parseOutputBinding(ob)
		if err != nil {
			return output, err
		}
		output.OutputBinding = binding
	}

	return output, nil
}

// parseSecondaryFiles parses secondary file specifications.
func (p *Parser) parseSecondaryFiles(raw interface{}) ([]SecondaryFileSpec, error) {
	var specs []SecondaryFileSpec

	switch v := raw.(type) {
	case string:
		specs = append(specs, SecondaryFileSpec{Pattern: v})
	case []interface{}:
		for _, item := range v {
			switch iv := item.(type) {
			case string:
				specs = append(specs, SecondaryFileSpec{Pattern: iv})
			case map[string]interface{}:
				spec := SecondaryFileSpec{}
				if pattern, ok := iv["pattern"].(string); ok {
					spec.Pattern = pattern
				}
				if req, ok := iv["required"]; ok {
					spec.Required = req
				}
				specs = append(specs, spec)
			}
		}
	case map[string]interface{}:
		spec := SecondaryFileSpec{}
		if pattern, ok := v["pattern"].(string); ok {
			spec.Pattern = pattern
		}
		if req, ok := v["required"]; ok {
			spec.Required = req
		}
		specs = append(specs, spec)
	}

	return specs, nil
}

// parseInputBinding parses a command line input binding.
func (p *Parser) parseInputBinding(m map[string]interface{}) (*CommandLineBinding, error) {
	binding := &CommandLineBinding{}

	if pos, ok := m["position"]; ok {
		switch v := pos.(type) {
		case int:
			binding.Position = v
		case float64:
			binding.Position = int(v)
		}
	}
	if prefix, ok := m["prefix"].(string); ok {
		binding.Prefix = prefix
	}
	if sep, ok := m["separate"].(bool); ok {
		binding.Separate = &sep
	}
	if itemSep, ok := m["itemSeparator"].(string); ok {
		binding.ItemSeparator = itemSep
	}
	if vf, ok := m["valueFrom"].(string); ok {
		binding.ValueFrom = vf
	}
	if sq, ok := m["shellQuote"].(bool); ok {
		binding.ShellQuote = &sq
	}

	return binding, nil
}

// parseOutputBinding parses a command output binding.
func (p *Parser) parseOutputBinding(m map[string]interface{}) (*CommandOutputBinding, error) {
	binding := &CommandOutputBinding{}

	if glob, ok := m["glob"]; ok {
		binding.Glob = glob
	}
	if lc, ok := m["loadContents"].(bool); ok {
		binding.LoadContents = lc
	}
	if ll, ok := m["loadListing"].(string); ok {
		binding.LoadListing = ll
	}
	if oe, ok := m["outputEval"].(string); ok {
		binding.OutputEval = oe
	}

	return binding, nil
}

// parseRequirements parses CWL requirements or hints.
func (p *Parser) parseRequirements(raw interface{}) ([]Requirement, error) {
	var reqs []Requirement

	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			req, err := p.parseRequirement(m)
			if err != nil {
				return nil, err
			}
			reqs = append(reqs, req)
		}
	case map[string]interface{}:
		for class, val := range v {
			m := make(map[string]interface{})
			m["class"] = class
			if vm, ok := val.(map[string]interface{}); ok {
				for k, kv := range vm {
					m[k] = kv
				}
			}
			req, err := p.parseRequirement(m)
			if err != nil {
				return nil, err
			}
			reqs = append(reqs, req)
		}
	}

	return reqs, nil
}

// parseRequirement parses a single CWL requirement.
func (p *Parser) parseRequirement(m map[string]interface{}) (Requirement, error) {
	req := Requirement{}

	if class, ok := m["class"].(string); ok {
		req.Class = class
	} else {
		return req, fmt.Errorf("requirement missing class")
	}

	// DockerRequirement
	if pull, ok := m["dockerPull"].(string); ok {
		req.DockerPull = pull
	}
	if load, ok := m["dockerLoad"].(string); ok {
		req.DockerLoad = load
	}
	if file, ok := m["dockerFile"].(string); ok {
		req.DockerFile = file
	}
	if imp, ok := m["dockerImport"].(string); ok {
		req.DockerImport = imp
	}
	if imgID, ok := m["dockerImageId"].(string); ok {
		req.DockerImageID = imgID
	}
	if outDir, ok := m["dockerOutputDirectory"].(string); ok {
		req.DockerOutputDir = outDir
	}

	// ResourceRequirement
	if v, ok := m["coresMin"]; ok {
		req.CoresMin = v
	}
	if v, ok := m["coresMax"]; ok {
		req.CoresMax = v
	}
	if v, ok := m["ramMin"]; ok {
		req.RAMMin = v
	}
	if v, ok := m["ramMax"]; ok {
		req.RAMMax = v
	}
	if v, ok := m["tmpdirMin"]; ok {
		req.TmpdirMin = v
	}
	if v, ok := m["tmpdirMax"]; ok {
		req.TmpdirMax = v
	}
	if v, ok := m["outdirMin"]; ok {
		req.OutdirMin = v
	}
	if v, ok := m["outdirMax"]; ok {
		req.OutdirMax = v
	}

	// ApptainerRequirement (BV-BRC extension)
	if pull, ok := m["apptainerPull"].(string); ok {
		req.ApptainerPull = pull
	}
	if file, ok := m["apptainerFile"].(string); ok {
		req.ApptainerFile = file
	}
	if build, ok := m["apptainerBuild"].(string); ok {
		req.ApptainerBuild = build
	}

	// CUDARequirement (cwltool extension)
	if v, ok := m["cudaVersionMin"].(string); ok {
		req.CUDAVersionMin = v
	}
	if v, ok := m["cudaComputeCapability"].(string); ok {
		req.CUDAComputeCapability = v
	}
	if v, ok := m["cudaDeviceCountMin"].(int); ok {
		req.CUDADeviceCountMin = v
	} else if v, ok := m["cudaDeviceCountMin"].(float64); ok {
		req.CUDADeviceCountMin = int(v)
	}
	if v, ok := m["cudaDeviceCountMax"].(int); ok {
		req.CUDADeviceCountMax = v
	} else if v, ok := m["cudaDeviceCountMax"].(float64); ok {
		req.CUDADeviceCountMax = int(v)
	}

	// InlineJavascriptRequirement
	if lib, ok := m["expressionLib"].([]interface{}); ok {
		for _, item := range lib {
			if s, ok := item.(string); ok {
				req.ExpressionLib = append(req.ExpressionLib, s)
			}
		}
	}

	// SchemaDefRequirement
	if types, ok := m["types"]; ok {
		req.Types = []interface{}{types}
	}

	// InitialWorkDirRequirement
	if listing, ok := m["listing"]; ok {
		req.Listing = listing
	}

	// EnvVarRequirement
	if envDef, ok := m["envDef"].([]interface{}); ok {
		for _, item := range envDef {
			if em, ok := item.(map[string]interface{}); ok {
				def := EnvVarDef{}
				if name, ok := em["envName"].(string); ok {
					def.EnvName = name
				}
				if val, ok := em["envValue"].(string); ok {
					def.EnvValue = val
				}
				req.EnvDef = append(req.EnvDef, def)
			}
		}
	}

	// NetworkAccess
	if na, ok := m["networkAccess"]; ok {
		req.NetworkAccess = na
	}

	// WorkReuse
	if er, ok := m["enableReuse"]; ok {
		req.EnableReuse = er
	}

	// ToolTimeLimit
	if tl, ok := m["timelimit"]; ok {
		req.TimeLimit = tl
	}

	return req, nil
}

// parseCommandLineTool parses CommandLineTool-specific fields.
func (p *Parser) parseCommandLineTool(doc *Document, raw map[string]interface{}) error {
	// baseCommand can be string or array of strings
	if bc, ok := raw["baseCommand"]; ok {
		doc.BaseCommand = bc
	}

	// arguments
	if args, ok := raw["arguments"].([]interface{}); ok {
		for _, arg := range args {
			switch v := arg.(type) {
			case string:
				doc.Arguments = append(doc.Arguments, CommandLineArg{ValueFrom: v})
			case map[string]interface{}:
				cla := CommandLineArg{}
				if pos, ok := v["position"]; ok {
					switch pv := pos.(type) {
					case int:
						cla.Position = pv
					case float64:
						cla.Position = int(pv)
					}
				}
				if prefix, ok := v["prefix"].(string); ok {
					cla.Prefix = prefix
				}
				if sep, ok := v["separate"].(bool); ok {
					cla.Separate = &sep
				}
				if itemSep, ok := v["itemSeparator"].(string); ok {
					cla.ItemSeparator = itemSep
				}
				if vf, ok := v["valueFrom"].(string); ok {
					cla.ValueFrom = vf
				}
				if sq, ok := v["shellQuote"].(bool); ok {
					cla.ShellQuote = &sq
				}
				doc.Arguments = append(doc.Arguments, cla)
			}
		}
	}

	// stdin, stdout, stderr
	if stdin, ok := raw["stdin"].(string); ok {
		doc.Stdin = stdin
	}
	if stdout, ok := raw["stdout"].(string); ok {
		doc.Stdout = stdout
	}
	if stderr, ok := raw["stderr"].(string); ok {
		doc.Stderr = stderr
	}

	// successCodes
	if sc, ok := raw["successCodes"].([]interface{}); ok {
		for _, code := range sc {
			switch v := code.(type) {
			case int:
				doc.SuccessCodes = append(doc.SuccessCodes, v)
			case float64:
				doc.SuccessCodes = append(doc.SuccessCodes, int(v))
			}
		}
	}

	return nil
}

// parseWorkflow parses Workflow-specific fields.
func (p *Parser) parseWorkflow(doc *Document, raw map[string]interface{}) error {
	steps, ok := raw["steps"]
	if !ok {
		return nil
	}

	switch v := steps.(type) {
	case []interface{}:
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			step, err := p.parseWorkflowStep(m)
			if err != nil {
				return err
			}
			doc.Steps = append(doc.Steps, step)
		}
	case map[string]interface{}:
		for id, val := range v {
			m, ok := val.(map[string]interface{})
			if !ok {
				continue
			}
			m["id"] = id
			step, err := p.parseWorkflowStep(m)
			if err != nil {
				return err
			}
			doc.Steps = append(doc.Steps, step)
		}
	}

	return nil
}

// parseWorkflowStep parses a single workflow step.
func (p *Parser) parseWorkflowStep(m map[string]interface{}) (WorkflowStep, error) {
	step := WorkflowStep{}

	if id, ok := m["id"].(string); ok {
		step.ID = id
	} else {
		return step, fmt.Errorf("step missing id")
	}

	// Parse step inputs
	if in, ok := m["in"]; ok {
		inputs, err := p.parseStepInputs(in)
		if err != nil {
			return step, err
		}
		step.In = inputs
	}

	// Parse step outputs
	if out, ok := m["out"].([]interface{}); ok {
		step.Out = out
	}

	// Run can be inline document, file path, or reference
	if run, ok := m["run"]; ok {
		step.Run = run
	}

	// Requirements and hints
	if reqs, ok := m["requirements"]; ok {
		requirements, err := p.parseRequirements(reqs)
		if err != nil {
			return step, err
		}
		step.Requirements = requirements
	}
	if hints, ok := m["hints"]; ok {
		hintsList, err := p.parseRequirements(hints)
		if err != nil {
			return step, err
		}
		step.Hints = hintsList
	}

	// Conditional execution
	if when, ok := m["when"].(string); ok {
		step.When = when
	}

	// Scatter
	if scatter, ok := m["scatter"]; ok {
		step.Scatter = scatter
	}
	if sm, ok := m["scatterMethod"].(string); ok {
		step.ScatterMethod = sm
	}

	return step, nil
}

// parseStepInputs parses workflow step inputs.
func (p *Parser) parseStepInputs(raw interface{}) ([]WorkflowStepInput, error) {
	var inputs []WorkflowStepInput

	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			switch iv := item.(type) {
			case string:
				// Short form: just the source
				parts := strings.SplitN(iv, "/", 2)
				if len(parts) == 2 {
					inputs = append(inputs, WorkflowStepInput{
						ID:     parts[1],
						Source: iv,
					})
				} else {
					inputs = append(inputs, WorkflowStepInput{
						ID:     iv,
						Source: iv,
					})
				}
			case map[string]interface{}:
				input, err := p.parseStepInput(iv)
				if err != nil {
					return nil, err
				}
				inputs = append(inputs, input)
			}
		}
	case map[string]interface{}:
		for id, val := range v {
			m := make(map[string]interface{})
			m["id"] = id
			switch vv := val.(type) {
			case string:
				m["source"] = vv
			case []interface{}:
				m["source"] = vv
			case map[string]interface{}:
				for k, kv := range vv {
					m[k] = kv
				}
			default:
				m["source"] = vv
			}
			input, err := p.parseStepInput(m)
			if err != nil {
				return nil, err
			}
			inputs = append(inputs, input)
		}
	}

	return inputs, nil
}

// parseStepInput parses a single step input.
func (p *Parser) parseStepInput(m map[string]interface{}) (WorkflowStepInput, error) {
	input := WorkflowStepInput{}

	if id, ok := m["id"].(string); ok {
		input.ID = id
	}
	if source, ok := m["source"]; ok {
		input.Source = source
	}
	if def, ok := m["default"]; ok {
		input.Default = def
	}
	if vf, ok := m["valueFrom"].(string); ok {
		input.ValueFrom = vf
	}
	if lm, ok := m["linkMerge"].(string); ok {
		input.LinkMerge = lm
	}
	if pv, ok := m["pickValue"].(string); ok {
		input.PickValue = pv
	}

	return input, nil
}

// isValidVersion checks if the CWL version is supported.
func isValidVersion(version string) bool {
	switch version {
	case CWLVersion12, CWLVersion11, CWLVersion10:
		return true
	default:
		return false
	}
}

// ContentHash computes a SHA-256 hash of the document content.
func ContentHash(data []byte) string {
	hash := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(hash[:])
}

// ToJSON converts a document to JSON.
func (doc *Document) ToJSON() ([]byte, error) {
	return json.MarshalIndent(doc, "", "  ")
}

// ToYAML converts a document to YAML.
func (doc *Document) ToYAML() ([]byte, error) {
	return yaml.Marshal(doc)
}
