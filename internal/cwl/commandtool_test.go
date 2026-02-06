package cwl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommandBuilder_BuildCommand(t *testing.T) {
	parser := NewParser()

	// Load real tool for testing
	toolPath := filepath.Join(testDataPath, "tools/boltz.cwl")
	if _, err := os.Stat(toolPath); os.IsNotExist(err) {
		t.Skip("Test data not available:", toolPath)
	}

	doc, err := parser.ParseFile(toolPath)
	if err != nil {
		t.Fatalf("Failed to parse tool: %v", err)
	}

	// Create inputs
	inputs := map[string]interface{}{
		"input_file": map[string]interface{}{
			"class": "File",
			"path":  "/path/to/sequence.yaml",
		},
		"output_dir":        "my_output",
		"use_msa_server":    true,
		"recycling_steps":   5,
		"diffusion_samples": 2,
		"output_format":     "pdb",
	}

	builder := NewCommandBuilder(doc, inputs)
	cmd, err := builder.BuildCommand()
	if err != nil {
		t.Fatalf("Failed to build command: %v", err)
	}

	// Verify base command
	if len(cmd) < 2 {
		t.Fatalf("Command too short: %v", cmd)
	}
	if cmd[0] != "boltz" || cmd[1] != "predict" {
		t.Errorf("Expected 'boltz predict', got '%s %s'", cmd[0], cmd[1])
	}

	// Check that input_file is in the command (position 1)
	found := false
	for _, arg := range cmd {
		if arg == "/path/to/sequence.yaml" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected input file path in command: %v", cmd)
	}

	// Check that flags are present
	hasOutputDir := false
	hasRecyclingSteps := false
	for i, arg := range cmd {
		if arg == "--out_dir" && i+1 < len(cmd) && cmd[i+1] == "my_output" {
			hasOutputDir = true
		}
		if arg == "--recycling_steps" && i+1 < len(cmd) && cmd[i+1] == "5" {
			hasRecyclingSteps = true
		}
	}
	if !hasOutputDir {
		t.Errorf("Expected --out_dir in command: %v", cmd)
	}
	if !hasRecyclingSteps {
		t.Errorf("Expected --recycling_steps in command: %v", cmd)
	}
}

func TestCommandBuilder_BuildCommand_Simple(t *testing.T) {
	doc := &Document{
		CWLVersion:  "v1.2",
		Class:       ClassCommandLineTool,
		BaseCommand: "echo",
		Inputs: []Input{
			{
				ID:   "message",
				Type: "string",
				InputBinding: &CommandLineBinding{
					Position: 1,
				},
			},
		},
		Outputs: []Output{
			{
				ID:   "output",
				Type: "stdout",
			},
		},
	}

	inputs := map[string]interface{}{
		"message": "Hello, World!",
	}

	builder := NewCommandBuilder(doc, inputs)
	cmd, err := builder.BuildCommand()
	if err != nil {
		t.Fatalf("Failed to build command: %v", err)
	}

	expected := []string{"echo", "Hello, World!"}
	if len(cmd) != len(expected) {
		t.Fatalf("Expected command %v, got %v", expected, cmd)
	}
	for i := range expected {
		if cmd[i] != expected[i] {
			t.Errorf("Expected cmd[%d]=%s, got %s", i, expected[i], cmd[i])
		}
	}
}

func TestCommandBuilder_BuildCommand_WithPrefix(t *testing.T) {
	doc := &Document{
		CWLVersion:  "v1.2",
		Class:       ClassCommandLineTool,
		BaseCommand: "grep",
		Inputs: []Input{
			{
				ID:   "pattern",
				Type: "string",
				InputBinding: &CommandLineBinding{
					Position: 1,
					Prefix:   "-e",
				},
			},
			{
				ID:   "input_file",
				Type: "File",
				InputBinding: &CommandLineBinding{
					Position: 2,
				},
			},
		},
		Outputs: []Output{},
	}

	inputs := map[string]interface{}{
		"pattern": "hello",
		"input_file": map[string]interface{}{
			"class": "File",
			"path":  "/path/to/file.txt",
		},
	}

	builder := NewCommandBuilder(doc, inputs)
	cmd, err := builder.BuildCommand()
	if err != nil {
		t.Fatalf("Failed to build command: %v", err)
	}

	// Should be: grep -e hello /path/to/file.txt
	if len(cmd) < 4 {
		t.Fatalf("Expected at least 4 args, got %v", cmd)
	}
	if cmd[0] != "grep" {
		t.Errorf("Expected 'grep', got '%s'", cmd[0])
	}

	// Check -e hello
	hasPrefix := false
	for i, arg := range cmd {
		if arg == "-e" && i+1 < len(cmd) && cmd[i+1] == "hello" {
			hasPrefix = true
			break
		}
	}
	if !hasPrefix {
		t.Errorf("Expected '-e hello' in command: %v", cmd)
	}
}

func TestCommandBuilder_BuildCommand_ArrayInput(t *testing.T) {
	doc := &Document{
		CWLVersion:  "v1.2",
		Class:       ClassCommandLineTool,
		BaseCommand: "echo",
		Inputs: []Input{
			{
				ID:   "items",
				Type: map[string]interface{}{"type": "array", "items": "string"},
				InputBinding: &CommandLineBinding{
					Position:      1,
					ItemSeparator: ",",
				},
			},
		},
		Outputs: []Output{},
	}

	inputs := map[string]interface{}{
		"items": []interface{}{"a", "b", "c"},
	}

	builder := NewCommandBuilder(doc, inputs)
	cmd, err := builder.BuildCommand()
	if err != nil {
		t.Fatalf("Failed to build command: %v", err)
	}

	// Should have "a,b,c" joined
	found := false
	for _, arg := range cmd {
		if arg == "a,b,c" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected 'a,b,c' in command: %v", cmd)
	}
}

func TestCommandBuilder_OptionalInput(t *testing.T) {
	doc := &Document{
		CWLVersion:  "v1.2",
		Class:       ClassCommandLineTool,
		BaseCommand: "echo",
		Inputs: []Input{
			{
				ID:   "required",
				Type: "string",
				InputBinding: &CommandLineBinding{
					Position: 1,
				},
			},
			{
				ID:   "optional",
				Type: []interface{}{"null", "string"},
				InputBinding: &CommandLineBinding{
					Position: 2,
					Prefix:   "--opt",
				},
			},
		},
		Outputs: []Output{},
	}

	// Test without optional input
	inputs := map[string]interface{}{
		"required": "hello",
	}

	builder := NewCommandBuilder(doc, inputs)
	cmd, err := builder.BuildCommand()
	if err != nil {
		t.Fatalf("Failed to build command: %v", err)
	}

	// Should not have --opt
	for _, arg := range cmd {
		if arg == "--opt" {
			t.Errorf("Did not expect --opt in command: %v", cmd)
		}
	}

	// Test with optional input
	inputs["optional"] = "world"
	builder = NewCommandBuilder(doc, inputs)
	cmd, err = builder.BuildCommand()
	if err != nil {
		t.Fatalf("Failed to build command: %v", err)
	}

	// Should have --opt
	hasOpt := false
	for _, arg := range cmd {
		if arg == "--opt" {
			hasOpt = true
			break
		}
	}
	if !hasOpt {
		t.Errorf("Expected --opt in command: %v", cmd)
	}
}

func TestDocument_GetDockerImage(t *testing.T) {
	parser := NewParser()

	toolPath := filepath.Join(testDataPath, "tools/boltz.cwl")
	if _, err := os.Stat(toolPath); os.IsNotExist(err) {
		t.Skip("Test data not available:", toolPath)
	}

	doc, err := parser.ParseFile(toolPath)
	if err != nil {
		t.Fatalf("Failed to parse tool: %v", err)
	}

	image := doc.GetDockerImage()
	if image != "dxkb/boltz-bvbrc:latest-gpu" {
		t.Errorf("Expected docker image 'dxkb/boltz-bvbrc:latest-gpu', got '%s'", image)
	}
}

func TestDocument_GetResourceRequirements(t *testing.T) {
	parser := NewParser()

	toolPath := filepath.Join(testDataPath, "tools/boltz.cwl")
	if _, err := os.Stat(toolPath); os.IsNotExist(err) {
		t.Skip("Test data not available:", toolPath)
	}

	doc, err := parser.ParseFile(toolPath)
	if err != nil {
		t.Fatalf("Failed to parse tool: %v", err)
	}

	cores, ram, err := doc.GetResourceRequirements()
	if err != nil {
		t.Fatalf("Failed to get resource requirements: %v", err)
	}

	if cores != 4 {
		t.Errorf("Expected 4 cores, got %d", cores)
	}
	if ram != 65536 {
		t.Errorf("Expected 65536 MB RAM, got %d", ram)
	}
}

func TestDocument_HasRequirement(t *testing.T) {
	parser := NewParser()

	toolPath := filepath.Join(testDataPath, "tools/boltz.cwl")
	if _, err := os.Stat(toolPath); os.IsNotExist(err) {
		t.Skip("Test data not available:", toolPath)
	}

	doc, err := parser.ParseFile(toolPath)
	if err != nil {
		t.Fatalf("Failed to parse tool: %v", err)
	}

	if !doc.HasRequirement("DockerRequirement") {
		t.Error("Expected DockerRequirement")
	}
	if !doc.HasRequirement("ResourceRequirement") {
		t.Error("Expected ResourceRequirement")
	}
	if !doc.HasRequirement("InlineJavascriptRequirement") {
		t.Error("Expected InlineJavascriptRequirement")
	}
	if doc.HasRequirement("ScatterFeatureRequirement") {
		t.Error("Did not expect ScatterFeatureRequirement")
	}
}
