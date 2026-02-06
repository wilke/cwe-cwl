package cwl

import (
	"fmt"
)

// ScatterMethod defines how to combine scattered inputs.
type ScatterMethod string

const (
	// ScatterDotProduct computes the Cartesian product of scattered inputs.
	ScatterDotProduct ScatterMethod = "dotproduct"
	// ScatterNestedCrossProduct computes nested cross product.
	ScatterNestedCrossProduct ScatterMethod = "nested_crossproduct"
	// ScatterFlatCrossProduct computes flat cross product.
	ScatterFlatCrossProduct ScatterMethod = "flat_crossproduct"
)

// ScatterConfig holds scatter configuration for a step.
type ScatterConfig struct {
	InputIDs []string
	Method   ScatterMethod
}

// ScatteredInputs represents a single combination of scattered input values.
type ScatteredInputs struct {
	Index  []int                  // Index position in each scattered array
	Values map[string]interface{} // Input ID -> value
}

// ScatterExpander expands scattered step executions.
type ScatterExpander struct {
	config  ScatterConfig
	inputs  map[string]interface{}
}

// NewScatterExpander creates a new scatter expander.
func NewScatterExpander(config ScatterConfig, inputs map[string]interface{}) *ScatterExpander {
	return &ScatterExpander{
		config: config,
		inputs: inputs,
	}
}

// ParseScatterConfig parses scatter configuration from a workflow step.
func ParseScatterConfig(step *WorkflowStep) (*ScatterConfig, error) {
	if step.Scatter == nil {
		return nil, nil
	}

	config := &ScatterConfig{
		Method: ScatterDotProduct, // Default method
	}

	// Parse scatter input IDs
	switch v := step.Scatter.(type) {
	case string:
		config.InputIDs = []string{v}
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				config.InputIDs = append(config.InputIDs, s)
			}
		}
	default:
		return nil, fmt.Errorf("unsupported scatter type: %T", v)
	}

	// Parse scatter method
	if step.ScatterMethod != "" {
		switch step.ScatterMethod {
		case "dotproduct":
			config.Method = ScatterDotProduct
		case "nested_crossproduct":
			config.Method = ScatterNestedCrossProduct
		case "flat_crossproduct":
			config.Method = ScatterFlatCrossProduct
		default:
			return nil, fmt.Errorf("unsupported scatter method: %s", step.ScatterMethod)
		}
	}

	return config, nil
}

// Expand generates all combinations of scattered inputs.
func (se *ScatterExpander) Expand() ([]ScatteredInputs, error) {
	if len(se.config.InputIDs) == 0 {
		return nil, nil
	}

	// Get array lengths for each scattered input
	var arrays [][]interface{}
	for _, inputID := range se.config.InputIDs {
		val, ok := se.inputs[inputID]
		if !ok {
			return nil, fmt.Errorf("scattered input not found: %s", inputID)
		}

		arr, ok := val.([]interface{})
		if !ok {
			return nil, fmt.Errorf("scattered input %s is not an array", inputID)
		}

		arrays = append(arrays, arr)
	}

	switch se.config.Method {
	case ScatterDotProduct:
		return se.expandDotProduct(arrays)
	case ScatterNestedCrossProduct, ScatterFlatCrossProduct:
		return se.expandCrossProduct(arrays)
	default:
		return nil, fmt.Errorf("unsupported scatter method: %s", se.config.Method)
	}
}

// expandDotProduct expands using dot product (parallel iteration).
func (se *ScatterExpander) expandDotProduct(arrays [][]interface{}) ([]ScatteredInputs, error) {
	// All arrays must have the same length for dot product
	if len(arrays) == 0 {
		return nil, nil
	}

	length := len(arrays[0])
	for i, arr := range arrays {
		if len(arr) != length {
			return nil, fmt.Errorf("scattered input %s has length %d, expected %d for dotproduct",
				se.config.InputIDs[i], len(arr), length)
		}
	}

	var results []ScatteredInputs
	for i := 0; i < length; i++ {
		si := ScatteredInputs{
			Index:  make([]int, len(arrays)),
			Values: make(map[string]interface{}),
		}

		// Copy non-scattered inputs
		for k, v := range se.inputs {
			if !se.isScattered(k) {
				si.Values[k] = v
			}
		}

		// Add scattered values at index i
		for j, inputID := range se.config.InputIDs {
			si.Index[j] = i
			si.Values[inputID] = arrays[j][i]
		}

		results = append(results, si)
	}

	return results, nil
}

// expandCrossProduct expands using cross product (all combinations).
func (se *ScatterExpander) expandCrossProduct(arrays [][]interface{}) ([]ScatteredInputs, error) {
	if len(arrays) == 0 {
		return nil, nil
	}

	// Calculate total combinations
	total := 1
	for _, arr := range arrays {
		total *= len(arr)
	}

	var results []ScatteredInputs

	// Generate all combinations
	indices := make([]int, len(arrays))
	for i := 0; i < total; i++ {
		si := ScatteredInputs{
			Index:  make([]int, len(arrays)),
			Values: make(map[string]interface{}),
		}

		// Copy non-scattered inputs
		for k, v := range se.inputs {
			if !se.isScattered(k) {
				si.Values[k] = v
			}
		}

		// Add scattered values at current indices
		for j, inputID := range se.config.InputIDs {
			si.Index[j] = indices[j]
			si.Values[inputID] = arrays[j][indices[j]]
		}

		results = append(results, si)

		// Increment indices (like counting in mixed-radix)
		se.incrementIndices(indices, arrays)
	}

	return results, nil
}

// incrementIndices increments the index array for cross product enumeration.
func (se *ScatterExpander) incrementIndices(indices []int, arrays [][]interface{}) {
	for i := len(indices) - 1; i >= 0; i-- {
		indices[i]++
		if indices[i] < len(arrays[i]) {
			return
		}
		indices[i] = 0
	}
}

// isScattered checks if an input ID is in the scatter list.
func (se *ScatterExpander) isScattered(inputID string) bool {
	for _, id := range se.config.InputIDs {
		if id == inputID {
			return true
		}
	}
	return false
}

// GatherOutputs gathers outputs from scattered step executions.
type GatherOutputs struct {
	outputs []map[string]interface{}
	method  ScatterMethod
}

// NewGatherOutputs creates a new output gatherer.
func NewGatherOutputs(method ScatterMethod) *GatherOutputs {
	return &GatherOutputs{
		method:  method,
		outputs: make([]map[string]interface{}, 0),
	}
}

// Add adds an output from a scattered execution.
func (go_ *GatherOutputs) Add(index []int, outputs map[string]interface{}) {
	go_.outputs = append(go_.outputs, outputs)
}

// Gather collects all outputs into the appropriate structure.
func (go_ *GatherOutputs) Gather(outputIDs []string) map[string]interface{} {
	result := make(map[string]interface{})

	for _, outID := range outputIDs {
		var gathered []interface{}
		for _, out := range go_.outputs {
			if val, ok := out[outID]; ok {
				gathered = append(gathered, val)
			} else {
				gathered = append(gathered, nil)
			}
		}
		result[outID] = gathered
	}

	return result
}

// IndexToString converts an index array to a string representation.
func IndexToString(index []int) string {
	if len(index) == 0 {
		return ""
	}
	s := fmt.Sprintf("%d", index[0])
	for i := 1; i < len(index); i++ {
		s += fmt.Sprintf("_%d", index[i])
	}
	return s
}

// StringToIndex parses an index string back to an array.
func StringToIndex(s string) ([]int, error) {
	if s == "" {
		return nil, nil
	}

	var result []int
	var current int
	for _, c := range s {
		if c == '_' {
			result = append(result, current)
			current = 0
		} else if c >= '0' && c <= '9' {
			current = current*10 + int(c-'0')
		} else {
			return nil, fmt.Errorf("invalid index string: %s", s)
		}
	}
	result = append(result, current)

	return result, nil
}
