package sandbox

import (
	"context"
	"testing"
	"time"
)

func TestInProcessEvaluator_SimpleExpression(t *testing.T) {
	eval := NewInProcessEvaluator()
	defer eval.Close()

	ctx := context.Background()
	req := Request{
		Expression: "inputs.a + inputs.b",
		Inputs: map[string]interface{}{
			"a": 1.0,
			"b": 2.0,
		},
	}

	result, err := eval.Evaluate(ctx, req)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if toFloat64(result) != 3.0 {
		t.Errorf("Expected 3, got %v", result)
	}
}

// toFloat64 converts numeric types to float64 for comparison
func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	default:
		return 0
	}
}

func TestInProcessEvaluator_StringConcatenation(t *testing.T) {
	eval := NewInProcessEvaluator()
	defer eval.Close()

	ctx := context.Background()
	req := Request{
		Expression: "inputs.prefix + '_' + inputs.suffix",
		Inputs: map[string]interface{}{
			"prefix": "hello",
			"suffix": "world",
		},
	}

	result, err := eval.Evaluate(ctx, req)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if result != "hello_world" {
		t.Errorf("Expected 'hello_world', got %v", result)
	}
}

func TestInProcessEvaluator_SelfReference(t *testing.T) {
	eval := NewInProcessEvaluator()
	defer eval.Close()

	ctx := context.Background()
	req := Request{
		Expression: "self.basename",
		Self: map[string]interface{}{
			"class":    "File",
			"path":     "/data/reads.fastq",
			"basename": "reads.fastq",
		},
	}

	result, err := eval.Evaluate(ctx, req)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if result != "reads.fastq" {
		t.Errorf("Expected 'reads.fastq', got %v", result)
	}
}

func TestInProcessEvaluator_RuntimeReference(t *testing.T) {
	eval := NewInProcessEvaluator()
	defer eval.Close()

	ctx := context.Background()
	req := Request{
		Expression: "runtime.cores * 2",
		Runtime: map[string]interface{}{
			"cores":  4.0,
			"ram":    8192.0,
			"tmpdir": "/tmp",
			"outdir": "/output",
		},
	}

	result, err := eval.Evaluate(ctx, req)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if toFloat64(result) != 8.0 {
		t.Errorf("Expected 8, got %v", result)
	}
}

func TestInProcessEvaluator_ArrayAccess(t *testing.T) {
	eval := NewInProcessEvaluator()
	defer eval.Close()

	ctx := context.Background()
	req := Request{
		Expression: "inputs.files[0].basename",
		Inputs: map[string]interface{}{
			"files": []interface{}{
				map[string]interface{}{"basename": "first.txt"},
				map[string]interface{}{"basename": "second.txt"},
			},
		},
	}

	result, err := eval.Evaluate(ctx, req)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if result != "first.txt" {
		t.Errorf("Expected 'first.txt', got %v", result)
	}
}

func TestInProcessEvaluator_ConditionalExpression(t *testing.T) {
	eval := NewInProcessEvaluator()
	defer eval.Close()

	ctx := context.Background()
	req := Request{
		Expression: "inputs.paired ? 'PE' : 'SE'",
		Inputs: map[string]interface{}{
			"paired": true,
		},
	}

	result, err := eval.Evaluate(ctx, req)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if result != "PE" {
		t.Errorf("Expected 'PE', got %v", result)
	}
}

func TestInProcessEvaluator_Timeout(t *testing.T) {
	eval := NewInProcessEvaluator()
	defer eval.Close()

	// Very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	req := Request{
		Expression: "while(true) {}", // Infinite loop
	}

	_, err := eval.Evaluate(ctx, req)
	if err != ErrTimeout {
		t.Errorf("Expected timeout error, got %v", err)
	}
}

func TestInProcessEvaluator_SyntaxError(t *testing.T) {
	eval := NewInProcessEvaluator()
	defer eval.Close()

	ctx := context.Background()
	req := Request{
		Expression: "inputs.a +++ inputs.b", // Invalid syntax
	}

	_, err := eval.Evaluate(ctx, req)
	if err == nil {
		t.Error("Expected error for invalid syntax")
	}
}

func TestInProcessEvaluator_UndefinedVariable(t *testing.T) {
	eval := NewInProcessEvaluator()
	defer eval.Close()

	ctx := context.Background()
	req := Request{
		Expression: "undefined_var",
		Inputs:     map[string]interface{}{},
	}

	_, err := eval.Evaluate(ctx, req)
	if err == nil {
		t.Error("Expected error for undefined variable")
	}
}

func TestInProcessEvaluator_NullHandling(t *testing.T) {
	eval := NewInProcessEvaluator()
	defer eval.Close()

	ctx := context.Background()
	req := Request{
		Expression: "inputs.optional === null ? 'default' : inputs.optional",
		Inputs: map[string]interface{}{
			"optional": nil,
		},
	}

	result, err := eval.Evaluate(ctx, req)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if result != "default" {
		t.Errorf("Expected 'default', got %v", result)
	}
}

func TestInProcessEvaluator_MathFunctions(t *testing.T) {
	eval := NewInProcessEvaluator()
	defer eval.Close()

	tests := []struct {
		expr     string
		expected float64
	}{
		{"Math.abs(-5)", 5},
		{"Math.floor(3.7)", 3},
		{"Math.ceil(3.2)", 4},
		{"Math.round(3.5)", 4},
		{"Math.min(1, 2, 3)", 1},
		{"Math.max(1, 2, 3)", 3},
		{"Math.pow(2, 3)", 8},
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			ctx := context.Background()
			req := Request{Expression: tc.expr}

			result, err := eval.Evaluate(ctx, req)
			if err != nil {
				t.Fatalf("Evaluate failed: %v", err)
			}

			if toFloat64(result) != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestInProcessEvaluator_JSONFunctions(t *testing.T) {
	eval := NewInProcessEvaluator()
	defer eval.Close()

	ctx := context.Background()
	req := Request{
		Expression: `JSON.stringify({"a": 1})`,
	}

	result, err := eval.Evaluate(ctx, req)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if result != `{"a":1}` {
		t.Errorf("Expected '{\"a\":1}', got %v", result)
	}
}

func TestInProcessEvaluator_CWLExpressions(t *testing.T) {
	eval := NewInProcessEvaluator()
	defer eval.Close()

	// Test real CWL-style expressions
	tests := []struct {
		name      string
		expr      string
		inputs    map[string]interface{}
		self      interface{}
		runtime   map[string]interface{}
		expected  interface{}
		isNumeric bool
	}{
		{
			name: "output filename from input",
			expr: "inputs.input_file.nameroot + '.bam'",
			inputs: map[string]interface{}{
				"input_file": map[string]interface{}{
					"class":    "File",
					"nameroot": "sample1",
				},
			},
			expected: "sample1.bam",
		},
		{
			name: "thread count",
			expr: "runtime.cores",
			runtime: map[string]interface{}{
				"cores": 8.0,
			},
			expected:  8.0,
			isNumeric: true,
		},
		{
			name: "array length",
			expr: "inputs.files.length",
			inputs: map[string]interface{}{
				"files": []interface{}{"a", "b", "c"},
			},
			expected:  3.0,
			isNumeric: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			req := Request{
				Expression: tc.expr,
				Inputs:     tc.inputs,
				Self:       tc.self,
				Runtime:    tc.runtime,
			}

			result, err := eval.Evaluate(ctx, req)
			if err != nil {
				t.Fatalf("Evaluate failed: %v", err)
			}

			if tc.isNumeric {
				if toFloat64(result) != tc.expected.(float64) {
					t.Errorf("Expected %v, got %v", tc.expected, result)
				}
			} else if result != tc.expected {
				t.Errorf("Expected %v (%T), got %v (%T)", tc.expected, tc.expected, result, result)
			}
		})
	}
}

func TestDefaultConfigs(t *testing.T) {
	// Verify default configs are sensible
	procCfg := DefaultConfig()
	if procCfg.WorkerCount < 1 {
		t.Error("Default worker count should be at least 1")
	}
	if procCfg.Timeout < time.Second {
		t.Error("Default timeout should be at least 1 second")
	}
	if procCfg.MaxMemoryMB < 10 {
		t.Error("Default memory limit should be at least 10MB")
	}

	contCfg := DefaultContainerConfig()
	if contCfg.Image == "" {
		t.Error("Default container image should be set")
	}
	if !contCfg.NetworkDisabled {
		t.Error("Network should be disabled by default")
	}
	if !contCfg.ReadOnlyRootfs {
		t.Error("Rootfs should be read-only by default")
	}
}
