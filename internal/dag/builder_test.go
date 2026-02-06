package dag

import (
	"testing"

	"github.com/BV-BRC/cwe-cwl/internal/cwl"
)

func TestBuilder_Build_SimpleWorkflow(t *testing.T) {
	// Create a test workflow with inline tools to avoid file resolution issues
	doc := &cwl.Document{
		CWLVersion: "v1.2",
		Class:      cwl.ClassWorkflow,
		ID:         "test-workflow",
		Inputs: []cwl.Input{
			{ID: "sequence_file", Type: "File"},
			{ID: "use_msa_server", Type: "boolean"},
		},
		Outputs: []cwl.Output{
			{ID: "structure", Type: "File", OutputSource: "structure_prediction/structure_file"},
			{ID: "proline_result", Type: "File", OutputSource: "proline_analysis/annotated_structure"},
			{ID: "disulfide_result", Type: "File", OutputSource: "disulfide_analysis/annotated_structure"},
		},
		Steps: []cwl.WorkflowStep{
			{
				ID: "structure_prediction",
				Run: map[string]interface{}{
					"cwlVersion":  "v1.2",
					"class":       "CommandLineTool",
					"baseCommand": []interface{}{"boltz", "predict"},
					"inputs":      []interface{}{map[string]interface{}{"id": "input_file", "type": "File"}},
					"outputs":     []interface{}{map[string]interface{}{"id": "structure_file", "type": "File"}},
				},
				In:  []cwl.WorkflowStepInput{{ID: "input_file", Source: "sequence_file"}},
				Out: []interface{}{"structure_file"},
			},
			{
				ID: "proline_analysis",
				Run: map[string]interface{}{
					"cwlVersion":  "v1.2",
					"class":       "CommandLineTool",
					"baseCommand": "prolinnator",
					"inputs":      []interface{}{map[string]interface{}{"id": "input_file", "type": "File"}},
					"outputs":     []interface{}{map[string]interface{}{"id": "annotated_structure", "type": "File"}},
				},
				In:  []cwl.WorkflowStepInput{{ID: "input_file", Source: "structure_prediction/structure_file"}},
				Out: []interface{}{"annotated_structure"},
			},
			{
				ID: "disulfide_analysis",
				Run: map[string]interface{}{
					"cwlVersion":  "v1.2",
					"class":       "CommandLineTool",
					"baseCommand": "disulfinnate",
					"inputs":      []interface{}{map[string]interface{}{"id": "input_file", "type": "File"}},
					"outputs":     []interface{}{map[string]interface{}{"id": "annotated_structure", "type": "File"}},
				},
				In:  []cwl.WorkflowStepInput{{ID: "input_file", Source: "structure_prediction/structure_file"}},
				Out: []interface{}{"annotated_structure"},
			},
		},
	}

	// Create workflow inputs
	inputs := map[string]interface{}{
		"sequence_file": map[string]interface{}{
			"class": "File",
			"path":  "/path/to/sequence.yaml",
		},
		"use_msa_server": true,
	}

	builder := NewBuilder(doc, inputs)
	dag, err := builder.Build("test-run-1")
	if err != nil {
		t.Fatalf("Failed to build DAG: %v", err)
	}

	// Verify DAG structure
	if dag.ID != "test-run-1" {
		t.Errorf("Expected DAG ID 'test-run-1', got '%s'", dag.ID)
	}

	// Should have 3 nodes (3 steps, no scatter)
	if len(dag.Nodes) != 3 {
		t.Errorf("Expected 3 nodes, got %d", len(dag.Nodes))
	}

	// Check node IDs
	expectedNodes := []string{"structure_prediction", "proline_analysis", "disulfide_analysis"}
	for _, expected := range expectedNodes {
		if dag.GetNode(expected) == nil {
			t.Errorf("Expected node '%s' not found", expected)
		}
	}

	// Verify dependencies
	structPred := dag.GetNode("structure_prediction")
	prolineAnalysis := dag.GetNode("proline_analysis")
	disulfideAnalysis := dag.GetNode("disulfide_analysis")

	if len(structPred.Dependencies) != 0 {
		t.Errorf("Expected structure_prediction to have no dependencies, got %v", structPred.Dependencies)
	}

	// proline_analysis should depend on structure_prediction
	hasDep := false
	for _, dep := range prolineAnalysis.Dependencies {
		if dep == "structure_prediction" {
			hasDep = true
			break
		}
	}
	if !hasDep {
		t.Errorf("Expected proline_analysis to depend on structure_prediction, deps: %v", prolineAnalysis.Dependencies)
	}

	// disulfide_analysis should depend on structure_prediction
	hasDep = false
	for _, dep := range disulfideAnalysis.Dependencies {
		if dep == "structure_prediction" {
			hasDep = true
			break
		}
	}
	if !hasDep {
		t.Errorf("Expected disulfide_analysis to depend on structure_prediction, deps: %v", disulfideAnalysis.Dependencies)
	}
}

func TestBuilder_Build_InitializesReadyNodes(t *testing.T) {
	// Create a test workflow with inline tools
	doc := &cwl.Document{
		CWLVersion: "v1.2",
		Class:      cwl.ClassWorkflow,
		ID:         "test-workflow",
		Inputs: []cwl.Input{
			{ID: "sequence_file", Type: "File"},
		},
		Outputs: []cwl.Output{
			{ID: "result", Type: "File", OutputSource: "step2/out"},
		},
		Steps: []cwl.WorkflowStep{
			{
				ID: "structure_prediction",
				Run: map[string]interface{}{
					"cwlVersion":  "v1.2",
					"class":       "CommandLineTool",
					"baseCommand": "predict",
					"inputs":      []interface{}{map[string]interface{}{"id": "in", "type": "File"}},
					"outputs":     []interface{}{map[string]interface{}{"id": "out", "type": "File"}},
				},
				In:  []cwl.WorkflowStepInput{{ID: "in", Source: "sequence_file"}},
				Out: []interface{}{"out"},
			},
			{
				ID: "proline_analysis",
				Run: map[string]interface{}{
					"cwlVersion":  "v1.2",
					"class":       "CommandLineTool",
					"baseCommand": "analyze",
					"inputs":      []interface{}{map[string]interface{}{"id": "in", "type": "File"}},
					"outputs":     []interface{}{map[string]interface{}{"id": "out", "type": "File"}},
				},
				In:  []cwl.WorkflowStepInput{{ID: "in", Source: "structure_prediction/out"}},
				Out: []interface{}{"out"},
			},
		},
	}

	inputs := map[string]interface{}{
		"sequence_file": map[string]interface{}{
			"class": "File",
			"path":  "/path/to/sequence.yaml",
		},
	}

	builder := NewBuilder(doc, inputs)
	dag, err := builder.Build("test-run-2")
	if err != nil {
		t.Fatalf("Failed to build DAG: %v", err)
	}

	// structure_prediction should be ready (no dependencies)
	structPred := dag.GetNode("structure_prediction")
	if structPred.GetStatus() != StatusReady {
		t.Errorf("Expected structure_prediction to be ready, got %s", structPred.GetStatus())
	}

	// proline_analysis should be pending (depends on structure_prediction)
	prolineAnalysis := dag.GetNode("proline_analysis")
	if prolineAnalysis.GetStatus() != StatusPending {
		t.Errorf("Expected proline_analysis to be pending, got %s", prolineAnalysis.GetStatus())
	}
}

func TestBuilder_Build_InlineWorkflow(t *testing.T) {
	// Test with inline tool definition
	doc := &cwl.Document{
		CWLVersion: "v1.2",
		Class:      cwl.ClassWorkflow,
		Inputs: []cwl.Input{
			{ID: "message", Type: "string"},
		},
		Outputs: []cwl.Output{
			{ID: "output", Type: "File", OutputSource: "echo_step/output"},
		},
		Steps: []cwl.WorkflowStep{
			{
				ID: "echo_step",
				Run: map[string]interface{}{
					"cwlVersion":  "v1.2",
					"class":       "CommandLineTool",
					"baseCommand": "echo",
					"inputs": []interface{}{
						map[string]interface{}{"id": "msg", "type": "string", "inputBinding": map[string]interface{}{"position": 1}},
					},
					"outputs": []interface{}{
						map[string]interface{}{"id": "output", "type": "stdout"},
					},
				},
				In:  []cwl.WorkflowStepInput{{ID: "msg", Source: "message"}},
				Out: []interface{}{"output"},
			},
		},
	}

	inputs := map[string]interface{}{
		"message": "Hello, World!",
	}

	builder := NewBuilder(doc, inputs)
	dag, err := builder.Build("inline-test")
	if err != nil {
		t.Fatalf("Failed to build DAG: %v", err)
	}

	if len(dag.Nodes) != 1 {
		t.Errorf("Expected 1 node, got %d", len(dag.Nodes))
	}

	node := dag.GetNode("echo_step")
	if node == nil {
		t.Fatal("Expected echo_step node")
	}

	// Verify tool was resolved
	if node.Tool == nil {
		t.Error("Expected tool to be resolved")
	} else if node.Tool.Class != cwl.ClassCommandLineTool {
		t.Errorf("Expected tool class CommandLineTool, got %s", node.Tool.Class)
	}

	// Verify inputs were resolved
	if node.Inputs["msg"] != "Hello, World!" {
		t.Errorf("Expected input msg='Hello, World!', got %v", node.Inputs["msg"])
	}
}

func TestResolveStepOutputs(t *testing.T) {
	dag := NewDAG("test", "wf")

	node := &Node{
		ID:     "step1",
		StepID: "step1",
		Status: StatusCompleted,
		Outputs: map[string]interface{}{
			"file":   "/path/to/output.txt",
			"number": 42,
		},
	}
	dag.AddNode(node)

	// Test resolving existing output
	output, err := ResolveStepOutputs(dag, "step1", "file")
	if err != nil {
		t.Fatalf("Failed to resolve output: %v", err)
	}
	if output != "/path/to/output.txt" {
		t.Errorf("Expected '/path/to/output.txt', got %v", output)
	}

	// Test resolving non-existent step
	_, err = ResolveStepOutputs(dag, "nonexistent", "file")
	if err == nil {
		t.Error("Expected error for non-existent step")
	}
}

func TestResolveStepOutputs_Scattered(t *testing.T) {
	dag := NewDAG("test", "wf")

	// Simulate scattered step with 3 instances
	for i := 0; i < 3; i++ {
		node := &Node{
			ID:           GenerateNodeID("step1", []int{i}),
			StepID:       "step1",
			ScatterIndex: []int{i},
			Status:       StatusCompleted,
			Outputs: map[string]interface{}{
				"result": i * 10,
			},
		}
		dag.AddNode(node)
	}

	// Should gather outputs into array
	output, err := ResolveStepOutputs(dag, "step1", "result")
	if err != nil {
		t.Fatalf("Failed to resolve scattered outputs: %v", err)
	}

	arr, ok := output.([]interface{})
	if !ok {
		t.Fatalf("Expected array output, got %T", output)
	}
	if len(arr) != 3 {
		t.Errorf("Expected 3 gathered outputs, got %d", len(arr))
	}
}

func TestPrepareNodeInputs(t *testing.T) {
	dag := NewDAG("test", "wf")

	// Add completed dependency node
	dep := &Node{
		ID:     "dep",
		StepID: "dep",
		Status: StatusCompleted,
		Outputs: map[string]interface{}{
			"output_file": map[string]interface{}{
				"class": "File",
				"path":  "/path/to/output.txt",
			},
		},
	}
	dag.AddNode(dep)

	// Add node that depends on dep
	node := &Node{
		ID:           "consumer",
		StepID:       "consumer",
		Status:       StatusReady,
		Dependencies: []string{"dep"},
		Step: &cwl.WorkflowStep{
			ID: "consumer",
			In: []cwl.WorkflowStepInput{
				{ID: "input_file", Source: "dep/output_file"},
				{ID: "static", Default: "default_value"},
			},
		},
	}
	dag.AddNode(node)

	workflowInputs := map[string]interface{}{}

	inputs, err := PrepareNodeInputs(dag, node, workflowInputs)
	if err != nil {
		t.Fatalf("Failed to prepare inputs: %v", err)
	}

	// Check that dep output was resolved
	if inputs["input_file"] == nil {
		t.Error("Expected input_file to be resolved")
	}

	fileMap, ok := inputs["input_file"].(map[string]interface{})
	if !ok {
		t.Errorf("Expected input_file to be a map, got %T", inputs["input_file"])
	} else if fileMap["path"] != "/path/to/output.txt" {
		t.Errorf("Expected path '/path/to/output.txt', got %v", fileMap["path"])
	}

	// Check default value
	if inputs["static"] != "default_value" {
		t.Errorf("Expected static='default_value', got %v", inputs["static"])
	}
}

func TestPrepareNodeInputs_WorkflowInputs(t *testing.T) {
	dag := NewDAG("test", "wf")

	node := &Node{
		ID:     "step1",
		StepID: "step1",
		Status: StatusReady,
		Step: &cwl.WorkflowStep{
			ID: "step1",
			In: []cwl.WorkflowStepInput{
				{ID: "seq_file", Source: "sequence_file"},
			},
		},
	}
	dag.AddNode(node)

	workflowInputs := map[string]interface{}{
		"sequence_file": map[string]interface{}{
			"class": "File",
			"path":  "/path/to/input.fasta",
		},
	}

	inputs, err := PrepareNodeInputs(dag, node, workflowInputs)
	if err != nil {
		t.Fatalf("Failed to prepare inputs: %v", err)
	}

	if inputs["seq_file"] == nil {
		t.Error("Expected seq_file to be resolved from workflow inputs")
	}

	fileMap, ok := inputs["seq_file"].(map[string]interface{})
	if !ok {
		t.Errorf("Expected seq_file to be a map, got %T", inputs["seq_file"])
	} else if fileMap["path"] != "/path/to/input.fasta" {
		t.Errorf("Expected path '/path/to/input.fasta', got %v", fileMap["path"])
	}
}
