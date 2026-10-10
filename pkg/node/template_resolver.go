package node

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
	"github.com/rs/zerolog/log"
)

var rawVariablePattern = regexp.MustCompile(`^\{\{\{\s*([^{}]+?)\s*\}\}\}$`)
var stringVariablePattern = regexp.MustCompile(`\{\{([^}]+)\}\}`)
var singleVariablePattern = regexp.MustCompile(`^\{\{\s*([^{}]+?)\s*\}\}$`)

// TemplateResolver handles resolution of {{variableName}} templates in strings and objects.
type TemplateResolver struct {
	variables map[string]any
	dynamic   spi.DynamicResolver
}

// NewTemplateResolver creates a new template resolver with the given variables.
func NewTemplateResolver(variables map[string]any) *TemplateResolver {
	return &TemplateResolver{
		variables: variables,
	}
}

// NewTemplateResolverWithDynamics creates a resolver that also resolves
// {{$name}} dynamic variables via the given resolver (may be nil).
func NewTemplateResolverWithDynamics(
	variables map[string]any, dynamic spi.DynamicResolver,
) *TemplateResolver {
	return &TemplateResolver{
		variables: variables,
		dynamic:   dynamic,
	}
}

// resolveDynamic resolves a single "$name:arg1:arg2" reference. Returns the
// generated value and true when handled; false when it is not a dynamic var.
func (tr *TemplateResolver) resolveDynamic(varName string) (string, bool) {
	if tr.dynamic == nil || !strings.HasPrefix(varName, "$") {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(varName, "$"), ":")
	value, err := tr.dynamic.Resolve(parts[0], parts[1:])
	if err != nil {
		log.Error().Err(err).Str("variable", varName).Msg("failed to resolve dynamic variable")
		return "", false
	}
	return value, true
}

// Resolve recursively resolves all {{variableName}} templates in the given value
// Supports strings, maps, slices, and nested structures.
func (tr *TemplateResolver) Resolve(value any) (any, error) {
	switch v := value.(type) {
	case string:
		if resolved, ok := tr.resolveRawVariable(v); ok {
			return resolved, nil
		}
		return tr.resolveString(v), nil
	case map[string]any:
		return tr.resolveMap(v)
	case []any:
		return tr.resolveSlice(v)
	case json.RawMessage:
		// Handle JSON raw messages
		log.Debug().
			Msg("Resolving JSON raw message")
		var unmarshalled any
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
	result := stringVariablePattern.ReplaceAllStringFunc(
		s, func(match string) string {
			// Extract variable name from {{varName}}
			varName := strings.TrimSpace(match[2 : len(match)-2])
			if val, handled := tr.resolveDynamic(varName); handled {
				return val
			}
			if val, exists := tr.lookup(varName); exists {
				return fmt.Sprintf("%v", val)
			}
			return match
		},
	)

	return result
}

func (tr *TemplateResolver) resolveRawVariable(value string) (any, bool) {
	match := rawVariablePattern.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 1+1 {
		return nil, false
	}

	varName := strings.TrimSpace(match[1])
	if val, handled := tr.resolveDynamic(varName); handled {
		return val, true
	}
	resolved, exists := tr.lookup(varName)
	if !exists {
		return value, true
	}

	return resolved, true
}

// lookup returns the value named by varName, walking into it when only a
// dotted prefix of varName is a variable.
func (tr *TemplateResolver) lookup(varName string) (any, bool) {
	return LookupPath(varName, func(key string) (any, bool) {
		val, exists := tr.variables[key]
		return val, exists
	})
}

// LookupPath resolves a dotted reference against get. An exact key wins.
// Otherwise the longest dotted prefix that get knows is taken, because keys
// themselves contain dots (list_accounts.accounts), and the remaining segments
// walk into its value: object fields by name, array elements by zero-based
// index. Any missing segment reports false.
func LookupPath(ref string, get func(key string) (any, bool)) (any, bool) {
	if val, exists := get(ref); exists {
		return val, true
	}
	for end := strings.LastIndex(ref, "."); end > 0; end = strings.LastIndex(ref[:end], ".") {
		if root, exists := get(ref[:end]); exists {
			return walkPath(root, strings.Split(ref[end+1:], "."))
		}
	}
	return nil, false
}

func walkPath(value any, segments []string) (any, bool) {
	for _, segment := range segments {
		switch container := value.(type) {
		case map[string]any:
			next, exists := container[segment]
			if !exists {
				return nil, false
			}
			value = next
		case []any:
			index, ok := sliceIndex(segment, len(container))
			if !ok {
				return nil, false
			}
			value = container[index]
		case []map[string]any:
			index, ok := sliceIndex(segment, len(container))
			if !ok {
				return nil, false
			}
			value = container[index]
		default:
			return nil, false
		}
	}
	return value, true
}

func sliceIndex(segment string, length int) (int, bool) {
	index, err := strconv.Atoi(segment)
	return index, err == nil && index >= 0 && index < length
}

// resolveMap recursively resolves templates in all map values.
func (tr *TemplateResolver) resolveMap(m map[string]any) (map[string]any, error) {
	log.Debug().
		Int("mapSize", len(m)).
		Msg("Resolving map templates")

	resolved := make(map[string]any)

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
func (tr *TemplateResolver) resolveSlice(s []any) ([]any, error) {
	log.Debug().
		Int("sliceSize", len(s)).
		Msg("Resolving slice templates")

	resolved := make([]any, len(s))

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
