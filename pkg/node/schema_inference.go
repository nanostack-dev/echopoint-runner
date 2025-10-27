package node

import (
	"regexp"
	"strings"
)

// SchemaInference provides utilities to infer input and output schemas from node configurations
type SchemaInference struct{}

// ExtractTemplateVariables extracts all {{variable}} references from a string or nested structure
func (si *SchemaInference) ExtractTemplateVariables(data interface{}) []string {
	vars := make(map[string]bool)
	si.extractVariablesRecursive(data, vars)

	// Convert map to sorted slice
	result := make([]string, 0, len(vars))
	for v := range vars {
		result = append(result, v)
	}
	return result
}

// extractVariablesRecursive recursively extracts variables from nested structures
func (si *SchemaInference) extractVariablesRecursive(data interface{}, vars map[string]bool) {
	switch v := data.(type) {
	case string:
		si.extractVariablesFromString(v, vars)
	case map[string]interface{}:
		for _, val := range v {
			si.extractVariablesRecursive(val, vars)
		}
	case []interface{}:
		for _, val := range v {
			si.extractVariablesRecursive(val, vars)
		}
	}
}

// extractVariablesFromString finds all {{variable}} patterns in a string
func (si *SchemaInference) extractVariablesFromString(s string, vars map[string]bool) {
	// Match {{variable}} patterns
	re := regexp.MustCompile(`\{\{([^}]+)\}\}`)
	matches := re.FindAllStringSubmatch(s, -1)
	for _, match := range matches {
		if len(match) > 1 {
			varName := strings.TrimSpace(match[1])
			vars[varName] = true
		}
	}
}

// InferRequestNodeInputSchema infers input schema from RequestNode data
func (si *SchemaInference) InferRequestNodeInputSchema(data RequestData) []string {
	vars := make(map[string]bool)

	// Extract from URL
	si.extractVariablesRecursive(data.URL, vars)

	// Extract from Headers
	si.extractVariablesRecursive(data.Headers, vars)

	// Extract from QueryParams
	si.extractVariablesRecursive(data.QueryParams, vars)

	// Extract from Body
	si.extractVariablesRecursive(data.Body, vars)

	// Convert to sorted slice
	result := make([]string, 0, len(vars))
	for v := range vars {
		result = append(result, v)
	}
	return result
}

// InferRequestNodeOutputSchema infers output schema from Outputs
func (si *SchemaInference) InferRequestNodeOutputSchema(outputs []Output) []string {
	result := make([]string, 0, len(outputs))
	for _, output := range outputs {
		result = append(result, output.Name)
	}
	return result
}

// InferDelayNodeInputSchema infers input schema from DelayNode (typically empty or passthrough)
func (si *SchemaInference) InferDelayNodeInputSchema(data DelayData) []string {
	// DelayNode doesn't need inputs
	return []string{}
}

// InferDelayNodeOutputSchema infers output schema from DelayNode (typically empty)
func (si *SchemaInference) InferDelayNodeOutputSchema(data DelayData) []string {
	// DelayNode typically doesn't produce outputs
	return []string{}
}
