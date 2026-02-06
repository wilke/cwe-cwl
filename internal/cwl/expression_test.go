package cwl

import (
	"testing"
)

func TestExpressionEvaluator_SimpleReference(t *testing.T) {
	ee := NewExpressionEvaluator()
	ee.SetInputs(map[string]interface{}{
		"message": "hello",
		"count":   42,
	})

	testCases := []struct {
		expr     string
		expected interface{}
	}{
		{"$(inputs.message)", "hello"},
		{"$(inputs.count)", int64(42)},
	}

	for _, tc := range testCases {
		result, err := ee.Evaluate(tc.expr)
		if err != nil {
			t.Errorf("Failed to evaluate '%s': %v", tc.expr, err)
			continue
		}
		if result != tc.expected {
			t.Errorf("Evaluate('%s') = %v (%T), expected %v (%T)", tc.expr, result, result, tc.expected, tc.expected)
		}
	}
}

func TestExpressionEvaluator_NestedReference(t *testing.T) {
	ee := NewExpressionEvaluator()
	ee.SetInputs(map[string]interface{}{
		"file": map[string]interface{}{
			"class":    "File",
			"path":     "/path/to/file.txt",
			"basename": "file.txt",
		},
	})

	result, err := ee.Evaluate("$(inputs.file.path)")
	if err != nil {
		t.Fatalf("Failed to evaluate: %v", err)
	}
	if result != "/path/to/file.txt" {
		t.Errorf("Expected '/path/to/file.txt', got %v", result)
	}

	result, err = ee.Evaluate("$(inputs.file.basename)")
	if err != nil {
		t.Fatalf("Failed to evaluate: %v", err)
	}
	if result != "file.txt" {
		t.Errorf("Expected 'file.txt', got %v", result)
	}
}

func TestExpressionEvaluator_JavaScript(t *testing.T) {
	ee := NewExpressionEvaluator()
	ee.SetInputs(map[string]interface{}{
		"a": 10,
		"b": 20,
	})

	result, err := ee.Evaluate("${return inputs.a + inputs.b;}")
	if err != nil {
		t.Fatalf("Failed to evaluate: %v", err)
	}
	if result != int64(30) {
		t.Errorf("Expected 30, got %v (%T)", result, result)
	}
}

func TestExpressionEvaluator_RuntimeReference(t *testing.T) {
	ee := NewExpressionEvaluator()
	ee.SetRuntime(map[string]interface{}{
		"cores":  4,
		"ram":    8192,
		"outdir": "/output",
	})

	result, err := ee.Evaluate("$(runtime.cores)")
	if err != nil {
		t.Fatalf("Failed to evaluate: %v", err)
	}
	if result != int64(4) {
		t.Errorf("Expected 4, got %v", result)
	}
}

func TestExpressionEvaluator_SelfReference(t *testing.T) {
	ee := NewExpressionEvaluator()
	ee.SetSelf(map[string]interface{}{
		"path":     "/path/to/file.txt",
		"basename": "file.txt",
		"nameroot": "file",
		"nameext":  ".txt",
	})

	result, err := ee.Evaluate("$(self.nameroot)")
	if err != nil {
		t.Fatalf("Failed to evaluate: %v", err)
	}
	if result != "file" {
		t.Errorf("Expected 'file', got %v", result)
	}
}

func TestExpressionEvaluator_StringWithExpressions(t *testing.T) {
	ee := NewExpressionEvaluator()
	ee.SetInputs(map[string]interface{}{
		"prefix": "output",
		"suffix": "csv",
	})

	// Test string concatenation via JavaScript
	result, err := ee.Evaluate("${return inputs.prefix + '_results.' + inputs.suffix;}")
	if err != nil {
		t.Fatalf("Failed to evaluate: %v", err)
	}
	if result != "output_results.csv" {
		t.Errorf("Expected 'output_results.csv', got %v", result)
	}
}

func TestExpressionEvaluator_Literal(t *testing.T) {
	ee := NewExpressionEvaluator()

	// Non-expression strings should be returned as-is
	result, err := ee.Evaluate("hello world")
	if err != nil {
		t.Fatalf("Failed to evaluate: %v", err)
	}
	if result != "hello world" {
		t.Errorf("Expected 'hello world', got %v", result)
	}
}

func TestIsExpression(t *testing.T) {
	testCases := []struct {
		input    string
		expected bool
	}{
		{"$(inputs.foo)", true},
		{"${return 42;}", true},
		{"hello", false},
		{"$100", false},
		{"(inputs.foo)", false},
		{"{return 42}", false},
		{"$()", true},
		{"${}", true},
	}

	for _, tc := range testCases {
		result := IsExpression(tc.input)
		if result != tc.expected {
			t.Errorf("IsExpression('%s') = %v, expected %v", tc.input, result, tc.expected)
		}
	}
}

func TestExpressionEvaluator_EvaluateGlob(t *testing.T) {
	ee := NewExpressionEvaluator()
	ee.SetInputs(map[string]interface{}{
		"output_dir": "results",
		"format":     "pdb",
	})

	// Simple string glob
	patterns, err := ee.EvaluateGlob("*.txt")
	if err != nil {
		t.Fatalf("Failed to evaluate glob: %v", err)
	}
	if len(patterns) != 1 || patterns[0] != "*.txt" {
		t.Errorf("Expected ['*.txt'], got %v", patterns)
	}

	// Expression glob - use JavaScript for concatenation
	patterns, err = ee.EvaluateGlob("${return inputs.output_dir + '/*.' + inputs.format;}")
	if err != nil {
		t.Fatalf("Failed to evaluate glob: %v", err)
	}
	if len(patterns) != 1 || patterns[0] != "results/*.pdb" {
		t.Errorf("Expected ['results/*.pdb'], got %v", patterns)
	}

	// Array of globs
	patterns, err = ee.EvaluateGlob([]interface{}{"*.txt", "*.log"})
	if err != nil {
		t.Fatalf("Failed to evaluate glob: %v", err)
	}
	if len(patterns) != 2 {
		t.Errorf("Expected 2 patterns, got %d", len(patterns))
	}
}

func TestExpressionEvaluator_EvaluateCondition(t *testing.T) {
	ee := NewExpressionEvaluator()

	testCases := []struct {
		condition string
		inputs    map[string]interface{}
		expected  bool
	}{
		{
			"$(inputs.run_step)",
			map[string]interface{}{"run_step": true},
			true,
		},
		{
			"$(inputs.run_step)",
			map[string]interface{}{"run_step": false},
			false,
		},
		{
			"$(inputs.count > 0)",
			map[string]interface{}{"count": 5},
			true,
		},
		{
			"$(inputs.count > 0)",
			map[string]interface{}{"count": 0},
			false,
		},
	}

	for _, tc := range testCases {
		result, err := ee.EvaluateCondition(tc.condition, tc.inputs)
		if err != nil {
			t.Errorf("Failed to evaluate condition '%s': %v", tc.condition, err)
			continue
		}
		if result != tc.expected {
			t.Errorf("EvaluateCondition('%s', %v) = %v, expected %v", tc.condition, tc.inputs, result, tc.expected)
		}
	}
}

func TestResolveSecondaryFilePattern(t *testing.T) {
	testCases := []struct {
		primary  string
		pattern  string
		expected string
	}{
		// Suffix patterns
		{"/path/to/file.bam", ".bai", "/path/to/file.bam.bai"},
		{"/path/to/file.fasta", ".fai", "/path/to/file.fasta.fai"},

		// Caret patterns (replace extension)
		{"/path/to/file.bam", "^.bai", "/path/to/file.bai"},
		{"/path/to/file.fasta", "^.fai", "/path/to/file.fai"},
		{"/path/to/file.tar.gz", "^^.txt", "/path/to/file.txt"},
	}

	for _, tc := range testCases {
		result := resolveSecondaryFilePattern(tc.primary, tc.pattern)
		if result != tc.expected {
			t.Errorf("resolveSecondaryFilePattern('%s', '%s') = '%s', expected '%s'",
				tc.primary, tc.pattern, result, tc.expected)
		}
	}
}

func TestGetBasename(t *testing.T) {
	testCases := []struct {
		path     string
		expected string
	}{
		{"/path/to/file.txt", "file.txt"},
		{"file.txt", "file.txt"},
		{"/path/to/", ""},
		{"/", ""},
	}

	for _, tc := range testCases {
		result := getBasename(tc.path)
		if result != tc.expected {
			t.Errorf("getBasename('%s') = '%s', expected '%s'", tc.path, result, tc.expected)
		}
	}
}

func TestGetNameroot(t *testing.T) {
	testCases := []struct {
		path     string
		expected string
	}{
		{"/path/to/file.txt", "file"},
		{"file.txt", "file"},
		{"file", "file"},
		{"file.tar.gz", "file.tar"},
	}

	for _, tc := range testCases {
		result := getNameroot(tc.path)
		if result != tc.expected {
			t.Errorf("getNameroot('%s') = '%s', expected '%s'", tc.path, result, tc.expected)
		}
	}
}

func TestGetNameext(t *testing.T) {
	testCases := []struct {
		path     string
		expected string
	}{
		{"/path/to/file.txt", ".txt"},
		{"file.txt", ".txt"},
		{"file", ""},
		{"file.tar.gz", ".gz"},
	}

	for _, tc := range testCases {
		result := getNameext(tc.path)
		if result != tc.expected {
			t.Errorf("getNameext('%s') = '%s', expected '%s'", tc.path, result, tc.expected)
		}
	}
}
