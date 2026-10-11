package httpcall

import "strings"

const maxPathTemplateLength = 512

// PathTemplate returns the path of an unresolved URL template, with each
// {{ expression }} written as {expression}. The scheme and host, or a leading
// variable that holds them, the query string and the fragment are dropped.
//
//	{{baseUrl}}/users/{{ userId }}?expand=true  ->  /users/{userId}
//	https://api.example.com/orders/{{id}}       ->  /orders/{id}
//	https://api.example.com                     ->  /
func PathTemplate(template string) string {
	path := strings.TrimSpace(template)
	path = cutOutsideTemplates(path, '?')
	path = cutOutsideTemplates(path, '#')

	switch {
	case strings.HasPrefix(path, "{{"):
		if end := strings.Index(path, "}}"); end >= 0 {
			path = path[end+len("}}"):]
		}
	case strings.Contains(path, "://"):
		afterScheme := path[strings.Index(path, "://")+len("://"):]
		path = ""
		if slash := strings.IndexByte(afterScheme, '/'); slash >= 0 {
			path = afterScheme[slash:]
		}
	}
	if path == "" {
		path = "/"
	}

	path = normalizeExpressions(path)
	if len(path) > maxPathTemplateLength {
		path = path[:maxPathTemplateLength]
	}
	return path
}

// cutOutsideTemplates drops everything from the first sep that is not inside
// a {{ }} expression.
func cutOutsideTemplates(value string, sep byte) string {
	depth := 0
	for i := 0; i < len(value); i++ {
		switch {
		case strings.HasPrefix(value[i:], "{{"):
			depth++
			i++
		case strings.HasPrefix(value[i:], "}}") && depth > 0:
			depth--
			i++
		case value[i] == sep && depth == 0:
			return value[:i]
		}
	}
	return value
}

func normalizeExpressions(path string) string {
	var out strings.Builder
	for {
		start := strings.Index(path, "{{")
		if start < 0 {
			out.WriteString(path)
			return out.String()
		}
		end := strings.Index(path[start:], "}}")
		if end < 0 {
			out.WriteString(path)
			return out.String()
		}
		out.WriteString(path[:start])
		out.WriteString("{")
		out.WriteString(strings.TrimSpace(path[start+len("{{") : start+end]))
		out.WriteString("}")
		path = path[start+end+len("}}"):]
	}
}
