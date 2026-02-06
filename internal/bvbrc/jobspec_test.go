package bvbrc

import (
	"testing"

	"github.com/BV-BRC/cwe-cwl/internal/cwl"
)

func TestNewCWLJobSpec(t *testing.T) {
	doc := &cwl.Document{
		Class: "CommandLineTool",
		ID:    "bwa-mem.cwl",
		Label: "BWA MEM Aligner",
		Doc:   "Aligns reads to a reference genome using BWA MEM",
		Inputs: []cwl.Input{
			{
				ID:    "reference",
				Label: "Reference Genome",
				Doc:   "Reference genome FASTA file",
				Type:  "File",
			},
			{
				ID:    "reads",
				Label: "Input Reads",
				Doc:   "FASTQ file with reads",
				Type:  "File",
			},
			{
				ID:      "threads",
				Label:   "Threads",
				Doc:     "Number of threads",
				Type:    "int?",
				Default: 4,
			},
		},
	}

	inputs := map[string]interface{}{
		"reference": map[string]interface{}{
			"class": "File",
			"path":  "/ws/genome.fa",
		},
		"reads": map[string]interface{}{
			"class": "File",
			"path":  "/ws/reads.fq",
		},
	}

	spec, err := NewCWLJobSpec(doc, inputs, "/ws/output")
	if err != nil {
		t.Fatalf("NewCWLJobSpec failed: %v", err)
	}

	if spec.Tool != doc {
		t.Error("Tool should be the same document")
	}
	if spec.OutputPath != "/ws/output" {
		t.Errorf("Expected output path /ws/output, got %s", spec.OutputPath)
	}
	if len(spec.Inputs) != 2 {
		t.Errorf("Expected 2 inputs, got %d", len(spec.Inputs))
	}
}

func TestNewCWLJobSpec_MissingRequired(t *testing.T) {
	doc := &cwl.Document{
		Class: "CommandLineTool",
		Inputs: []cwl.Input{
			{ID: "required_input", Type: "File"},
		},
	}

	inputs := map[string]interface{}{} // Missing required input

	_, err := NewCWLJobSpec(doc, inputs, "/output")
	if err == nil {
		t.Error("Expected error for missing required input")
	}
}

func TestNewCWLJobSpec_OptionalMissing(t *testing.T) {
	doc := &cwl.Document{
		Class: "CommandLineTool",
		Inputs: []cwl.Input{
			{ID: "optional_input", Type: "File?"},
		},
	}

	inputs := map[string]interface{}{} // Missing optional is OK

	spec, err := NewCWLJobSpec(doc, inputs, "/output")
	if err != nil {
		t.Fatalf("NewCWLJobSpec failed: %v", err)
	}
	if spec == nil {
		t.Error("Expected non-nil spec")
	}
}

func TestNewCWLJobSpec_UnionTypeOptional(t *testing.T) {
	doc := &cwl.Document{
		Class: "CommandLineTool",
		Inputs: []cwl.Input{
			{ID: "optional_file", Type: []interface{}{"null", "File"}},
		},
	}

	inputs := map[string]interface{}{} // Missing optional is OK

	spec, err := NewCWLJobSpec(doc, inputs, "/output")
	if err != nil {
		t.Fatalf("NewCWLJobSpec failed: %v", err)
	}
	if spec == nil {
		t.Error("Expected non-nil spec")
	}
}

func TestNewCWLJobSpec_WithDefault(t *testing.T) {
	doc := &cwl.Document{
		Class: "CommandLineTool",
		Inputs: []cwl.Input{
			{ID: "count", Type: "int", Default: 10},
		},
	}

	inputs := map[string]interface{}{} // Has default, so OK

	spec, err := NewCWLJobSpec(doc, inputs, "/output")
	if err != nil {
		t.Fatalf("NewCWLJobSpec failed: %v", err)
	}
	if spec == nil {
		t.Error("Expected non-nil spec")
	}
}

func TestNewCWLJobSpec_WrongClass(t *testing.T) {
	doc := &cwl.Document{
		Class: "Workflow",
	}

	_, err := NewCWLJobSpec(doc, nil, "/output")
	if err == nil {
		t.Error("Expected error for non-CommandLineTool")
	}
}

func TestCWLJobSpec_GetContainerID(t *testing.T) {
	doc := &cwl.Document{
		Class: "CommandLineTool",
		Requirements: []cwl.Requirement{
			{
				Class:      "DockerRequirement",
				DockerPull: "biocontainers/bwa:0.7.17",
			},
		},
	}

	spec := &CWLJobSpec{Tool: doc}
	containerID := spec.GetContainerID()

	if containerID != "biocontainers/bwa:0.7.17" {
		t.Errorf("Expected 'biocontainers/bwa:0.7.17', got %s", containerID)
	}
}

func TestCWLJobSpec_GetContainerID_Apptainer(t *testing.T) {
	doc := &cwl.Document{
		Class: "CommandLineTool",
		Requirements: []cwl.Requirement{
			{
				Class:         "ApptainerRequirement",
				ApptainerPull: "docker://biocontainers/bwa:0.7.17",
			},
		},
	}

	spec := &CWLJobSpec{Tool: doc}
	containerID := spec.GetContainerID()

	if containerID != "docker://biocontainers/bwa:0.7.17" {
		t.Errorf("Expected 'docker://biocontainers/bwa:0.7.17', got %s", containerID)
	}
}

func TestResolveInputValue(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		expected string
	}{
		{"string", "hello", "hello"},
		{"int", 42, "42"},
		{"float", 3.14, "3.14"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{
			"file object",
			map[string]interface{}{"class": "File", "path": "/data/file.txt"},
			"/data/file.txt",
		},
		{
			"file with location",
			map[string]interface{}{"class": "File", "location": "file:///data/file.txt"},
			"file:///data/file.txt",
		},
		{
			"directory object",
			map[string]interface{}{"class": "Directory", "path": "/data/dir"},
			"/data/dir",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ResolveInputValue(tc.value)
			if err != nil {
				t.Fatalf("ResolveInputValue failed: %v", err)
			}
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestResolveInputValue_Array(t *testing.T) {
	value := []interface{}{
		map[string]interface{}{"class": "File", "path": "/a.txt"},
		map[string]interface{}{"class": "File", "path": "/b.txt"},
	}

	result, err := ResolveInputValue(value)
	if err != nil {
		t.Fatalf("ResolveInputValue failed: %v", err)
	}

	expected := `["/a.txt","/b.txt"]`
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestCWLJobSpec_ToJSON(t *testing.T) {
	doc := &cwl.Document{
		Class: "CommandLineTool",
		ID:    "test-tool",
		Inputs: []cwl.Input{
			{ID: "input1", Type: "File"},
		},
	}

	spec := &CWLJobSpec{
		Tool:       doc,
		Inputs:     map[string]interface{}{"input1": "/path/to/file"},
		OutputPath: "/output",
		Owner:      "user@example.com",
	}

	data, err := spec.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	// Parse back
	parsed, err := FromJSON(data)
	if err != nil {
		t.Fatalf("FromJSON failed: %v", err)
	}

	if parsed.OutputPath != "/output" {
		t.Errorf("Expected output path /output, got %s", parsed.OutputPath)
	}
	if parsed.Owner != "user@example.com" {
		t.Errorf("Expected owner user@example.com, got %s", parsed.Owner)
	}
}

func TestIsOptional(t *testing.T) {
	tests := []struct {
		name     string
		input    cwl.Input
		expected bool
	}{
		{"required string", cwl.Input{Type: "string"}, false},
		{"optional string", cwl.Input{Type: "string?"}, true},
		{"required File", cwl.Input{Type: "File"}, false},
		{"optional File", cwl.Input{Type: "File?"}, true},
		{"null union", cwl.Input{Type: []interface{}{"null", "File"}}, true},
		{"non-null union", cwl.Input{Type: []interface{}{"File", "Directory"}}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isOptional(tc.input)
			if result != tc.expected {
				t.Errorf("Expected isOptional=%v, got %v", tc.expected, result)
			}
		})
	}
}
