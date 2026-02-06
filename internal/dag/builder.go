package dag

import (
	"fmt"
	"strings"

	"github.com/BV-BRC/cwe-cwl/internal/cwl"
)

// Builder constructs DAGs from CWL workflow documents.
type Builder struct {
	workflow      *cwl.Document
	workflowInputs map[string]interface{}
	parser        *cwl.Parser
}

// NewBuilder creates a new DAG builder.
func NewBuilder(workflow *cwl.Document, inputs map[string]interface{}) *Builder {
	return &Builder{
		workflow:      workflow,
		workflowInputs: inputs,
		parser:        cwl.NewParser(),
	}
}

// Build constructs a DAG from the workflow.
func (b *Builder) Build(dagID string) (*DAG, error) {
	if b.workflow.Class != cwl.ClassWorkflow {
		return nil, fmt.Errorf("document is not a Workflow")
	}

	dag := NewDAG(dagID, b.workflow.ID)

	// Analyze workflow dependencies
	analyzer := cwl.NewWorkflowAnalyzer(b.workflow)
	deps, err := analyzer.GetStepDependencies()
	if err != nil {
		return nil, fmt.Errorf("failed to analyze dependencies: %w", err)
	}

	// Build dependency map for quick lookup
	depMap := make(map[string]cwl.StepDependency)
	for _, d := range deps {
		depMap[d.StepID] = d
	}

	// Create nodes for each step
	for _, step := range b.workflow.Steps {
		// Check for scatter
		scatterConfig, err := cwl.ParseScatterConfig(&step)
		if err != nil {
			return nil, fmt.Errorf("failed to parse scatter config for step %s: %w", step.ID, err)
		}

		// Resolve step inputs
		stepInputs, err := b.resolveStepInputs(&step, nil) // nil for non-scattered initial resolution
		if err != nil {
			return nil, fmt.Errorf("failed to resolve inputs for step %s: %w", step.ID, err)
		}

		// Resolve the tool
		tool, toolPath, err := analyzer.ResolveStepTool(&step)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve tool for step %s: %w", step.ID, err)
		}

		// If tool is a file path, parse it
		if tool == nil && toolPath != "" {
			tool, err = b.parser.ParseFile(toolPath)
			if err != nil {
				return nil, fmt.Errorf("failed to parse tool %s for step %s: %w", toolPath, step.ID, err)
			}
		}

		if scatterConfig != nil {
			// Create expanded nodes for scatter
			nodes, err := b.createScatteredNodes(&step, scatterConfig, stepInputs, tool, depMap)
			if err != nil {
				return nil, fmt.Errorf("failed to create scattered nodes for step %s: %w", step.ID, err)
			}
			for _, node := range nodes {
				dag.AddNode(node)
			}
		} else {
			// Create single node
			node := b.createNode(&step, nil, stepInputs, tool, depMap)
			dag.AddNode(node)
		}
	}

	// Build dependency links between nodes
	if err := b.linkDependencies(dag); err != nil {
		return nil, fmt.Errorf("failed to link dependencies: %w", err)
	}

	// Initialize ready nodes
	dag.InitializeReadyNodes()

	return dag, nil
}

// createNode creates a single DAG node.
func (b *Builder) createNode(step *cwl.WorkflowStep, scatterIndex []int, inputs map[string]interface{}, tool *cwl.Document, depMap map[string]cwl.StepDependency) *Node {
	stepCopy := *step
	node := &Node{
		ID:           GenerateNodeID(step.ID, scatterIndex),
		StepID:       step.ID,
		ScatterIndex: scatterIndex,
		Status:       StatusPending,
		Inputs:       inputs,
		Step:         &stepCopy,
		Tool:         tool,
		Dependencies: []string{},
		Dependents:   []string{},
	}

	// Add dependencies based on step inputs
	if dep, ok := depMap[step.ID]; ok {
		for _, depStepID := range dep.DependsOn {
			// For now, just add the step ID - we'll resolve scatter indices later
			node.Dependencies = append(node.Dependencies, depStepID)
		}
	}

	return node
}

// createScatteredNodes creates nodes for a scattered step.
func (b *Builder) createScatteredNodes(step *cwl.WorkflowStep, config *cwl.ScatterConfig, inputs map[string]interface{}, tool *cwl.Document, depMap map[string]cwl.StepDependency) ([]*Node, error) {
	expander := cwl.NewScatterExpander(*config, inputs)
	expanded, err := expander.Expand()
	if err != nil {
		return nil, err
	}

	var nodes []*Node
	for _, si := range expanded {
		node := b.createNode(step, si.Index, si.Values, tool, depMap)
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// resolveStepInputs resolves input values for a step.
func (b *Builder) resolveStepInputs(step *cwl.WorkflowStep, scatterValues map[string]interface{}) (map[string]interface{}, error) {
	inputs := make(map[string]interface{})

	for _, in := range step.In {
		var value interface{}

		// Check scatter values first
		if scatterValues != nil {
			if sv, ok := scatterValues[in.ID]; ok {
				value = sv
				inputs[in.ID] = value
				continue
			}
		}

		// Try to resolve from source
		if in.Source != nil {
			resolved, err := b.resolveSource(in.Source)
			if err != nil {
				// Source might reference a step output that isn't available yet
				// Store the source reference for later resolution
				inputs[in.ID] = in.Source
				continue
			}
			value = resolved
		}

		// Use default if no source value
		if value == nil && in.Default != nil {
			value = in.Default
		}

		if value != nil {
			inputs[in.ID] = value
		}
	}

	return inputs, nil
}

// resolveSource resolves a source reference to its value.
func (b *Builder) resolveSource(source interface{}) (interface{}, error) {
	switch v := source.(type) {
	case string:
		return b.resolveSourceString(v)
	case []interface{}:
		var values []interface{}
		for _, item := range v {
			if s, ok := item.(string); ok {
				val, err := b.resolveSourceString(s)
				if err != nil {
					return nil, err
				}
				values = append(values, val)
			}
		}
		return values, nil
	default:
		return nil, fmt.Errorf("unsupported source type: %T", source)
	}
}

// resolveSourceString resolves a single source string.
func (b *Builder) resolveSourceString(source string) (interface{}, error) {
	source = strings.TrimPrefix(source, "#")
	parts := strings.SplitN(source, "/", 2)

	if len(parts) == 1 {
		// Workflow input
		if val, ok := b.workflowInputs[parts[0]]; ok {
			return val, nil
		}
		return nil, fmt.Errorf("workflow input not found: %s", parts[0])
	}

	// Step output - return as reference to be resolved during execution
	return nil, fmt.Errorf("step output reference: %s", source)
}

// linkDependencies builds the dependency graph between nodes.
func (b *Builder) linkDependencies(dag *DAG) error {
	// First pass: collect all nodes by step ID
	nodesByStep := make(map[string][]*Node)
	for _, node := range dag.Nodes {
		nodesByStep[node.StepID] = append(nodesByStep[node.StepID], node)
	}

	// Second pass: resolve dependencies
	for _, node := range dag.Nodes {
		var resolvedDeps []string

		for _, depStepID := range node.Dependencies {
			depNodes, ok := nodesByStep[depStepID]
			if !ok {
				return fmt.Errorf("dependency step not found: %s", depStepID)
			}

			// If node is scattered, it might depend on:
			// - All instances of the dependency (if dependency is scattered)
			// - Single instance of the dependency (if dependency is not scattered)
			for _, depNode := range depNodes {
				// Add dependency
				resolvedDeps = append(resolvedDeps, depNode.ID)
				// Also add this node as a dependent
				depNode.Dependents = append(depNode.Dependents, node.ID)
			}
		}

		node.Dependencies = resolvedDeps
	}

	return nil
}

// ResolveStepOutputs resolves step outputs from a completed node.
func ResolveStepOutputs(dag *DAG, stepID string, outputID string) (interface{}, error) {
	// Find all nodes for this step
	var nodes []*Node
	for _, node := range dag.Nodes {
		if node.StepID == stepID && node.GetStatus() == StatusCompleted {
			nodes = append(nodes, node)
		}
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("no completed nodes for step: %s", stepID)
	}

	// If single node, return the output directly
	if len(nodes) == 1 {
		if val, ok := nodes[0].Outputs[outputID]; ok {
			return val, nil
		}
		return nil, fmt.Errorf("output not found: %s/%s", stepID, outputID)
	}

	// Multiple nodes (scattered) - gather outputs into array
	var gathered []interface{}
	for _, node := range nodes {
		if val, ok := node.Outputs[outputID]; ok {
			gathered = append(gathered, val)
		} else {
			gathered = append(gathered, nil)
		}
	}

	return gathered, nil
}

// PrepareNodeInputs resolves input values for a node from completed dependencies.
func PrepareNodeInputs(dag *DAG, node *Node, workflowInputs map[string]interface{}) (map[string]interface{}, error) {
	inputs := make(map[string]interface{})

	for _, in := range node.Step.In {
		var value interface{}

		// First check if we already have a resolved value
		if v, ok := node.Inputs[in.ID]; ok {
			// Check if it's a source reference that needs resolution
			if src, isStr := v.(string); isStr && strings.Contains(src, "/") {
				resolved, err := resolveRuntimeSource(dag, src, workflowInputs)
				if err != nil {
					return nil, fmt.Errorf("failed to resolve source %s for input %s: %w", src, in.ID, err)
				}
				value = resolved
			} else if srcArr, isArr := v.([]interface{}); isArr {
				// Check if array contains source references
				var resolved []interface{}
				for _, item := range srcArr {
					if src, isStr := item.(string); isStr && strings.Contains(src, "/") {
						r, err := resolveRuntimeSource(dag, src, workflowInputs)
						if err != nil {
							return nil, err
						}
						resolved = append(resolved, r)
					} else {
						resolved = append(resolved, item)
					}
				}
				value = resolved
			} else {
				value = v
			}
		}

		// Try to resolve from source
		if value == nil && in.Source != nil {
			switch src := in.Source.(type) {
			case string:
				resolved, err := resolveRuntimeSource(dag, src, workflowInputs)
				if err != nil {
					return nil, err
				}
				value = resolved
			case []interface{}:
				var resolved []interface{}
				for _, item := range src {
					if s, ok := item.(string); ok {
						r, err := resolveRuntimeSource(dag, s, workflowInputs)
						if err != nil {
							return nil, err
						}
						resolved = append(resolved, r)
					}
				}
				value = resolved
			}
		}

		// Use default if still nil
		if value == nil && in.Default != nil {
			value = in.Default
		}

		if value != nil {
			inputs[in.ID] = value
		}
	}

	return inputs, nil
}

// resolveRuntimeSource resolves a source reference at runtime.
func resolveRuntimeSource(dag *DAG, source string, workflowInputs map[string]interface{}) (interface{}, error) {
	source = strings.TrimPrefix(source, "#")
	parts := strings.SplitN(source, "/", 2)

	if len(parts) == 1 {
		// Workflow input
		if val, ok := workflowInputs[parts[0]]; ok {
			return val, nil
		}
		return nil, fmt.Errorf("workflow input not found: %s", parts[0])
	}

	// Step output
	stepID := parts[0]
	outputID := parts[1]
	return ResolveStepOutputs(dag, stepID, outputID)
}
