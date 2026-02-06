package cwl

import (
	"fmt"
	"strings"
)

// WorkflowAnalyzer provides analysis utilities for CWL workflows.
type WorkflowAnalyzer struct {
	doc *Document
}

// NewWorkflowAnalyzer creates a new workflow analyzer.
func NewWorkflowAnalyzer(doc *Document) *WorkflowAnalyzer {
	return &WorkflowAnalyzer{doc: doc}
}

// StepDependency represents a dependency between steps.
type StepDependency struct {
	StepID     string
	DependsOn  []string
	InputSources map[string][]string // input ID -> source step IDs
}

// GetStepDependencies analyzes the workflow and returns step dependencies.
func (wa *WorkflowAnalyzer) GetStepDependencies() ([]StepDependency, error) {
	if wa.doc.Class != ClassWorkflow {
		return nil, fmt.Errorf("document is not a Workflow")
	}

	var deps []StepDependency

	for _, step := range wa.doc.Steps {
		dep := StepDependency{
			StepID:       step.ID,
			DependsOn:    []string{},
			InputSources: make(map[string][]string),
		}

		dependencySet := make(map[string]bool)

		for _, in := range step.In {
			sources := wa.getSources(in.Source)
			dep.InputSources[in.ID] = sources

			for _, source := range sources {
				// Parse source to extract step ID
				stepID := wa.extractStepID(source)
				if stepID != "" && stepID != step.ID {
					if !dependencySet[stepID] {
						dependencySet[stepID] = true
						dep.DependsOn = append(dep.DependsOn, stepID)
					}
				}
			}
		}

		deps = append(deps, dep)
	}

	return deps, nil
}

// getSources extracts source references from a step input.
func (wa *WorkflowAnalyzer) getSources(source interface{}) []string {
	if source == nil {
		return nil
	}

	switch v := source.(type) {
	case string:
		return []string{v}
	case []interface{}:
		var sources []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				sources = append(sources, s)
			}
		}
		return sources
	default:
		return nil
	}
}

// extractStepID extracts the step ID from a source reference.
// Source references are in the format: step_id/output_id or just input_id for workflow inputs
func (wa *WorkflowAnalyzer) extractStepID(source string) string {
	// Remove leading # if present (for full URI references)
	source = strings.TrimPrefix(source, "#")

	parts := strings.SplitN(source, "/", 2)
	if len(parts) != 2 {
		// This is a workflow input, not a step output
		return ""
	}

	stepID := parts[0]

	// Verify this is actually a step (not a workflow input)
	for _, step := range wa.doc.Steps {
		if step.ID == stepID {
			return stepID
		}
	}

	return ""
}

// GetStepOutputIDs returns the output IDs for a step.
func (wa *WorkflowAnalyzer) GetStepOutputIDs(stepID string) []string {
	for _, step := range wa.doc.Steps {
		if step.ID == stepID {
			var outputs []string
			for _, out := range step.Out {
				switch v := out.(type) {
				case string:
					outputs = append(outputs, v)
				case map[string]interface{}:
					if id, ok := v["id"].(string); ok {
						outputs = append(outputs, id)
					}
				}
			}
			return outputs
		}
	}
	return nil
}

// GetStep returns a step by ID.
func (wa *WorkflowAnalyzer) GetStep(stepID string) *WorkflowStep {
	for i := range wa.doc.Steps {
		if wa.doc.Steps[i].ID == stepID {
			return &wa.doc.Steps[i]
		}
	}
	return nil
}

// GetWorkflowInputs returns workflow-level inputs.
func (wa *WorkflowAnalyzer) GetWorkflowInputs() []Input {
	return wa.doc.Inputs
}

// GetWorkflowOutputs returns workflow-level outputs.
func (wa *WorkflowAnalyzer) GetWorkflowOutputs() []Output {
	return wa.doc.Outputs
}

// ResolveStepTool resolves the tool for a workflow step.
// Returns the parsed document if run is inline, or the path if it's a file reference.
func (wa *WorkflowAnalyzer) ResolveStepTool(step *WorkflowStep) (*Document, string, error) {
	switch v := step.Run.(type) {
	case string:
		// File path or reference
		return nil, v, nil
	case map[string]interface{}:
		// Inline tool definition
		parser := NewParser()
		doc, err := parser.parseDocument(v)
		if err != nil {
			return nil, "", fmt.Errorf("failed to parse inline tool: %w", err)
		}
		return doc, "", nil
	default:
		return nil, "", fmt.Errorf("unsupported run type: %T", v)
	}
}

// CollectOutputSources maps workflow outputs to their step sources.
func (wa *WorkflowAnalyzer) CollectOutputSources() map[string]string {
	sources := make(map[string]string)

	for _, out := range wa.doc.Outputs {
		switch v := out.OutputSource.(type) {
		case string:
			sources[out.ID] = v
		case []interface{}:
			if len(v) > 0 {
				if s, ok := v[0].(string); ok {
					sources[out.ID] = s
				}
			}
		}
	}

	return sources
}

// ValidateWorkflow performs basic validation on the workflow.
func (wa *WorkflowAnalyzer) ValidateWorkflow() []error {
	var errors []error

	if wa.doc.Class != ClassWorkflow {
		errors = append(errors, fmt.Errorf("document class is %s, expected Workflow", wa.doc.Class))
		return errors
	}

	// Check for duplicate step IDs
	stepIDs := make(map[string]bool)
	for _, step := range wa.doc.Steps {
		if stepIDs[step.ID] {
			errors = append(errors, fmt.Errorf("duplicate step ID: %s", step.ID))
		}
		stepIDs[step.ID] = true
	}

	// Check for duplicate input IDs
	inputIDs := make(map[string]bool)
	for _, input := range wa.doc.Inputs {
		if inputIDs[input.ID] {
			errors = append(errors, fmt.Errorf("duplicate input ID: %s", input.ID))
		}
		inputIDs[input.ID] = true
	}

	// Check for duplicate output IDs
	outputIDs := make(map[string]bool)
	for _, output := range wa.doc.Outputs {
		if outputIDs[output.ID] {
			errors = append(errors, fmt.Errorf("duplicate output ID: %s", output.ID))
		}
		outputIDs[output.ID] = true
	}

	// Validate step input sources exist
	for _, step := range wa.doc.Steps {
		for _, in := range step.In {
			sources := wa.getSources(in.Source)
			for _, source := range sources {
				if !wa.sourceExists(source, stepIDs, inputIDs) {
					errors = append(errors, fmt.Errorf("step %s input %s references non-existent source: %s", step.ID, in.ID, source))
				}
			}
		}
	}

	// Validate workflow output sources exist
	for _, out := range wa.doc.Outputs {
		sources := wa.getSources(out.OutputSource)
		for _, source := range sources {
			if !wa.sourceExists(source, stepIDs, inputIDs) {
				errors = append(errors, fmt.Errorf("workflow output %s references non-existent source: %s", out.ID, source))
			}
		}
	}

	// Check for cycles
	deps, err := wa.GetStepDependencies()
	if err != nil {
		errors = append(errors, err)
	} else {
		if cycle := wa.detectCycle(deps); cycle != "" {
			errors = append(errors, fmt.Errorf("cycle detected in workflow: %s", cycle))
		}
	}

	return errors
}

// sourceExists checks if a source reference is valid.
func (wa *WorkflowAnalyzer) sourceExists(source string, stepIDs, inputIDs map[string]bool) bool {
	source = strings.TrimPrefix(source, "#")

	parts := strings.SplitN(source, "/", 2)
	if len(parts) == 1 {
		// Workflow input
		return inputIDs[parts[0]]
	}

	// Step output
	return stepIDs[parts[0]]
}

// detectCycle detects cycles in the workflow DAG using DFS.
func (wa *WorkflowAnalyzer) detectCycle(deps []StepDependency) string {
	depMap := make(map[string][]string)
	for _, d := range deps {
		depMap[d.StepID] = d.DependsOn
	}

	visited := make(map[string]int) // 0: not visited, 1: visiting, 2: visited
	var path []string

	var dfs func(node string) bool
	dfs = func(node string) bool {
		visited[node] = 1
		path = append(path, node)

		for _, dep := range depMap[node] {
			if visited[dep] == 1 {
				// Found cycle
				path = append(path, dep)
				return true
			}
			if visited[dep] == 0 {
				if dfs(dep) {
					return true
				}
			}
		}

		path = path[:len(path)-1]
		visited[node] = 2
		return false
	}

	for _, d := range deps {
		if visited[d.StepID] == 0 {
			if dfs(d.StepID) {
				return strings.Join(path, " -> ")
			}
		}
	}

	return ""
}

// GetScatteredSteps returns steps that use scatter.
func (wa *WorkflowAnalyzer) GetScatteredSteps() []*WorkflowStep {
	var scattered []*WorkflowStep
	for i := range wa.doc.Steps {
		if wa.doc.Steps[i].Scatter != nil {
			scattered = append(scattered, &wa.doc.Steps[i])
		}
	}
	return scattered
}

// GetConditionalSteps returns steps that have a "when" condition.
func (wa *WorkflowAnalyzer) GetConditionalSteps() []*WorkflowStep {
	var conditional []*WorkflowStep
	for i := range wa.doc.Steps {
		if wa.doc.Steps[i].When != "" {
			conditional = append(conditional, &wa.doc.Steps[i])
		}
	}
	return conditional
}
