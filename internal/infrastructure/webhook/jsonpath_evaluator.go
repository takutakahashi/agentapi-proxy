package webhook

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/oliveagle/jsonpath"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// JSONPathEvaluator evaluates JSONPath conditions against JSON payloads
type JSONPathEvaluator struct{}

// NewJSONPathEvaluator creates a new JSONPathEvaluator
func NewJSONPathEvaluator() *JSONPathEvaluator {
	return &JSONPathEvaluator{}
}

// Evaluate evaluates all JSONPath conditions against a payload
// Returns true if all conditions match, false otherwise
func (e *JSONPathEvaluator) Evaluate(payload map[string]interface{}, conditions []entities.WebhookJSONPathCondition) (bool, error) {
	if len(conditions) == 0 {
		return true, nil
	}

	for _, cond := range conditions {
		matched, err := e.evaluateCondition(payload, cond)
		if err != nil {
			return false, fmt.Errorf("failed to evaluate condition for path %s: %w", cond.Path(), err)
		}
		if !matched {
			return false, nil
		}
	}

	return true, nil
}

// evaluateCondition evaluates a single JSONPath condition
func (e *JSONPathEvaluator) evaluateCondition(payload map[string]interface{}, cond entities.WebhookJSONPathCondition) (bool, error) {
	// Extract value using JSONPath
	value, err := jsonpath.JsonPathLookup(payload, cond.Path())

	// Handle "exists" operator specially
	if string(cond.Operator()) == "exists" {
		expectedExists, ok := cond.Value().(bool)
		if !ok {
			return false, fmt.Errorf("exists operator requires boolean value")
		}

		exists := (err == nil)
		return exists == expectedExists, nil
	}

	// For other operators, path must exist and have a value
	if err != nil {
		return false, nil // Path doesn't exist
	}

	// Evaluate based on operator
	switch string(cond.Operator()) {
	case "eq":
		return e.evaluateEquals(value, cond.Value())
	case "ne":
		result, err := e.evaluateEquals(value, cond.Value())
		return !result, err
	case "contains":
		return e.evaluateContains(value, cond.Value())
	case "matches":
		return e.evaluateMatches(value, cond.Value())
	case "in":
		return e.evaluateIn(value, cond.Value())
	default:
		return false, fmt.Errorf("unsupported operator: %s", string(cond.Operator()))
	}
}

// evaluateEquals checks if value equals expected
func (e *JSONPathEvaluator) evaluateEquals(value, expected interface{}) (bool, error) {
	// Use reflect.DeepEqual for deep comparison
	return reflect.DeepEqual(value, expected), nil
}

// evaluateContains checks if value contains expected
// Works for strings (substring) and arrays/slices (element)
func (e *JSONPathEvaluator) evaluateContains(value, expected interface{}) (bool, error) {
	// String contains
	if str, ok := value.(string); ok {
		expectedStr, ok := expected.(string)
		if !ok {
			return false, fmt.Errorf("contains operator with string value requires string expected value")
		}
		return strings.Contains(str, expectedStr), nil
	}

	// Array/slice contains
	val := reflect.ValueOf(value)
	if val.Kind() == reflect.Slice || val.Kind() == reflect.Array {
		for i := 0; i < val.Len(); i++ {
			if reflect.DeepEqual(val.Index(i).Interface(), expected) {
				return true, nil
			}
		}
		return false, nil
	}

	return false, fmt.Errorf("contains operator requires string or array value")
}

// evaluateMatches checks if value matches a regular expression
func (e *JSONPathEvaluator) evaluateMatches(value, expected interface{}) (bool, error) {
	str, ok := value.(string)
	if !ok {
		return false, fmt.Errorf("matches operator requires string value")
	}

	pattern, ok := expected.(string)
	if !ok {
		return false, fmt.Errorf("matches operator requires string pattern")
	}

	matched, err := regexp.MatchString(pattern, str)
	if err != nil {
		return false, fmt.Errorf("invalid regex pattern: %w", err)
	}

	return matched, nil
}

// evaluateIn checks if value is in the expected array
func (e *JSONPathEvaluator) evaluateIn(value, expected interface{}) (bool, error) {
	// Expected must be an array/slice
	expectedVal := reflect.ValueOf(expected)
	if expectedVal.Kind() != reflect.Slice && expectedVal.Kind() != reflect.Array {
		return false, fmt.Errorf("in operator requires array expected value")
	}

	// Check if value is in the array
	for i := 0; i < expectedVal.Len(); i++ {
		if reflect.DeepEqual(value, expectedVal.Index(i).Interface()) {
			return true, nil
		}
	}

	return false, nil
}

// EvaluateAny evaluates conditions with OR logic (returns true if any condition matches)
// This is useful for scenarios where you want to match multiple possible values
func (e *JSONPathEvaluator) EvaluateAny(payload map[string]interface{}, conditions []entities.WebhookJSONPathCondition) (bool, error) {
	if len(conditions) == 0 {
		return true, nil
	}

	for _, cond := range conditions {
		matched, err := e.evaluateCondition(payload, cond)
		if err != nil {
			return false, fmt.Errorf("failed to evaluate condition for path %s: %w", cond.Path(), err)
		}
		if matched {
			return true, nil
		}
	}

	return false, nil
}
