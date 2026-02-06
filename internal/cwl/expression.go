package cwl

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/dop251/goja"
)

// ExpressionEvaluator evaluates CWL expressions.
type ExpressionEvaluator struct {
	runtime    *goja.Runtime
	expressionLib []string
	inputs     map[string]interface{}
	self       interface{}
	runtimeCtx map[string]interface{}
}

// NewExpressionEvaluator creates a new expression evaluator.
func NewExpressionEvaluator() *ExpressionEvaluator {
	return &ExpressionEvaluator{
		runtime:    goja.New(),
		runtimeCtx: make(map[string]interface{}),
	}
}

// SetInputs sets the inputs context for expression evaluation.
func (ee *ExpressionEvaluator) SetInputs(inputs map[string]interface{}) {
	ee.inputs = inputs
}

// SetSelf sets the self context for expression evaluation.
func (ee *ExpressionEvaluator) SetSelf(self interface{}) {
	ee.self = self
}

// SetRuntime sets the runtime context for expression evaluation.
func (ee *ExpressionEvaluator) SetRuntime(ctx map[string]interface{}) {
	ee.runtimeCtx = ctx
}

// SetExpressionLib sets the expression library functions.
func (ee *ExpressionEvaluator) SetExpressionLib(lib []string) {
	ee.expressionLib = lib
}

// Evaluate evaluates a CWL expression and returns the result.
func (ee *ExpressionEvaluator) Evaluate(expr string) (interface{}, error) {
	// Check expression type
	if strings.HasPrefix(expr, "$(") && strings.HasSuffix(expr, ")") {
		// Parameter reference or simple expression
		inner := expr[2 : len(expr)-1]
		return ee.evaluateParameterReference(inner)
	}

	if strings.HasPrefix(expr, "${") && strings.HasSuffix(expr, "}") {
		// JavaScript expression
		inner := expr[2 : len(expr)-1]
		return ee.evaluateJavaScript(inner)
	}

	// Check for embedded expressions in string
	if containsExpression(expr) {
		return ee.evaluateStringWithExpressions(expr)
	}

	// Literal value
	return expr, nil
}

// evaluateParameterReference evaluates a simple parameter reference.
func (ee *ExpressionEvaluator) evaluateParameterReference(ref string) (interface{}, error) {
	// Set up the JavaScript runtime with contexts
	ee.setupContext()

	// Evaluate as JavaScript expression
	result, err := ee.runtime.RunString(ref)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate reference '%s': %w", ref, err)
	}

	return result.Export(), nil
}

// evaluateJavaScript evaluates a JavaScript expression.
func (ee *ExpressionEvaluator) evaluateJavaScript(code string) (interface{}, error) {
	// Set up the JavaScript runtime with contexts
	ee.setupContext()

	// Load expression library
	for _, lib := range ee.expressionLib {
		if _, err := ee.runtime.RunString(lib); err != nil {
			return nil, fmt.Errorf("failed to load expression library: %w", err)
		}
	}

	// Wrap code to return result
	wrappedCode := fmt.Sprintf("(function() { %s })()", code)

	result, err := ee.runtime.RunString(wrappedCode)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate JavaScript: %w", err)
	}

	return result.Export(), nil
}

// evaluateStringWithExpressions evaluates a string containing embedded expressions.
func (ee *ExpressionEvaluator) evaluateStringWithExpressions(s string) (interface{}, error) {
	// Find all $(expr) patterns
	paramPattern := regexp.MustCompile(`\$\([^)]+\)`)

	result := paramPattern.ReplaceAllStringFunc(s, func(match string) string {
		inner := match[2 : len(match)-1]
		val, err := ee.evaluateParameterReference(inner)
		if err != nil {
			return match // Keep original on error
		}
		return fmt.Sprintf("%v", val)
	})

	return result, nil
}

// setupContext sets up the JavaScript runtime with CWL contexts.
func (ee *ExpressionEvaluator) setupContext() {
	// Set up inputs
	if ee.inputs != nil {
		ee.runtime.Set("inputs", ee.inputs)
	} else {
		ee.runtime.Set("inputs", make(map[string]interface{}))
	}

	// Set up self
	if ee.self != nil {
		ee.runtime.Set("self", ee.self)
	} else {
		ee.runtime.Set("self", nil)
	}

	// Set up runtime
	if ee.runtimeCtx != nil {
		ee.runtime.Set("runtime", ee.runtimeCtx)
	} else {
		ee.runtime.Set("runtime", map[string]interface{}{
			"cores":      1,
			"ram":        4096,
			"tmpdirSize": 1024,
			"outdirSize": 1024,
			"tmpdir":     "/tmp",
			"outdir":     "/output",
		})
	}
}

// containsExpression checks if a string contains CWL expressions.
func containsExpression(s string) bool {
	return strings.Contains(s, "$(") || strings.Contains(s, "${")
}

// IsExpression checks if a string is a CWL expression.
func IsExpression(s string) bool {
	if strings.HasPrefix(s, "$(") && strings.HasSuffix(s, ")") {
		return true
	}
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		return true
	}
	return false
}

// EvaluateGlob evaluates a glob expression which can be a string or expression.
func (ee *ExpressionEvaluator) EvaluateGlob(glob interface{}) ([]string, error) {
	switch v := glob.(type) {
	case string:
		if IsExpression(v) {
			result, err := ee.Evaluate(v)
			if err != nil {
				return nil, err
			}
			return toStringSlice(result), nil
		}
		return []string{v}, nil
	case []interface{}:
		var patterns []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				if IsExpression(s) {
					result, err := ee.Evaluate(s)
					if err != nil {
						return nil, err
					}
					patterns = append(patterns, toStringSlice(result)...)
				} else {
					patterns = append(patterns, s)
				}
			}
		}
		return patterns, nil
	default:
		return nil, fmt.Errorf("unsupported glob type: %T", glob)
	}
}

// toStringSlice converts an interface to a string slice.
func toStringSlice(v interface{}) []string {
	switch val := v.(type) {
	case string:
		return []string{val}
	case []interface{}:
		var result []string
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return val
	default:
		return []string{fmt.Sprintf("%v", v)}
	}
}

// EvaluateSecondaryFiles evaluates secondary file patterns.
func (ee *ExpressionEvaluator) EvaluateSecondaryFiles(specs []SecondaryFileSpec, primaryPath string) ([]string, error) {
	var results []string

	for _, spec := range specs {
		pattern := spec.Pattern

		if IsExpression(pattern) {
			// Set self to the primary file for evaluation
			oldSelf := ee.self
			ee.self = map[string]interface{}{
				"class":    TypeFile,
				"path":     primaryPath,
				"basename": getBasename(primaryPath),
				"nameroot": getNameroot(primaryPath),
				"nameext":  getNameext(primaryPath),
			}

			result, err := ee.Evaluate(pattern)
			ee.self = oldSelf

			if err != nil {
				return nil, err
			}

			for _, p := range toStringSlice(result) {
				results = append(results, p)
			}
		} else {
			// Handle caret notation (^) for replacing extensions
			resolved := resolveSecondaryFilePattern(primaryPath, pattern)
			results = append(results, resolved)
		}
	}

	return results, nil
}

// resolveSecondaryFilePattern resolves a secondary file pattern.
func resolveSecondaryFilePattern(primaryPath, pattern string) string {
	if strings.HasPrefix(pattern, "^") {
		// Count leading carets
		caretCount := 0
		for _, c := range pattern {
			if c == '^' {
				caretCount++
			} else {
				break
			}
		}

		suffix := pattern[caretCount:]
		base := primaryPath

		// Remove extensions
		for i := 0; i < caretCount; i++ {
			ext := getNameext(base)
			if ext != "" {
				base = base[:len(base)-len(ext)]
			}
		}

		return base + suffix
	}

	// Simple suffix
	return primaryPath + pattern
}

// getBasename extracts the basename from a path.
func getBasename(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx >= 0 {
		return path[idx+1:]
	}
	return path
}

// getNameroot extracts the nameroot (filename without extension) from a path.
func getNameroot(path string) string {
	basename := getBasename(path)
	idx := strings.LastIndex(basename, ".")
	if idx > 0 {
		return basename[:idx]
	}
	return basename
}

// getNameext extracts the name extension from a path.
func getNameext(path string) string {
	basename := getBasename(path)
	idx := strings.LastIndex(basename, ".")
	if idx > 0 {
		return basename[idx:]
	}
	return ""
}

// EvaluateCondition evaluates a CWL "when" condition.
func (ee *ExpressionEvaluator) EvaluateCondition(condition string, inputs map[string]interface{}) (bool, error) {
	ee.SetInputs(inputs)

	result, err := ee.Evaluate(condition)
	if err != nil {
		return false, err
	}

	switch v := result.(type) {
	case bool:
		return v, nil
	case nil:
		return false, nil
	default:
		// Truthy evaluation
		return result != nil && result != false && result != 0 && result != "", nil
	}
}
