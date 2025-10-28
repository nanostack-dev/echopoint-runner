package assertions_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-flow-engine/internal/logger"
	"github.com/stretchr/testify/assert"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/assertions"
)

func init() {
	// Enable debug logging with human-readable format for tests
	logger.SetDebugLogging()
}

func TestStringAssertion_GetType(t *testing.T) {
	assertion := assertions.StringAssertion{
		Operator: assertions.StringOperatorEquals,
		Expected: "test",
	}
	assert.Equal(t, assertions.AssertionTypeBody, assertion.GetType())
}

func TestStringAssertion_Validate_Equals(t *testing.T) {
	t.Skip("TODO: Implement string validation logic")

	testCases := []struct {
		name     string
		expected string
		actual   string
		want     bool
	}{
		{"exact match", "hello", "hello", true},
		{"different values", "hello", "world", false},
		{"empty strings", "", "", true},
		{"case sensitive", "Hello", "hello", false},
	}

	for _, tc := range testCases {
		t.Run(
			tc.name, func(t *testing.T) {
				assertion := assertions.StringAssertion{
					Operator: assertions.StringOperatorEquals,
					Expected: tc.expected,
				}
				result := assertion.Validate(tc.actual)
				assert.Equal(t, tc.want, result)
			},
		)
	}
}

func TestStringAssertion_Validate_NotEquals(t *testing.T) {
	t.Skip("TODO: Implement string validation logic")

	testCases := []struct {
		name     string
		expected string
		actual   string
		want     bool
	}{
		{"different values", "hello", "world", true},
		{"same values", "hello", "hello", false},
		{"empty vs non-empty", "", "hello", true},
	}

	for _, tc := range testCases {
		t.Run(
			tc.name, func(t *testing.T) {
				assertion := assertions.StringAssertion{
					Operator: assertions.StringOperatorNotEquals,
					Expected: tc.expected,
				}
				result := assertion.Validate(tc.actual)
				assert.Equal(t, tc.want, result)
			},
		)
	}
}

func TestStringAssertion_Validate_Contains(t *testing.T) {
	t.Skip("TODO: Implement string validation logic")

	testCases := []struct {
		name     string
		expected string
		actual   string
		want     bool
	}{
		{"substring exists", "world", "hello world", true},
		{"substring at start", "hello", "hello world", true},
		{"substring at end", "world", "hello world", true},
		{"substring not found", "foo", "hello world", false},
		{"empty substring", "", "hello", true},
	}

	for _, tc := range testCases {
		t.Run(
			tc.name, func(t *testing.T) {
				assertion := assertions.StringAssertion{
					Operator: assertions.StringOperatorContains,
					Expected: tc.expected,
				}
				result := assertion.Validate(tc.actual)
				assert.Equal(t, tc.want, result)
			},
		)
	}
}

func TestStringAssertion_Validate_NotContains(t *testing.T) {
	t.Skip("TODO: Implement string validation logic")

	testCases := []struct {
		name     string
		expected string
		actual   string
		want     bool
	}{
		{"substring not found", "foo", "hello world", true},
		{"substring exists", "world", "hello world", false},
	}

	for _, tc := range testCases {
		t.Run(
			tc.name, func(t *testing.T) {
				assertion := assertions.StringAssertion{
					Operator: assertions.StringOperatorNotContains,
					Expected: tc.expected,
				}
				result := assertion.Validate(tc.actual)
				assert.Equal(t, tc.want, result)
			},
		)
	}
}

func TestStringAssertion_Validate_StartsWith(t *testing.T) {
	t.Skip("TODO: Implement string validation logic")

	testCases := []struct {
		name     string
		expected string
		actual   string
		want     bool
	}{
		{"starts with prefix", "hello", "hello world", true},
		{"does not start with prefix", "world", "hello world", false},
		{"empty prefix", "", "hello", true},
		{"prefix longer than string", "hello world!", "hello", false},
	}

	for _, tc := range testCases {
		t.Run(
			tc.name, func(t *testing.T) {
				assertion := assertions.StringAssertion{
					Operator: assertions.StringOperatorStartsWith,
					Expected: tc.expected,
				}
				result := assertion.Validate(tc.actual)
				assert.Equal(t, tc.want, result)
			},
		)
	}
}

func TestStringAssertion_Validate_EndsWith(t *testing.T) {
	t.Skip("TODO: Implement string validation logic")

	testCases := []struct {
		name     string
		expected string
		actual   string
		want     bool
	}{
		{"ends with suffix", "world", "hello world", true},
		{"does not end with suffix", "hello", "hello world", false},
		{"empty suffix", "", "hello", true},
		{"suffix longer than string", "hello world!", "world", false},
	}

	for _, tc := range testCases {
		t.Run(
			tc.name, func(t *testing.T) {
				assertion := assertions.StringAssertion{
					Operator: assertions.StringOperatorEndsWith,
					Expected: tc.expected,
				}
				result := assertion.Validate(tc.actual)
				assert.Equal(t, tc.want, result)
			},
		)
	}
}

func TestStringAssertion_Validate_Regex(t *testing.T) {
	t.Skip("TODO: Implement string validation logic")

	testCases := []struct {
		name    string
		pattern string
		actual  string
		want    bool
	}{
		{"simple pattern match", "^hello", "hello world", true},
		{"pattern not match", "^world", "hello world", false},
		{
			"email pattern", `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`, "user@example.com",
			true,
		},
		{
			"invalid email", `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`, "invalid-email",
			false,
		},
		{"order ID pattern", `^[A-Z]{3}-\d{4}$`, "ABC-1234", true},
		{"invalid order ID", `^[A-Z]{3}-\d{4}$`, "ABC-12", false},
	}

	for _, tc := range testCases {
		t.Run(
			tc.name, func(t *testing.T) {
				assertion := assertions.StringAssertion{
					Operator: assertions.StringOperatorRegex,
					Expected: tc.pattern,
				}
				result := assertion.Validate(tc.actual)
				assert.Equal(t, tc.want, result)
			},
		)
	}
}

func TestStringAssertion_Validate_Empty(t *testing.T) {
	t.Skip("TODO: Implement string validation logic")

	testCases := []struct {
		name   string
		actual string
		want   bool
	}{
		{"empty string", "", true},
		{"non-empty string", "hello", false},
		{"whitespace only", "   ", false},
	}

	for _, tc := range testCases {
		t.Run(
			tc.name, func(t *testing.T) {
				assertion := assertions.StringAssertion{
					Operator: assertions.StringOperatorEmpty,
				}
				result := assertion.Validate(tc.actual)
				assert.Equal(t, tc.want, result)
			},
		)
	}
}

func TestStringAssertion_Validate_NotEmpty(t *testing.T) {
	t.Skip("TODO: Implement string validation logic")

	testCases := []struct {
		name   string
		actual string
		want   bool
	}{
		{"empty string", "", false},
		{"non-empty string", "hello", true},
		{"whitespace only", "   ", true},
	}

	for _, tc := range testCases {
		t.Run(
			tc.name, func(t *testing.T) {
				assertion := assertions.StringAssertion{
					Operator: assertions.StringOperatorNotEmpty,
				}
				result := assertion.Validate(tc.actual)
				assert.Equal(t, tc.want, result)
			},
		)
	}
}

func TestStringAssertion_Validate_InvalidOperator(t *testing.T) {
	t.Skip("TODO: Implement string validation logic")

	assertion := assertions.StringAssertion{
		Operator: assertions.StringOperator("invalid"),
		Expected: "test",
	}
	result := assertion.Validate("test")
	assert.False(t, result, "invalid operator should return false")
}
