package node

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/rs/zerolog/log"
)

// TemplateResolver handles resolution of {{variableName}} templates in strings and objects.
type TemplateResolver struct {
	variables map[string]interface{}
}

// NewTemplateResolver creates a new template resolver with the given variables.
func NewTemplateResolver(variables map[string]interface{}) *TemplateResolver {
	return &TemplateResolver{
		variables: variables,
	}
}

// Resolve recursively resolves all {{variableName}} templates in the given value
// Supports strings, maps, slices, and nested structures.
func (tr *TemplateResolver) Resolve(value interface{}) (interface{}, error) {
	log.Debug().
		Any("value", value).
		Msg("Resolving template")

	switch v := value.(type) {
	case string:
		resolved := tr.resolveString(v)
		log.Debug().
			Str("original", v).
			Str("resolved", resolved).
			Msg("String template resolved")
		return resolved, nil
	case map[string]interface{}:
		return tr.resolveMap(v)
	case []interface{}:
		return tr.resolveSlice(v)
	case json.RawMessage:
		// Handle JSON raw messages
		log.Debug().
			Msg("Resolving JSON raw message")
		var unmarshalled interface{}
		if err := json.Unmarshal(v, &unmarshalled); err != nil {
			log.Error().
				Err(err).
				Msg("Failed to unmarshal JSON raw message")
			return nil, err
		}
		return tr.Resolve(unmarshalled)
	default:
		return v, nil
	}
}

// resolveString replaces all {{variableName}} patterns with their values.
func (tr *TemplateResolver) resolveString(s string) string {
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

	return result
}

// resolveMap recursively resolves templates in all map values.
func (tr *TemplateResolver) resolveMap(m map[string]interface{}) (map[string]interface{}, error) {
	log.Debug().
		Int("mapSize", len(m)).
		Msg("Resolving map templates")

	resolved := make(map[string]interface{})

	for key, val := range m {
		resolvedVal, err := tr.Resolve(val)
		if err != nil {
			err = fmt.Errorf("error resolving value for key '%s': %w", key, err)
			log.Error().
				Str("key", key).
				Err(err).
				Msg("Failed to resolve map value")
			return nil, err
		}
		resolved[key] = resolvedVal
	}

	log.Debug().
		Int("mapSize", len(resolved)).
		Msg("Map templates resolved")

	return resolved, nil
}

// resolveSlice recursively resolves templates in all slice elements.
func (tr *TemplateResolver) resolveSlice(s []interface{}) ([]interface{}, error) {
	log.Debug().
		Int("sliceSize", len(s)).
		Msg("Resolving slice templates")

	resolved := make([]interface{}, len(s))

	for i, val := range s {
		resolvedVal, err := tr.Resolve(val)
		if err != nil {
			err = fmt.Errorf("error resolving element at index %d: %w", i, err)
			log.Error().
				Int("index", i).
				Err(err).
				Msg("Failed to resolve slice element")
			return nil, err
		}
		resolved[i] = resolvedVal
	}

	log.Debug().
		Int("sliceSize", len(resolved)).
		Msg("Slice templates resolved")

	return resolved, nil
}

// ResolveTemplatesInRequest is a convenience function for RequestNode to resolve templates.
func ResolveTemplatesInRequest(
	url string, headers map[string]string, body interface{}, inputs map[string]interface{},
) (string, map[string]string, interface{}, error) {
	resolver := NewTemplateResolver(inputs)

	// Resolve URL
	resolvedURL := resolver.resolveString(url)

	// Resolve headers
	resolvedHeaders := make(map[string]string)
	for key, headerVal := range headers {
		resolved := resolver.resolveString(headerVal)
		resolvedHeaders[key] = resolved
	}

	// Resolve body
	resolvedBody, err := resolver.Resolve(body)
	if err != nil {
		return "", nil, nil, fmt.Errorf("error resolving body: %w", err)
	}

	return resolvedURL, resolvedHeaders, resolvedBody, nil
}
