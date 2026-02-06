package cwl

import (
	"testing"
)

func TestParseScatterConfig_Single(t *testing.T) {
	step := &WorkflowStep{
		ID:      "step1",
		Scatter: "input1",
	}

	config, err := ParseScatterConfig(step)
	if err != nil {
		t.Fatalf("Failed to parse scatter config: %v", err)
	}

	if config == nil {
		t.Fatal("Expected non-nil config")
	}

	if len(config.InputIDs) != 1 {
		t.Errorf("Expected 1 input ID, got %d", len(config.InputIDs))
	}
	if config.InputIDs[0] != "input1" {
		t.Errorf("Expected input ID 'input1', got '%s'", config.InputIDs[0])
	}
	if config.Method != ScatterDotProduct {
		t.Errorf("Expected default method dotproduct, got %s", config.Method)
	}
}

func TestParseScatterConfig_Multiple(t *testing.T) {
	step := &WorkflowStep{
		ID:            "step1",
		Scatter:       []interface{}{"input1", "input2"},
		ScatterMethod: "flat_crossproduct",
	}

	config, err := ParseScatterConfig(step)
	if err != nil {
		t.Fatalf("Failed to parse scatter config: %v", err)
	}

	if len(config.InputIDs) != 2 {
		t.Errorf("Expected 2 input IDs, got %d", len(config.InputIDs))
	}
	if config.Method != ScatterFlatCrossProduct {
		t.Errorf("Expected method flat_crossproduct, got %s", config.Method)
	}
}

func TestParseScatterConfig_NoScatter(t *testing.T) {
	step := &WorkflowStep{
		ID: "step1",
	}

	config, err := ParseScatterConfig(step)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if config != nil {
		t.Error("Expected nil config for non-scattered step")
	}
}

func TestScatterExpander_DotProduct(t *testing.T) {
	config := ScatterConfig{
		InputIDs: []string{"files"},
		Method:   ScatterDotProduct,
	}

	inputs := map[string]interface{}{
		"files":  []interface{}{"file1.txt", "file2.txt", "file3.txt"},
		"param1": "static_value",
	}

	expander := NewScatterExpander(config, inputs)
	expanded, err := expander.Expand()
	if err != nil {
		t.Fatalf("Failed to expand: %v", err)
	}

	if len(expanded) != 3 {
		t.Fatalf("Expected 3 expansions, got %d", len(expanded))
	}

	// Check each expansion
	for i, exp := range expanded {
		if len(exp.Index) != 1 || exp.Index[0] != i {
			t.Errorf("Expected index [%d], got %v", i, exp.Index)
		}

		expectedFile := inputs["files"].([]interface{})[i]
		if exp.Values["files"] != expectedFile {
			t.Errorf("Expected files=%v, got %v", expectedFile, exp.Values["files"])
		}

		// Static value should be preserved
		if exp.Values["param1"] != "static_value" {
			t.Errorf("Expected param1='static_value', got %v", exp.Values["param1"])
		}
	}
}

func TestScatterExpander_DotProduct_LengthMismatch(t *testing.T) {
	config := ScatterConfig{
		InputIDs: []string{"files", "labels"},
		Method:   ScatterDotProduct,
	}

	inputs := map[string]interface{}{
		"files":  []interface{}{"file1.txt", "file2.txt", "file3.txt"},
		"labels": []interface{}{"A", "B"}, // Mismatched length
	}

	expander := NewScatterExpander(config, inputs)
	_, err := expander.Expand()

	if err == nil {
		t.Error("Expected error for mismatched lengths in dotproduct")
	}
}

func TestScatterExpander_CrossProduct(t *testing.T) {
	config := ScatterConfig{
		InputIDs: []string{"x", "y"},
		Method:   ScatterFlatCrossProduct,
	}

	inputs := map[string]interface{}{
		"x": []interface{}{"a", "b"},
		"y": []interface{}{1, 2, 3},
	}

	expander := NewScatterExpander(config, inputs)
	expanded, err := expander.Expand()
	if err != nil {
		t.Fatalf("Failed to expand: %v", err)
	}

	// 2 * 3 = 6 combinations
	if len(expanded) != 6 {
		t.Fatalf("Expected 6 expansions, got %d", len(expanded))
	}

	// Check all combinations exist
	expected := []struct {
		x string
		y int
	}{
		{"a", 1}, {"a", 2}, {"a", 3},
		{"b", 1}, {"b", 2}, {"b", 3},
	}

	for _, exp := range expanded {
		found := false
		for _, e := range expected {
			if exp.Values["x"] == e.x && exp.Values["y"] == e.y {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Unexpected combination: x=%v, y=%v", exp.Values["x"], exp.Values["y"])
		}
	}
}

func TestGatherOutputs(t *testing.T) {
	gather := NewGatherOutputs(ScatterDotProduct)

	// Add outputs from scattered executions
	gather.Add([]int{0}, map[string]interface{}{"result": "a", "count": 1})
	gather.Add([]int{1}, map[string]interface{}{"result": "b", "count": 2})
	gather.Add([]int{2}, map[string]interface{}{"result": "c", "count": 3})

	gathered := gather.Gather([]string{"result", "count"})

	results := gathered["result"].([]interface{})
	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}
	if results[0] != "a" || results[1] != "b" || results[2] != "c" {
		t.Errorf("Unexpected results: %v", results)
	}

	counts := gathered["count"].([]interface{})
	if counts[0] != 1 || counts[1] != 2 || counts[2] != 3 {
		t.Errorf("Unexpected counts: %v", counts)
	}
}

func TestIndexToString(t *testing.T) {
	testCases := []struct {
		index    []int
		expected string
	}{
		{nil, ""},
		{[]int{}, ""},
		{[]int{0}, "0"},
		{[]int{1, 2}, "1_2"},
		{[]int{0, 1, 2}, "0_1_2"},
		{[]int{10, 20, 30}, "10_20_30"},
	}

	for _, tc := range testCases {
		result := IndexToString(tc.index)
		if result != tc.expected {
			t.Errorf("IndexToString(%v) = '%s', expected '%s'", tc.index, result, tc.expected)
		}
	}
}

func TestStringToIndex(t *testing.T) {
	testCases := []struct {
		input    string
		expected []int
		hasError bool
	}{
		{"", nil, false},
		{"0", []int{0}, false},
		{"1_2", []int{1, 2}, false},
		{"0_1_2", []int{0, 1, 2}, false},
		{"10_20_30", []int{10, 20, 30}, false},
	}

	for _, tc := range testCases {
		result, err := StringToIndex(tc.input)
		if tc.hasError {
			if err == nil {
				t.Errorf("StringToIndex('%s') expected error", tc.input)
			}
			continue
		}

		if err != nil {
			t.Errorf("StringToIndex('%s') unexpected error: %v", tc.input, err)
			continue
		}

		if len(result) != len(tc.expected) {
			t.Errorf("StringToIndex('%s') = %v, expected %v", tc.input, result, tc.expected)
			continue
		}

		for i := range tc.expected {
			if result[i] != tc.expected[i] {
				t.Errorf("StringToIndex('%s')[%d] = %d, expected %d", tc.input, i, result[i], tc.expected[i])
			}
		}
	}
}
