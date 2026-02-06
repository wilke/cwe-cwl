package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadParamsFromFile(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Write params file
	params := StepParams{
		Command: []string{"echo", "hello"},
		Inputs: map[string]interface{}{
			"message": "world",
		},
		Outputs: []OutputBinding{
			{ID: "output", Type: "File", Glob: "*.txt"},
		},
	}
	data, _ := json.Marshal(params)
	paramsFile := filepath.Join(tmpDir, "cwl_params.json")
	os.WriteFile(paramsFile, data, 0644)

	// Change to temp dir and set env
	oldDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldDir)

	// Load params
	loaded, err := loadParams()
	if err != nil {
		t.Fatalf("loadParams failed: %v", err)
	}

	if len(loaded.Command) != 2 || loaded.Command[0] != "echo" {
		t.Errorf("Expected command [echo hello], got %v", loaded.Command)
	}
	if loaded.Inputs["message"] != "world" {
		t.Errorf("Expected input message=world, got %v", loaded.Inputs["message"])
	}
}

func TestLoadParamsFromEnv(t *testing.T) {
	// Set environment variables
	os.Setenv("CWL_COMMAND", `["ls", "-la"]`)
	os.Setenv("CWL_INPUTS", `{"path": "/tmp"}`)
	os.Setenv("CWL_OUTPUTS", `[{"id": "files", "type": "File[]", "glob": "*"}]`)
	os.Setenv("CWL_STEP_ID", "list_files")
	defer func() {
		os.Unsetenv("CWL_COMMAND")
		os.Unsetenv("CWL_INPUTS")
		os.Unsetenv("CWL_OUTPUTS")
		os.Unsetenv("CWL_STEP_ID")
	}()

	// Use non-existent params file to force env loading
	os.Setenv("CWL_PARAMS_FILE", "/nonexistent/params.json")
	defer os.Unsetenv("CWL_PARAMS_FILE")

	params, err := loadParams()
	if err != nil {
		t.Fatalf("loadParams failed: %v", err)
	}

	if len(params.Command) != 2 || params.Command[0] != "ls" {
		t.Errorf("Expected command [ls -la], got %v", params.Command)
	}
	if params.StepID != "list_files" {
		t.Errorf("Expected step_id list_files, got %s", params.StepID)
	}
}

func TestExecuteCommand(t *testing.T) {
	tmpDir := t.TempDir()

	params := &StepParams{
		Command: []string{"echo", "hello world"},
		Stdout:  "output.txt",
	}

	exitCode, err := executeCommand(params, tmpDir)
	if err != nil {
		t.Fatalf("executeCommand failed: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", exitCode)
	}

	// Check output file
	content, err := os.ReadFile(filepath.Join(tmpDir, "output.txt"))
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}
	if string(content) != "hello world\n" {
		t.Errorf("Expected 'hello world\\n', got %q", string(content))
	}
}

func TestExecuteCommandWithStdin(t *testing.T) {
	tmpDir := t.TempDir()

	// Create input file
	inputFile := filepath.Join(tmpDir, "input.txt")
	os.WriteFile(inputFile, []byte("test input"), 0644)

	params := &StepParams{
		Command: []string{"cat"},
		Stdin:   "input.txt",
		Stdout:  "output.txt",
	}

	exitCode, err := executeCommand(params, tmpDir)
	if err != nil {
		t.Fatalf("executeCommand failed: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", exitCode)
	}

	// Check output
	content, _ := os.ReadFile(filepath.Join(tmpDir, "output.txt"))
	if string(content) != "test input" {
		t.Errorf("Expected 'test input', got %q", string(content))
	}
}

func TestExecuteCommandWithEnv(t *testing.T) {
	tmpDir := t.TempDir()

	params := &StepParams{
		Command: []string{"sh", "-c", "echo $MY_VAR"},
		Environment: map[string]string{
			"MY_VAR": "custom_value",
		},
		Stdout: "output.txt",
	}

	exitCode, err := executeCommand(params, tmpDir)
	if err != nil {
		t.Fatalf("executeCommand failed: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", exitCode)
	}

	content, _ := os.ReadFile(filepath.Join(tmpDir, "output.txt"))
	if string(content) != "custom_value\n" {
		t.Errorf("Expected 'custom_value\\n', got %q", string(content))
	}
}

func TestCollectOutputs(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	os.WriteFile(filepath.Join(tmpDir, "result.txt"), []byte("output data"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "data.csv"), []byte("a,b,c"), 0644)

	bindings := []OutputBinding{
		{ID: "text_file", Type: "File", Glob: "*.txt"},
		{ID: "all_files", Type: "File[]", Glob: "*.*"},
	}

	outputs, err := collectOutputs(bindings, nil, tmpDir)
	if err != nil {
		t.Fatalf("collectOutputs failed: %v", err)
	}

	// Check single file output
	textFile, ok := outputs["text_file"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected text_file to be a map, got %T", outputs["text_file"])
	}
	if textFile["basename"] != "result.txt" {
		t.Errorf("Expected basename result.txt, got %v", textFile["basename"])
	}

	// Check array output
	allFiles, ok := outputs["all_files"].([]interface{})
	if !ok {
		t.Fatalf("Expected all_files to be an array, got %T", outputs["all_files"])
	}
	if len(allFiles) != 2 {
		t.Errorf("Expected 2 files, got %d", len(allFiles))
	}
}

func TestCollectOutputWithLoadContents(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test file
	os.WriteFile(filepath.Join(tmpDir, "small.txt"), []byte("content here"), 0644)

	binding := OutputBinding{
		ID:           "file",
		Type:         "File",
		Glob:         "small.txt",
		LoadContents: true,
	}

	output, err := collectOutput(binding, nil, tmpDir)
	if err != nil {
		t.Fatalf("collectOutput failed: %v", err)
	}

	fileObj := output.(map[string]interface{})
	if fileObj["contents"] != "content here" {
		t.Errorf("Expected contents 'content here', got %v", fileObj["contents"])
	}
}

func TestBuildFileObject(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.data.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	obj, err := buildFileObject(testFile, false, "", tmpDir)
	if err != nil {
		t.Fatalf("buildFileObject failed: %v", err)
	}

	if obj["class"] != "File" {
		t.Errorf("Expected class File, got %v", obj["class"])
	}
	if obj["basename"] != "test.data.txt" {
		t.Errorf("Expected basename test.data.txt, got %v", obj["basename"])
	}
	if obj["nameroot"] != "test.data" {
		t.Errorf("Expected nameroot test.data, got %v", obj["nameroot"])
	}
	if obj["nameext"] != ".txt" {
		t.Errorf("Expected nameext .txt, got %v", obj["nameext"])
	}
	if obj["size"].(int64) != 4 {
		t.Errorf("Expected size 4, got %v", obj["size"])
	}
}

func TestBuildDirectoryListing(t *testing.T) {
	tmpDir := t.TempDir()

	// Create subdirectory with files
	subDir := filepath.Join(tmpDir, "subdir")
	os.Mkdir(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "file1.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(subDir, "file2.txt"), []byte("bb"), 0644)

	listing, err := buildDirectoryListing(subDir, false, tmpDir)
	if err != nil {
		t.Fatalf("buildDirectoryListing failed: %v", err)
	}

	if len(listing) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(listing))
	}
}

func TestWriteResult(t *testing.T) {
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "result.json")
	os.Setenv("CWL_OUTPUT_FILE", outputFile)
	defer os.Unsetenv("CWL_OUTPUT_FILE")

	result := StepResult{
		Status:   "completed",
		ExitCode: 0,
		Outputs: map[string]interface{}{
			"output": map[string]interface{}{
				"class":    "File",
				"basename": "result.txt",
			},
		},
	}

	writeResult(result)

	// Read and verify
	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read result file: %v", err)
	}

	var loaded StepResult
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if loaded.Status != "completed" {
		t.Errorf("Expected status completed, got %s", loaded.Status)
	}
}
