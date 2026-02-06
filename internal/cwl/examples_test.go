package cwl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseExampleBWAMem(t *testing.T) {
	// Find examples directory relative to test
	examplesDir := findExamplesDir(t)
	cwlPath := filepath.Join(examplesDir, "tools", "bwa-mem.cwl")

	data, err := os.ReadFile(cwlPath)
	if err != nil {
		t.Fatalf("Failed to read bwa-mem.cwl: %v", err)
	}

	parser := NewParser()
	doc, err := parser.ParseBytes(data)
	if err != nil {
		t.Fatalf("Failed to parse bwa-mem.cwl: %v", err)
	}

	// Verify basic structure
	if doc.CWLVersion != "v1.2" {
		t.Errorf("Expected cwlVersion v1.2, got %s", doc.CWLVersion)
	}
	if doc.Class != "CommandLineTool" {
		t.Errorf("Expected class CommandLineTool, got %s", doc.Class)
	}

	// Verify baseCommand is simple executable (no paths)
	if doc.BaseCommand != "bwa" {
		t.Errorf("Expected baseCommand 'bwa', got %v", doc.BaseCommand)
	}

	// Verify Docker requirement exists
	dockerReq := doc.GetDockerRequirement()
	if dockerReq == nil {
		t.Error("Expected DockerRequirement to be present")
	} else if dockerReq.DockerPull != "biocontainers/bwa:v0.7.17_cv1" {
		t.Errorf("Expected dockerPull 'biocontainers/bwa:v0.7.17_cv1', got %s", dockerReq.DockerPull)
	}

	// Verify Apptainer hint exists
	apptainerReq := doc.GetApptainerRequirement()
	if apptainerReq == nil {
		t.Error("Expected ApptainerRequirement hint to be present")
	} else if apptainerReq.ApptainerPull != "docker://biocontainers/bwa:v0.7.17_cv1" {
		t.Errorf("Expected apptainerPull 'docker://biocontainers/bwa:v0.7.17_cv1', got %s", apptainerReq.ApptainerPull)
	}

	// Verify inputs
	if len(doc.Inputs) < 4 {
		t.Errorf("Expected at least 4 inputs, got %d", len(doc.Inputs))
	}

	// Check reference input has secondaryFiles
	var refInput *Input
	for i := range doc.Inputs {
		if doc.Inputs[i].ID == "reference" {
			refInput = &doc.Inputs[i]
			break
		}
	}
	if refInput == nil {
		t.Error("Expected 'reference' input")
	} else if len(refInput.SecondaryFiles) != 5 {
		t.Errorf("Expected 5 secondary files for reference, got %d", len(refInput.SecondaryFiles))
	}

	// Verify outputs
	if len(doc.Outputs) != 1 {
		t.Errorf("Expected 1 output, got %d", len(doc.Outputs))
	}

	// Verify container requirement validation passes
	if !doc.RequiresContainer() {
		t.Error("Expected RequiresContainer() to return true")
	}
}

func TestParseExampleAlphaFold(t *testing.T) {
	examplesDir := findExamplesDir(t)
	cwlPath := filepath.Join(examplesDir, "tools", "alphafold-predict.cwl")

	data, err := os.ReadFile(cwlPath)
	if err != nil {
		t.Fatalf("Failed to read alphafold-predict.cwl: %v", err)
	}

	parser := NewParser()
	doc, err := parser.ParseBytes(data)
	if err != nil {
		t.Fatalf("Failed to parse alphafold-predict.cwl: %v", err)
	}

	// Verify basic structure
	if doc.Class != "CommandLineTool" {
		t.Errorf("Expected class CommandLineTool, got %s", doc.Class)
	}
	if doc.BaseCommand != "run_alphafold.py" {
		t.Errorf("Expected baseCommand 'run_alphafold.py', got %v", doc.BaseCommand)
	}

	// Verify Apptainer is primary requirement
	apptainerReq := doc.GetApptainerRequirement()
	if apptainerReq == nil {
		t.Error("Expected ApptainerRequirement to be present")
	} else if apptainerReq.ApptainerPull != "docker://catgumag/alphafold:2.3.2" {
		t.Errorf("Expected apptainerPull, got %s", apptainerReq.ApptainerPull)
	}

	// Verify CUDA requirement for GPU
	cudaReq := doc.GetCUDARequirement()
	if cudaReq == nil {
		t.Error("Expected CUDARequirement to be present")
	} else {
		if cudaReq.CUDAVersionMin != "11.0" {
			t.Errorf("Expected cudaVersionMin '11.0', got %s", cudaReq.CUDAVersionMin)
		}
		if cudaReq.CUDADeviceCountMin != 1 {
			t.Errorf("Expected cudaDeviceCountMin 1, got %d", cudaReq.CUDADeviceCountMin)
		}
		if cudaReq.CUDAComputeCapability != "7.0" {
			t.Errorf("Expected cudaComputeCapability '7.0', got %s", cudaReq.CUDAComputeCapability)
		}
	}

	// Verify enum type input
	var modelPresetInput *Input
	for i := range doc.Inputs {
		if doc.Inputs[i].ID == "model_preset" {
			modelPresetInput = &doc.Inputs[i]
			break
		}
	}
	if modelPresetInput == nil {
		t.Error("Expected 'model_preset' input")
	}

	// Verify container spec with GPU
	spec := doc.GetContainerSpec(RuntimeApptainer)
	if spec == nil {
		t.Error("Expected container spec")
	} else {
		if !spec.NeedsGPU {
			t.Error("Expected NeedsGPU to be true")
		}
		if spec.GPUCount != 1 {
			t.Errorf("Expected GPUCount 1, got %d", spec.GPUCount)
		}
		if spec.CUDAMinVersion != "11.0" {
			t.Errorf("Expected CUDAMinVersion '11.0', got %s", spec.CUDAMinVersion)
		}
	}
}

func TestParseExampleAlignReadsWorkflow(t *testing.T) {
	examplesDir := findExamplesDir(t)
	cwlPath := filepath.Join(examplesDir, "workflows", "align-reads.cwl")

	data, err := os.ReadFile(cwlPath)
	if err != nil {
		t.Fatalf("Failed to read align-reads.cwl: %v", err)
	}

	parser := NewParser()
	doc, err := parser.ParseBytes(data)
	if err != nil {
		t.Fatalf("Failed to parse align-reads.cwl: %v", err)
	}

	// Verify it's a workflow
	if doc.Class != "Workflow" {
		t.Errorf("Expected class Workflow, got %s", doc.Class)
	}

	// Verify workflow inputs
	expectedInputs := []string{"reference", "reads_1", "reads_2", "sample_id"}
	if len(doc.Inputs) != len(expectedInputs) {
		t.Errorf("Expected %d inputs, got %d", len(expectedInputs), len(doc.Inputs))
	}

	// Verify workflow outputs
	if len(doc.Outputs) != 2 {
		t.Errorf("Expected 2 outputs, got %d", len(doc.Outputs))
	}

	// Verify steps
	if len(doc.Steps) != 3 {
		t.Errorf("Expected 3 steps, got %d", len(doc.Steps))
	}

	// Check step names
	stepNames := make(map[string]bool)
	for _, step := range doc.Steps {
		stepNames[step.ID] = true
	}
	for _, expected := range []string{"align", "sam_to_bam", "flagstat"} {
		if !stepNames[expected] {
			t.Errorf("Expected step '%s' not found", expected)
		}
	}

	// Verify SubworkflowFeatureRequirement
	hasSubworkflowReq := false
	hasScatterReq := false
	for _, req := range doc.Requirements {
		if req.Class == "SubworkflowFeatureRequirement" {
			hasSubworkflowReq = true
		}
		if req.Class == "ScatterFeatureRequirement" {
			hasScatterReq = true
		}
	}
	if !hasSubworkflowReq {
		t.Error("Expected SubworkflowFeatureRequirement")
	}
	if !hasScatterReq {
		t.Error("Expected ScatterFeatureRequirement")
	}
}

func TestContainerSpecConversion(t *testing.T) {
	examplesDir := findExamplesDir(t)
	cwlPath := filepath.Join(examplesDir, "tools", "bwa-mem.cwl")

	data, err := os.ReadFile(cwlPath)
	if err != nil {
		t.Fatalf("Failed to read bwa-mem.cwl: %v", err)
	}

	parser := NewParser()
	doc, err := parser.ParseBytes(data)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	// Test Docker spec
	dockerSpec := doc.GetContainerSpec(RuntimeDocker)
	if dockerSpec.Runtime != RuntimeDocker {
		t.Errorf("Expected Docker runtime, got %s", dockerSpec.Runtime)
	}
	if dockerSpec.Image != "biocontainers/bwa:v0.7.17_cv1" {
		t.Errorf("Expected Docker image, got %s", dockerSpec.Image)
	}

	// Test Apptainer spec (should prefer native Apptainer hint)
	apptainerSpec := doc.GetContainerSpec(RuntimeApptainer)
	if apptainerSpec.Runtime != RuntimeApptainer {
		t.Errorf("Expected Apptainer runtime, got %s", apptainerSpec.Runtime)
	}
	// Should use the native apptainerPull from hints
	if apptainerSpec.Pull != "docker://biocontainers/bwa:v0.7.17_cv1" {
		t.Errorf("Expected Apptainer pull URI, got %s", apptainerSpec.Pull)
	}
}

// findExamplesDir locates the examples directory
func findExamplesDir(t *testing.T) string {
	// Try relative to working directory
	candidates := []string{
		"../../examples",
		"../examples",
		"examples",
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			abs, _ := filepath.Abs(candidate)
			return abs
		}
	}

	// Try from GOPATH or module root
	wd, _ := os.Getwd()
	t.Logf("Working directory: %s", wd)

	// Walk up looking for examples
	dir := wd
	for i := 0; i < 5; i++ {
		examplesPath := filepath.Join(dir, "examples")
		if _, err := os.Stat(examplesPath); err == nil {
			return examplesPath
		}
		dir = filepath.Dir(dir)
	}

	t.Skip("Could not find examples directory")
	return ""
}
