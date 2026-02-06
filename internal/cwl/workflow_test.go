package cwl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkflowAnalyzer_GetStepDependencies(t *testing.T) {
	parser := NewParser()

	// Test with real workflow
	workflowPath := filepath.Join(testDataPath, "workflows/protein_stability_explicit.cwl")
	if _, err := os.Stat(workflowPath); os.IsNotExist(err) {
		t.Skip("Test data not available:", workflowPath)
	}

	doc, err := parser.ParseFile(workflowPath)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	analyzer := NewWorkflowAnalyzer(doc)
	deps, err := analyzer.GetStepDependencies()
	if err != nil {
		t.Fatalf("Failed to get dependencies: %v", err)
	}

	if len(deps) != 3 {
		t.Errorf("Expected 3 step dependencies, got %d", len(deps))
	}

	// Build dependency map for easier testing
	depMap := make(map[string][]string)
	for _, d := range deps {
		depMap[d.StepID] = d.DependsOn
	}

	// structure_prediction has no dependencies (uses workflow inputs)
	if len(depMap["structure_prediction"]) != 0 {
		t.Errorf("Expected structure_prediction to have no dependencies, got %v", depMap["structure_prediction"])
	}

	// proline_analysis depends on structure_prediction
	if len(depMap["proline_analysis"]) != 1 || depMap["proline_analysis"][0] != "structure_prediction" {
		t.Errorf("Expected proline_analysis to depend on structure_prediction, got %v", depMap["proline_analysis"])
	}

	// disulfide_analysis depends on structure_prediction
	if len(depMap["disulfide_analysis"]) != 1 || depMap["disulfide_analysis"][0] != "structure_prediction" {
		t.Errorf("Expected disulfide_analysis to depend on structure_prediction, got %v", depMap["disulfide_analysis"])
	}
}

func TestWorkflowAnalyzer_ValidateWorkflow(t *testing.T) {
	parser := NewParser()

	// Test with valid workflow
	workflowPath := filepath.Join(testDataPath, "workflows/protein_stability_explicit.cwl")
	if _, err := os.Stat(workflowPath); os.IsNotExist(err) {
		t.Skip("Test data not available:", workflowPath)
	}

	doc, err := parser.ParseFile(workflowPath)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	analyzer := NewWorkflowAnalyzer(doc)
	errs := analyzer.ValidateWorkflow()

	if len(errs) != 0 {
		t.Errorf("Expected valid workflow, got errors: %v", errs)
	}
}

func TestWorkflowAnalyzer_ValidateWorkflow_Invalid(t *testing.T) {
	// Test with invalid workflow (duplicate step IDs)
	doc := &Document{
		CWLVersion: "v1.2",
		Class:      ClassWorkflow,
		Inputs:     []Input{{ID: "input1", Type: "File"}},
		Outputs:    []Output{{ID: "out1", Type: "File", OutputSource: "step1/out"}},
		Steps: []WorkflowStep{
			{ID: "step1", Run: "tool.cwl", In: []WorkflowStepInput{{ID: "in1", Source: "input1"}}, Out: []interface{}{"out"}},
			{ID: "step1", Run: "tool2.cwl", In: []WorkflowStepInput{{ID: "in1", Source: "input1"}}, Out: []interface{}{"out"}}, // duplicate
		},
	}

	analyzer := NewWorkflowAnalyzer(doc)
	errs := analyzer.ValidateWorkflow()

	if len(errs) == 0 {
		t.Error("Expected validation errors for duplicate step IDs")
	}

	hasDuplicateError := false
	for _, err := range errs {
		if err.Error() == "duplicate step ID: step1" {
			hasDuplicateError = true
			break
		}
	}
	if !hasDuplicateError {
		t.Errorf("Expected 'duplicate step ID' error, got: %v", errs)
	}
}

func TestWorkflowAnalyzer_DetectCycle(t *testing.T) {
	// Create workflow with a cycle
	doc := &Document{
		CWLVersion: "v1.2",
		Class:      ClassWorkflow,
		Inputs:     []Input{{ID: "input1", Type: "File"}},
		Outputs:    []Output{{ID: "out1", Type: "File", OutputSource: "step1/out"}},
		Steps: []WorkflowStep{
			{ID: "step1", Run: "tool.cwl", In: []WorkflowStepInput{{ID: "in1", Source: "step2/out"}}, Out: []interface{}{"out"}},
			{ID: "step2", Run: "tool.cwl", In: []WorkflowStepInput{{ID: "in1", Source: "step1/out"}}, Out: []interface{}{"out"}},
		},
	}

	analyzer := NewWorkflowAnalyzer(doc)
	errs := analyzer.ValidateWorkflow()

	hasCycleError := false
	for _, err := range errs {
		if len(err.Error()) > 10 && err.Error()[:5] == "cycle" {
			hasCycleError = true
			break
		}
	}
	if !hasCycleError {
		t.Errorf("Expected cycle detection error, got: %v", errs)
	}
}

func TestWorkflowAnalyzer_GetStep(t *testing.T) {
	parser := NewParser()

	workflowPath := filepath.Join(testDataPath, "workflows/protein_stability_explicit.cwl")
	if _, err := os.Stat(workflowPath); os.IsNotExist(err) {
		t.Skip("Test data not available:", workflowPath)
	}

	doc, err := parser.ParseFile(workflowPath)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	analyzer := NewWorkflowAnalyzer(doc)

	step := analyzer.GetStep("structure_prediction")
	if step == nil {
		t.Error("Expected to find structure_prediction step")
	} else if step.ID != "structure_prediction" {
		t.Errorf("Expected step ID structure_prediction, got %s", step.ID)
	}

	step = analyzer.GetStep("nonexistent")
	if step != nil {
		t.Error("Expected nil for nonexistent step")
	}
}

func TestWorkflowAnalyzer_GetStepOutputIDs(t *testing.T) {
	parser := NewParser()

	workflowPath := filepath.Join(testDataPath, "workflows/protein_stability_explicit.cwl")
	if _, err := os.Stat(workflowPath); os.IsNotExist(err) {
		t.Skip("Test data not available:", workflowPath)
	}

	doc, err := parser.ParseFile(workflowPath)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	analyzer := NewWorkflowAnalyzer(doc)
	outputs := analyzer.GetStepOutputIDs("structure_prediction")

	expectedOutputs := []string{"structure_file", "all_outputs", "confidence_scores"}
	if len(outputs) != len(expectedOutputs) {
		t.Errorf("Expected %d outputs, got %d", len(expectedOutputs), len(outputs))
	}

	outputSet := make(map[string]bool)
	for _, out := range outputs {
		outputSet[out] = true
	}
	for _, expected := range expectedOutputs {
		if !outputSet[expected] {
			t.Errorf("Expected output %s not found", expected)
		}
	}
}

func TestWorkflowAnalyzer_CollectOutputSources(t *testing.T) {
	parser := NewParser()

	workflowPath := filepath.Join(testDataPath, "workflows/protein_stability_explicit.cwl")
	if _, err := os.Stat(workflowPath); os.IsNotExist(err) {
		t.Skip("Test data not available:", workflowPath)
	}

	doc, err := parser.ParseFile(workflowPath)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	analyzer := NewWorkflowAnalyzer(doc)
	sources := analyzer.CollectOutputSources()

	if len(sources) != 9 {
		t.Errorf("Expected 9 output sources, got %d", len(sources))
	}

	// Check specific mappings
	if sources["predicted_structure"] != "structure_prediction/structure_file" {
		t.Errorf("Expected predicted_structure -> structure_prediction/structure_file, got %s", sources["predicted_structure"])
	}

	if sources["proline_annotated_structure"] != "proline_analysis/annotated_structure" {
		t.Errorf("Expected proline_annotated_structure -> proline_analysis/annotated_structure, got %s", sources["proline_annotated_structure"])
	}
}

func TestWorkflowAnalyzer_GetScatteredSteps(t *testing.T) {
	// Create workflow with scatter
	doc := &Document{
		CWLVersion: "v1.2",
		Class:      ClassWorkflow,
		Inputs:     []Input{{ID: "files", Type: map[string]interface{}{"type": "array", "items": "File"}}},
		Outputs:    []Output{{ID: "out", Type: "File", OutputSource: "step1/out"}},
		Steps: []WorkflowStep{
			{
				ID:      "step1",
				Run:     "tool.cwl",
				Scatter: "file",
				In:      []WorkflowStepInput{{ID: "file", Source: "files"}},
				Out:     []interface{}{"out"},
			},
			{
				ID:  "step2",
				Run: "tool.cwl",
				In:  []WorkflowStepInput{{ID: "in", Source: "step1/out"}},
				Out: []interface{}{"out"},
			},
		},
	}

	analyzer := NewWorkflowAnalyzer(doc)
	scattered := analyzer.GetScatteredSteps()

	if len(scattered) != 1 {
		t.Errorf("Expected 1 scattered step, got %d", len(scattered))
	}
	if scattered[0].ID != "step1" {
		t.Errorf("Expected scattered step step1, got %s", scattered[0].ID)
	}
}

func TestWorkflowAnalyzer_ResolveStepTool(t *testing.T) {
	// Create a workflow with inline tool
	doc := &Document{
		CWLVersion: "v1.2",
		Class:      ClassWorkflow,
		Inputs:     []Input{{ID: "input1", Type: "string"}},
		Outputs:    []Output{{ID: "out", Type: "File", OutputSource: "step1/out"}},
		Steps: []WorkflowStep{
			{
				ID: "step1",
				Run: map[string]interface{}{
					"cwlVersion":  "v1.2",
					"class":       "CommandLineTool",
					"baseCommand": "echo",
					"inputs": []interface{}{
						map[string]interface{}{"id": "msg", "type": "string"},
					},
					"outputs": []interface{}{
						map[string]interface{}{"id": "out", "type": "stdout"},
					},
				},
				In:  []WorkflowStepInput{{ID: "msg", Source: "input1"}},
				Out: []interface{}{"out"},
			},
		},
	}

	analyzer := NewWorkflowAnalyzer(doc)
	step := analyzer.GetStep("step1")

	tool, path, err := analyzer.ResolveStepTool(step)
	if err != nil {
		t.Fatalf("Failed to resolve step tool: %v", err)
	}
	if path != "" {
		t.Errorf("Expected empty path for inline tool, got %s", path)
	}
	if tool == nil {
		t.Error("Expected parsed tool document")
	} else if tool.Class != ClassCommandLineTool {
		t.Errorf("Expected class CommandLineTool, got %s", tool.Class)
	}
}

func TestWorkflowAnalyzer_ResolveStepTool_FilePath(t *testing.T) {
	parser := NewParser()

	workflowPath := filepath.Join(testDataPath, "workflows/protein_stability_explicit.cwl")
	if _, err := os.Stat(workflowPath); os.IsNotExist(err) {
		t.Skip("Test data not available:", workflowPath)
	}

	doc, err := parser.ParseFile(workflowPath)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	analyzer := NewWorkflowAnalyzer(doc)
	step := analyzer.GetStep("structure_prediction")

	tool, path, err := analyzer.ResolveStepTool(step)
	if err != nil {
		t.Fatalf("Failed to resolve step tool: %v", err)
	}
	if tool != nil {
		t.Error("Expected nil tool for file reference")
	}
	if path != "../tools/boltz.cwl" {
		t.Errorf("Expected path '../tools/boltz.cwl', got '%s'", path)
	}
}
