package node

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// TemplateResolver handles resolution of {{variableName}} templates in strings and objects
type TemplateResolver struct {
	variables map[string]interface{}
}

// NewTemplateResolver creates a new template resolver with the given variables
func NewTemplateResolver(variables map[string]interface{}) *TemplateResolver {
	return &TemplateResolver{
		variables: variables,
	}
}

// Resolve recursively resolves all {{variableName}} templates in the given value
// Supports strings, maps, slices, and nested structures
func (tr *TemplateResolver) Resolve(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		return tr.resolveString(v)
	case map[string]interface{}:
		return tr.resolveMap(v)
	case []interface{}:
		return tr.resolveSlice(v)
	case json.RawMessage:
		// Handle JSON raw messages
		var unmarshalled interface{}
		if err := json.Unmarshal(v, &unmarshalled); err != nil {
			return nil, err
		}
		return tr.Resolve(unmarshalled)
	default:
		return v, nil
	}
}

// resolveString replaces all {{variableName}} patterns with their values
func (tr *TemplateResolver) resolveString(s string) (string, error) {
	// Pattern: {{variableName}}
	pattern := regexp.MustCompile(`\{\{([^}]+)\}\}`)

	result := pattern.ReplaceAllStringFunc(
		s, func(match string) string {
			// Extract variable name from {{varName}}
			varName := match[2 : len(match)-2]
			if val, exists := tr.variables[varName]; exists {
				return fmt.Sprintf("%v", val)
			}
			return match
		},
	)

	return result, nil
}

// resolveMap recursively resolves templates in all map values
func (tr *TemplateResolver) resolveMap(m map[string]interface{}) (map[string]interface{}, error) {
	resolved := make(map[string]interface{})

	for key, val := range m {
		resolvedVal, err := tr.Resolve(val)
		if err != nil {
			return nil, fmt.Errorf("error resolving value for key '%s': %w", key, err)
		}
		resolved[key] = resolvedVal
	}

	return resolved, nil
}

// resolveSlice recursively resolves templates in all slice elements
func (tr *TemplateResolver) resolveSlice(s []interface{}) ([]interface{}, error) {
	resolved := make([]interface{}, len(s))

	for i, val := range s {
		resolvedVal, err := tr.Resolve(val)
		if err != nil {
			return nil, fmt.Errorf("error resolving element at index %d: %w", i, err)
		}
		resolved[i] = resolvedVal
	}

	return resolved, nil
}

// ResolveTemplatesInRequest is a convenience function for RequestNode to resolve templates
func ResolveTemplatesInRequest(
	url string, headers map[string]string, body interface{}, inputs map[string]interface{},
) (
	resolvedURL string, resolvedHeaders map[string]string, resolvedBody interface{}, err error,
) {

	resolver := NewTemplateResolver(inputs)

	// Resolve URL
	resolvedURL, err = resolver.resolveString(url)
	if err != nil {
		return "", nil, nil, fmt.Errorf("error resolving URL: %w", err)
	}

	// Resolve headers
	resolvedHeaders = make(map[string]string)
	for key, headerVal := range headers {
		resolved, err := resolver.resolveString(headerVal)
		if err != nil {
			return "", nil, nil, fmt.Errorf("error resolving header '%s': %w", key, err)
		}
		resolvedHeaders[key] = resolved
	}

	// Resolve body
	resolvedBody, err = resolver.Resolve(body)
	if err != nil {
		return "", nil, nil, fmt.Errorf("error resolving body: %w", err)
	}

	return resolvedURL, resolvedHeaders, resolvedBody, nil
}
