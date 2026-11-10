package flowdsl

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/nanostack-dev/echopoint-runner/pkg/flowdsl/wire"

	"gopkg.in/yaml.v3"
)

// The operator list matches `flow node expect`. between is the one addition:
// the API has it, and the expect command does not.
var expectOperators = []string{
	"equals", "not_equals", "contains", "not_contains", "greater_than", "less_than",
	"greater_than_or_equal", "less_than_or_equal", "empty", "not_empty", "starts_with",
	"ends_with", "regex", "between",
}

var valuelessOperators = []string{"empty", "not_empty"}

func parseExpectNode(n *yaml.Node) ([]wire.CompositeAssertion, error) {
	if n == nil {
		return nil, nil
	}
	switch n.Kind {
	case yaml.ScalarNode:
		one, err := parseExpectLine(n.Value)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n.Line, err)
		}
		return []wire.CompositeAssertion{one}, nil
	case yaml.SequenceNode:
		out := make([]wire.CompositeAssertion, 0, len(n.Content))
		for _, item := range n.Content {
			one, err := parseExpectItem(item)
			if err != nil {
				return nil, err
			}
			out = append(out, one)
		}
		return out, nil
	case yaml.MappingNode:
		one, err := parseStatusMapping(n)
		if err != nil {
			return nil, err
		}
		return []wire.CompositeAssertion{one}, nil
	default:
		return nil, fmt.Errorf("line %d: expected a check", n.Line)
	}
}

func parseExpectItem(n *yaml.Node) (wire.CompositeAssertion, error) {
	if text, ok := scalar(n); ok {
		one, err := parseExpectLine(text)
		if err != nil {
			return wire.CompositeAssertion{}, fmt.Errorf("line %d: %w", n.Line, err)
		}
		return one, nil
	}
	if n.Kind == yaml.MappingNode {
		return parseStatusMapping(n)
	}
	return wire.CompositeAssertion{}, fmt.Errorf("line %d: expected a check", n.Line)
}

// parseStatusMapping reads `status: 200`. The value is kept as text: that is
// the wire form `flow node expect` and build_flow already send.
func parseStatusMapping(n *yaml.Node) (wire.CompositeAssertion, error) {
	m, err := mappingOf(n)
	if err != nil {
		return wire.CompositeAssertion{}, err
	}
	status, ok := m.take("status")
	if !ok || len(m.leftover()) > 0 {
		return wire.CompositeAssertion{}, fmt.Errorf(
			"line %d: a check mapping only accepts status, or write \"$.path equals value\"", n.Line)
	}
	text, ok := scalar(status)
	if !ok {
		return wire.CompositeAssertion{}, fmt.Errorf("line %d: status needs a value", status.Line)
	}
	if _, err := strconv.Atoi(strings.TrimSpace(text)); err != nil {
		return wire.CompositeAssertion{}, fmt.Errorf("line %d: status %q is not a number", status.Line, text)
	}
	return statusEquals(text), nil
}

func parseExpectLine(line string) (wire.CompositeAssertion, error) {
	// Trim the left side and a trailing newline only. A value may end with a
	// space, and TrimSpace would delete it.
	text := strings.TrimLeft(line, " \t")
	text = strings.TrimRight(text, "\r\n")
	if strings.TrimSpace(text) == "" {
		return wire.CompositeAssertion{}, fmt.Errorf("empty check")
	}
	if code, ok := strings.CutPrefix(text, "status:"); ok {
		code = strings.TrimSpace(code)
		if _, err := strconv.Atoi(code); err != nil {
			return wire.CompositeAssertion{}, fmt.Errorf("status %q is not a number", code)
		}
		return statusEquals(code), nil
	}
	return parseCheck(text)
}

func statusEquals(code string) wire.CompositeAssertion {
	return wire.CompositeAssertion{
		ExtractorType: wire.ExtractorTypeStatusCode,
		ExtractorData: map[string]any{},
		OperatorType:  wire.OperatorTypeEquals,
		OperatorData:  map[string]any{"value": code},
	}
}

func parseCheck(match string) (wire.CompositeAssertion, error) {
	target, operator, value, hasValue, err := splitCheck(match)
	if err != nil {
		return wire.CompositeAssertion{}, err
	}
	if operator == "" {
		return wire.CompositeAssertion{}, fmt.Errorf(
			"%q: expected \"<target> <operator> [value]\", such as \"$.id not_empty\"", match)
	}
	if !slices.Contains(expectOperators, operator) {
		return wire.CompositeAssertion{}, fmt.Errorf("%q: unknown operator %q", match, operator)
	}
	assertion := wire.CompositeAssertion{
		OperatorType:  wire.OperatorType(operator),
		OperatorData:  map[string]any{},
		ExtractorData: map[string]any{},
	}
	switch {
	case strings.HasPrefix(target, "$"):
		assertion.ExtractorType = wire.ExtractorTypeJsonPath
		assertion.ExtractorData["path"] = target
	case strings.HasPrefix(target, "header:") && len(target) > len("header:"):
		assertion.ExtractorType = wire.ExtractorTypeHeader
		assertion.ExtractorData["header_name"] = strings.TrimPrefix(target, "header:")
	case strings.HasPrefix(target, "query:") && len(target) > len("query:"):
		assertion.ExtractorType = wire.ExtractorTypeQueryParam
		assertion.ExtractorData["param_name"] = strings.TrimPrefix(target, "query:")
	case target == string(wire.ExtractorTypeBody):
		assertion.ExtractorType = wire.ExtractorTypeBody
	case target == statusWord:
		assertion.ExtractorType = wire.ExtractorTypeStatusCode
	case strings.HasPrefix(target, htmlWord+":") && len(target) > len(htmlWord+":"):
		assertion.ExtractorType = wire.ExtractorTypeHtml
		selector, attribute := splitHTMLTarget(strings.TrimPrefix(target, htmlWord+":"))
		assertion.ExtractorData["selector"] = selector
		if attribute != "" {
			assertion.ExtractorData["attribute"] = attribute
		}
	case strings.HasPrefix(target, regexWord+":") && len(target) > len(regexWord+":"):
		pattern := strings.TrimPrefix(target, regexWord+":")
		if _, err := regexp.Compile(pattern); err != nil {
			return wire.CompositeAssertion{}, fmt.Errorf("%q: %w", match, err)
		}
		assertion.ExtractorType = wire.ExtractorTypeRegex
		assertion.ExtractorData["pattern"] = pattern
	default:
		return wire.CompositeAssertion{}, fmt.Errorf(
			"%q: target must be a JSONPath ($.id), header:<name>, query:<name>, html:<selector>[@attribute], "+
				"regex:<pattern>, body, or status: <code>", match)
	}
	if operator == string(wire.OperatorTypeBetween) {
		return finishBetween(assertion, match, value, hasValue)
	}
	valueless := slices.Contains(valuelessOperators, operator)
	switch {
	case valueless && hasValue:
		return wire.CompositeAssertion{}, fmt.Errorf("%q: %s takes no value", match, operator)
	case !valueless && !hasValue:
		return wire.CompositeAssertion{}, fmt.Errorf("%q: %s needs a value", match, operator)
	case hasValue:
		assertion.OperatorData["value"] = value
	}
	return assertion, nil
}

// splitHTMLTarget reads "selector@attribute". The attribute is the text after
// the last @ that follows every ], so an @ inside an attribute selector such
// as a[href*="@"] stays part of the selector.
func splitHTMLTarget(text string) (string, string) {
	at := strings.LastIndex(text, "@")
	if at <= 0 || at < strings.LastIndex(text, "]") || !validAttribute(text[at+1:]) {
		return text, ""
	}
	return text[:at], text[at+1:]
}

func validAttribute(name string) bool {
	if name == "" {
		return false
	}
	return strings.IndexFunc(name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("-_:", r)
	}) < 0
}

// splitCheck reads a target, then an operator, then the rest of the line.
// A target that contains a space is a quoted JSONPath, such as a filter.
// The value is not trimmed: it may end with a space.
func splitCheck(match string) (string, string, string, bool, error) {
	target, text, err := takeTarget(strings.TrimLeft(match, " \t"))
	if err != nil {
		return "", "", "", false, fmt.Errorf("%q: %w", match, err)
	}
	if text == "" {
		return target, "", "", false, nil
	}
	operator, value, hasValue := strings.Cut(text, " ")
	if hasValue && strings.TrimSpace(value) == "" && slices.Contains(valuelessOperators, operator) {
		return target, operator, "", false, nil
	}
	return target, operator, value, hasValue, nil
}

func takeTarget(text string) (string, string, error) {
	if !strings.HasPrefix(text, `"`) {
		head, rest, found := strings.Cut(text, " ")
		if !found {
			return head, "", nil
		}
		return head, rest, nil
	}
	quoted, err := strconv.QuotedPrefix(text)
	if err != nil {
		return "", "", err
	}
	target, err := strconv.Unquote(quoted)
	if err != nil {
		return "", "", err
	}
	return target, strings.TrimLeft(text[len(quoted):], " \t"), nil
}

func finishBetween(
	assertion wire.CompositeAssertion,
	match, value string,
	hasValue bool,
) (wire.CompositeAssertion, error) {
	if !hasValue {
		return wire.CompositeAssertion{}, fmt.Errorf("%q: between needs two values, such as \"$.n between 1 5\"", match)
	}
	fields := strings.Fields(value)
	if len(fields) != 2 {
		return wire.CompositeAssertion{}, fmt.Errorf("%q: between needs two values, such as \"$.n between 1 5\"", match)
	}
	// The runner reads operator_data.value as a [min, max] pair, not min/max keys.
	assertion.OperatorData = map[string]any{"value": []any{boundToken(fields[0]), boundToken(fields[1])}}
	return assertion, nil
}

// boundToken keeps a numeric bound a number so the pair matches the stored
// form. A template such as {{min}} stays text.
func boundToken(text string) any {
	if value, err := strconv.Atoi(text); err == nil {
		return value
	}
	if value, err := strconv.ParseFloat(text, 64); err == nil {
		return value
	}
	return text
}
