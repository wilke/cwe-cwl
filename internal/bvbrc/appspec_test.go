package bvbrc

import (
	"testing"

	"github.com/BV-BRC/cwe-cwl/internal/cwl"
)

func TestCWLToAppSpec(t *testing.T) {
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

	spec, err := CWLToAppSpec(doc)
	if err != nil {
		t.Fatalf("CWLToAppSpec failed: %v", err)
	}

	// Check basic fields
	if spec.ID != "bwa_mem" {
		t.Errorf("Expected ID bwa_mem, got %s", spec.ID)
	}
	if spec.Label != "BWA MEM Aligner" {
		t.Errorf("Expected label 'BWA MEM Aligner', got %s", spec.Label)
	}

	// Should have 5 parameters: output_path, output_file, reference, reads, threads
	if len(spec.Parameters) != 5 {
		t.Errorf("Expected 5 parameters, got %d", len(spec.Parameters))
	}

	// Check reference parameter
	var refParam *AppParameter
	for i := range spec.Parameters {
		if spec.Parameters[i].ID == "reference" {
			refParam = &spec.Parameters[i]
			break
		}
	}
	if refParam == nil {
		t.Fatal("reference parameter not found")
	}
	if refParam.Required != 1 {
		t.Error("reference should be required")
	}
	if refParam.Type != "wsid" {
		t.Errorf("reference type should be wsid, got %s", refParam.Type)
	}

	// Check threads parameter (optional with default)
	var threadsParam *AppParameter
	for i := range spec.Parameters {
		if spec.Parameters[i].ID == "threads" {
			threadsParam = &spec.Parameters[i]
			break
		}
	}
	if threadsParam == nil {
		t.Fatal("threads parameter not found")
	}
	if threadsParam.Required != 0 {
		t.Error("threads should be optional")
	}
	if threadsParam.Type != "int" {
		t.Errorf("threads type should be int, got %s", threadsParam.Type)
	}
	if threadsParam.Default != 4 {
		t.Errorf("threads default should be 4, got %v", threadsParam.Default)
	}
}

func TestCWLToAppSpec_EnumType(t *testing.T) {
	doc := &cwl.Document{
		Class: "CommandLineTool",
		ID:    "tool",
		Inputs: []cwl.Input{
			{
				ID: "mode",
				Type: map[string]interface{}{
					"type":    "enum",
					"symbols": []interface{}{"fast", "normal", "slow"},
				},
			},
		},
	}

	spec, err := CWLToAppSpec(doc)
	if err != nil {
		t.Fatalf("CWLToAppSpec failed: %v", err)
	}

	// Find mode parameter
	var modeParam *AppParameter
	for i := range spec.Parameters {
		if spec.Parameters[i].ID == "mode" {
			modeParam = &spec.Parameters[i]
			break
		}
	}
	if modeParam == nil {
		t.Fatal("mode parameter not found")
	}
	if modeParam.Type != "enum" {
		t.Errorf("mode type should be enum, got %s", modeParam.Type)
	}
	if modeParam.Enum != "fast,normal,slow" {
		t.Errorf("mode enum should be 'fast,normal,slow', got %s", modeParam.Enum)
	}
}

func TestCWLToAppSpec_UnionType(t *testing.T) {
	doc := &cwl.Document{
		Class: "CommandLineTool",
		ID:    "tool",
		Inputs: []cwl.Input{
			{
				ID:   "optional_file",
				Type: []interface{}{"null", "File"},
			},
		},
	}

	spec, err := CWLToAppSpec(doc)
	if err != nil {
		t.Fatalf("CWLToAppSpec failed: %v", err)
	}

	var param *AppParameter
	for i := range spec.Parameters {
		if spec.Parameters[i].ID == "optional_file" {
			param = &spec.Parameters[i]
			break
		}
	}
	if param == nil {
		t.Fatal("optional_file parameter not found")
	}
	if param.Required != 0 {
		t.Error("optional_file should be optional (union with null)")
	}
	if param.Type != "wsid" {
		t.Errorf("optional_file type should be wsid, got %s", param.Type)
	}
}

func TestBuildJobSpec(t *testing.T) {
	doc := &cwl.Document{
		Class: "CommandLineTool",
		Inputs: []cwl.Input{
			{ID: "input_file", Type: "File"},
			{ID: "count", Type: "int"},
			{ID: "flag", Type: "boolean"},
		},
	}

	inputs := map[string]interface{}{
		"input_file": map[string]interface{}{
			"class": "File",
			"path":  "/user/home/data/reads.fastq",
		},
		"count": 10,
		"flag":  true,
	}

	params, err := BuildJobSpec(doc, inputs, "/user/home/output")
	if err != nil {
		t.Fatalf("BuildJobSpec failed: %v", err)
	}

	if params["output_path"] != "/user/home/output" {
		t.Errorf("Expected output_path '/user/home/output', got %s", params["output_path"])
	}
	if params["input_file"] != "/user/home/data/reads.fastq" {
		t.Errorf("Expected input_file path, got %s", params["input_file"])
	}
	if params["count"] != "10" {
		t.Errorf("Expected count '10', got %s", params["count"])
	}
	if params["flag"] != "1" {
		t.Errorf("Expected flag '1', got %s", params["flag"])
	}
}

func TestBuildJobSpec_MissingRequired(t *testing.T) {
	doc := &cwl.Document{
		Class: "CommandLineTool",
		Inputs: []cwl.Input{
			{ID: "required_input", Type: "File"},
		},
	}

	inputs := map[string]interface{}{} // Missing required input

	_, err := BuildJobSpec(doc, inputs, "/output")
	if err == nil {
		t.Error("Expected error for missing required input")
	}
}

func TestBuildJobSpec_OptionalMissing(t *testing.T) {
	doc := &cwl.Document{
		Class: "CommandLineTool",
		Inputs: []cwl.Input{
			{ID: "optional_input", Type: "File?"},
		},
	}

	inputs := map[string]interface{}{} // Missing optional is OK

	params, err := BuildJobSpec(doc, inputs, "/output")
	if err != nil {
		t.Fatalf("BuildJobSpec failed: %v", err)
	}

	if _, ok := params["optional_input"]; ok {
		t.Error("optional_input should not be in params when not provided")
	}
}

func TestValueToString(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		expected string
	}{
		{"string", "hello", "hello"},
		{"int", 42, "42"},
		{"float", 3.14, "3.14"},
		{"bool true", true, "1"},
		{"bool false", false, "0"},
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := valueToString(tc.value)
			if err != nil {
				t.Fatalf("valueToString failed: %v", err)
			}
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestParseTypeString(t *testing.T) {
	tests := []struct {
		name       string
		typeVal    interface{}
		expected   string
		isOptional bool
	}{
		{"simple string", "string", "string", false},
		{"optional string", "string?", "string", true},
		{"file", "File", "File", false},
		{"file array", "File[]", "File[]", false},
		{"null union", []interface{}{"null", "File"}, "File", true},
		{"non-null union", []interface{}{"File", "Directory"}, "Directory", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, optional := parseTypeString(tc.typeVal)
			if result != tc.expected {
				t.Errorf("Expected type %q, got %q", tc.expected, result)
			}
			if optional != tc.isOptional {
				t.Errorf("Expected optional=%v, got %v", tc.isOptional, optional)
			}
		})
	}
}

func TestSanitizeAppID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"bwa-mem", "bwa_mem"},
		{"bwa-mem.cwl", "bwa_mem"},
		{"tools/bwa-mem.cwl", "bwa_mem"},
		{"BWA.MEM", "BWA_MEM"},
	}

	for _, tc := range tests {
		result := sanitizeAppID(tc.input)
		if result != tc.expected {
			t.Errorf("sanitizeAppID(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestGetContainerID(t *testing.T) {
	doc := &cwl.Document{
		Requirements: []cwl.Requirement{
			{
				Class:      "DockerRequirement",
				DockerPull: "biocontainers/bwa:0.7.17",
			},
		},
	}

	containerID := GetContainerID(doc)
	if containerID != "biocontainers/bwa:0.7.17" {
		t.Errorf("Expected 'biocontainers/bwa:0.7.17', got %s", containerID)
	}
}
