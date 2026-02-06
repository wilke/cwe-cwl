package cwl

import (
	"os"
	"path/filepath"
	"testing"
)

// Test fixtures path - reference real CWL files from ProteinEngineeringWorkflows
const testDataPath = "/Users/me/Development/dxkb/ProteinEngineeringWorkflows/cwl"

func TestParser_ParseCommandLineTool(t *testing.T) {
	parser := NewParser()

	// Test parsing boltz.cwl - a real CommandLineTool
	toolPath := filepath.Join(testDataPath, "tools/boltz.cwl")
	if _, err := os.Stat(toolPath); os.IsNotExist(err) {
		t.Skip("Test data not available:", toolPath)
	}

	doc, err := parser.ParseFile(toolPath)
	if err != nil {
		t.Fatalf("Failed to parse boltz.cwl: %v", err)
	}

	// Verify basic properties
	if doc.CWLVersion != "v1.2" {
		t.Errorf("Expected cwlVersion v1.2, got %s", doc.CWLVersion)
	}

	if doc.Class != ClassCommandLineTool {
		t.Errorf("Expected class CommandLineTool, got %s", doc.Class)
	}

	if doc.Label != "Boltz-2 Structure Prediction" {
		t.Errorf("Expected label 'Boltz-2 Structure Prediction', got %s", doc.Label)
	}

	// Verify baseCommand
	baseCmd, ok := doc.BaseCommand.([]interface{})
	if !ok {
		t.Errorf("Expected baseCommand to be array, got %T", doc.BaseCommand)
	} else if len(baseCmd) != 2 || baseCmd[0] != "boltz" || baseCmd[1] != "predict" {
		t.Errorf("Expected baseCommand [boltz, predict], got %v", baseCmd)
	}

	// Verify inputs
	if len(doc.Inputs) < 7 {
		t.Errorf("Expected at least 7 inputs, got %d", len(doc.Inputs))
	}

	// Check specific input
	var inputFile *Input
	for i := range doc.Inputs {
		if doc.Inputs[i].ID == "input_file" {
			inputFile = &doc.Inputs[i]
			break
		}
	}
	if inputFile == nil {
		t.Error("Expected input_file input not found")
	} else {
		if inputFile.InputBinding == nil {
			t.Error("Expected inputBinding on input_file")
		} else if inputFile.InputBinding.Position != 1 {
			t.Errorf("Expected position 1, got %d", inputFile.InputBinding.Position)
		}
	}

	// Verify outputs
	if len(doc.Outputs) != 3 {
		t.Errorf("Expected 3 outputs, got %d", len(doc.Outputs))
	}

	// Check output with glob
	var structureFile *Output
	for i := range doc.Outputs {
		if doc.Outputs[i].ID == "structure_file" {
			structureFile = &doc.Outputs[i]
			break
		}
	}
	if structureFile == nil {
		t.Error("Expected structure_file output not found")
	} else if structureFile.OutputBinding == nil {
		t.Error("Expected outputBinding on structure_file")
	}

	// Verify requirements
	dockerReq := doc.GetDockerRequirement()
	if dockerReq == nil {
		t.Error("Expected DockerRequirement")
	} else if dockerReq.DockerPull != "dxkb/boltz-bvbrc:latest-gpu" {
		t.Errorf("Expected docker image dxkb/boltz-bvbrc:latest-gpu, got %s", dockerReq.DockerPull)
	}

	resourceReq := doc.GetResourceRequirement()
	if resourceReq == nil {
		t.Error("Expected ResourceRequirement")
	}
}

func TestParser_ParseWorkflow(t *testing.T) {
	parser := NewParser()

	// Test parsing protein_stability_explicit.cwl - a real Workflow
	workflowPath := filepath.Join(testDataPath, "workflows/protein_stability_explicit.cwl")
	if _, err := os.Stat(workflowPath); os.IsNotExist(err) {
		t.Skip("Test data not available:", workflowPath)
	}

	doc, err := parser.ParseFile(workflowPath)
	if err != nil {
		t.Fatalf("Failed to parse protein_stability_explicit.cwl: %v", err)
	}

	// Verify basic properties
	if doc.CWLVersion != "v1.2" {
		t.Errorf("Expected cwlVersion v1.2, got %s", doc.CWLVersion)
	}

	if doc.Class != ClassWorkflow {
		t.Errorf("Expected class Workflow, got %s", doc.Class)
	}

	// Verify steps
	if len(doc.Steps) != 3 {
		t.Errorf("Expected 3 steps, got %d", len(doc.Steps))
	}

	// Check step IDs
	stepIDs := make(map[string]bool)
	for _, step := range doc.Steps {
		stepIDs[step.ID] = true
	}
	expectedSteps := []string{"structure_prediction", "proline_analysis", "disulfide_analysis"}
	for _, expected := range expectedSteps {
		if !stepIDs[expected] {
			t.Errorf("Expected step %s not found", expected)
		}
	}

	// Verify workflow inputs
	if len(doc.Inputs) < 5 {
		t.Errorf("Expected at least 5 workflow inputs, got %d", len(doc.Inputs))
	}

	// Verify workflow outputs
	if len(doc.Outputs) != 9 {
		t.Errorf("Expected 9 workflow outputs, got %d", len(doc.Outputs))
	}

	// Check output source wiring
	var predictedStructure *Output
	for i := range doc.Outputs {
		if doc.Outputs[i].ID == "predicted_structure" {
			predictedStructure = &doc.Outputs[i]
			break
		}
	}
	if predictedStructure == nil {
		t.Error("Expected predicted_structure output not found")
	} else {
		source, ok := predictedStructure.OutputSource.(string)
		if !ok || source != "structure_prediction/structure_file" {
			t.Errorf("Expected outputSource 'structure_prediction/structure_file', got %v", predictedStructure.OutputSource)
		}
	}
}

func TestParser_ParseBytes(t *testing.T) {
	parser := NewParser()

	cwlDoc := `
cwlVersion: v1.2
class: CommandLineTool
baseCommand: echo
inputs:
  message:
    type: string
    inputBinding:
      position: 1
outputs:
  output:
    type: stdout
`

	doc, err := parser.ParseBytes([]byte(cwlDoc))
	if err != nil {
		t.Fatalf("Failed to parse CWL bytes: %v", err)
	}

	if doc.Class != ClassCommandLineTool {
		t.Errorf("Expected class CommandLineTool, got %s", doc.Class)
	}

	if doc.BaseCommand != "echo" {
		t.Errorf("Expected baseCommand 'echo', got %v", doc.BaseCommand)
	}

	if len(doc.Inputs) != 1 {
		t.Errorf("Expected 1 input, got %d", len(doc.Inputs))
	}
}

func TestParser_ParseWorkflowWithMapInputs(t *testing.T) {
	parser := NewParser()

	// CWL with map-style inputs (common alternative format)
	cwlDoc := `
cwlVersion: v1.2
class: Workflow
inputs:
  input1: File
  input2:
    type: string
    default: "hello"
outputs:
  output1:
    type: File
    outputSource: step1/out
steps:
  step1:
    run: tool.cwl
    in:
      in1: input1
    out: [out]
`

	doc, err := parser.ParseBytes([]byte(cwlDoc))
	if err != nil {
		t.Fatalf("Failed to parse CWL: %v", err)
	}

	if len(doc.Inputs) != 2 {
		t.Errorf("Expected 2 inputs, got %d", len(doc.Inputs))
	}

	// Check that map keys became IDs
	inputIDs := make(map[string]bool)
	for _, input := range doc.Inputs {
		inputIDs[input.ID] = true
	}
	if !inputIDs["input1"] || !inputIDs["input2"] {
		t.Errorf("Expected input IDs input1 and input2, got %v", inputIDs)
	}
}

func TestParser_InvalidCWL(t *testing.T) {
	parser := NewParser()

	testCases := []struct {
		name string
		cwl  string
	}{
		{
			name: "missing cwlVersion",
			cwl: `
class: CommandLineTool
baseCommand: echo
inputs: []
outputs: []
`,
		},
		{
			name: "missing class",
			cwl: `
cwlVersion: v1.2
baseCommand: echo
inputs: []
outputs: []
`,
		},
		{
			name: "unsupported version",
			cwl: `
cwlVersion: draft-3
class: CommandLineTool
baseCommand: echo
inputs: []
outputs: []
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parser.ParseBytes([]byte(tc.cwl))
			if err == nil {
				t.Errorf("Expected error for %s, got none", tc.name)
			}
		})
	}
}

func TestContentHash(t *testing.T) {
	data := []byte("test content")
	hash := ContentHash(data)

	if len(hash) < 10 {
		t.Error("Hash too short")
	}

	if hash[:7] != "sha256:" {
		t.Errorf("Expected sha256: prefix, got %s", hash[:7])
	}

	// Same content should produce same hash
	hash2 := ContentHash(data)
	if hash != hash2 {
		t.Error("Same content produced different hashes")
	}

	// Different content should produce different hash
	hash3 := ContentHash([]byte("different content"))
	if hash == hash3 {
		t.Error("Different content produced same hash")
	}
}
