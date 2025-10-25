package operators

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test EqualsOperator
func TestEqualsOperator_String(t *testing.T) {
	op := EqualsOperator{Expected: "hello"}

	result, err := op.Validate("hello")
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate("world")
	require.NoError(t, err)
	assert.False(t, result)
}

func TestEqualsOperator_Number(t *testing.T) {
	op := EqualsOperator{Expected: 200}

	result, err := op.Validate(200)
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate(404)
	require.NoError(t, err)
	assert.False(t, result)
}

func TestEqualsOperator_Boolean(t *testing.T) {
	op := EqualsOperator{Expected: true}

	result, err := op.Validate(true)
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate(false)
	require.NoError(t, err)
	assert.False(t, result)
}

// Test ContainsOperator
func TestContainsOperator(t *testing.T) {
	op := ContainsOperator{Substring: "world"}

	result, err := op.Validate("hello world")
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate("hello")
	require.NoError(t, err)
	assert.False(t, result)
}

func TestContainsOperator_InvalidType(t *testing.T) {
	op := ContainsOperator{Substring: "test"}

	result, err := op.Validate(123)
	require.Error(t, err)
	assert.False(t, result)
	assert.Contains(t, err.Error(), "requires string")
}

// Test GreaterThanOperator
func TestGreaterThanOperator(t *testing.T) {
	op := GreaterThanOperator{Expected: 100}

	result, err := op.Validate(200)
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate(50)
	require.NoError(t, err)
	assert.False(t, result)

	result, err = op.Validate(100)
	require.NoError(t, err)
	assert.False(t, result)
}

// Test LessThanOperator
func TestLessThanOperator(t *testing.T) {
	op := LessThanOperator{Expected: 100}

	result, err := op.Validate(50)
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate(200)
	require.NoError(t, err)
	assert.False(t, result)
}

// Test BetweenOperator
func TestBetweenOperator(t *testing.T) {
	op := BetweenOperator{Min: 200, Max: 299}

	result, err := op.Validate(250)
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate(200)
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate(299)
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate(100)
	require.NoError(t, err)
	assert.False(t, result)

	result, err = op.Validate(300)
	require.NoError(t, err)
	assert.False(t, result)
}

// Test StartsWithOperator
func TestStartsWithOperator(t *testing.T) {
	op := StartsWithOperator{Prefix: "hello"}

	result, err := op.Validate("hello world")
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate("world hello")
	require.NoError(t, err)
	assert.False(t, result)
}

// Test EndsWithOperator
func TestEndsWithOperator(t *testing.T) {
	op := EndsWithOperator{Suffix: "world"}

	result, err := op.Validate("hello world")
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate("world hello")
	require.NoError(t, err)
	assert.False(t, result)
}

// Test RegexOperator
func TestRegexOperator(t *testing.T) {
	op := RegexOperator{Pattern: `^[A-Z]{3}-\d{4}$`}

	result, err := op.Validate("ABC-1234")
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate("ABC-12")
	require.NoError(t, err)
	assert.False(t, result)
}

func TestRegexOperator_InvalidPattern(t *testing.T) {
	op := RegexOperator{Pattern: `[invalid`}

	result, err := op.Validate("test")
	require.Error(t, err)
	assert.False(t, result)
	assert.Contains(t, err.Error(), "invalid regex pattern")
}

// Test EmptyOperator
func TestEmptyOperator(t *testing.T) {
	op := EmptyOperator{}

	result, err := op.Validate("")
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate("hello")
	require.NoError(t, err)
	assert.False(t, result)
}

// Test NotEmptyOperator
func TestNotEmptyOperator(t *testing.T) {
	op := NotEmptyOperator{}

	result, err := op.Validate("hello")
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate("")
	require.NoError(t, err)
	assert.False(t, result)
}

// Test NotEqualsOperator
func TestNotEqualsOperator(t *testing.T) {
	op := NotEqualsOperator{Expected: "hello"}

	result, err := op.Validate("world")
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate("hello")
	require.NoError(t, err)
	assert.False(t, result)
}

// Test NotContainsOperator
func TestNotContainsOperator(t *testing.T) {
	op := NotContainsOperator{Substring: "foo"}

	result, err := op.Validate("hello world")
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate("hello foo world")
	require.NoError(t, err)
	assert.False(t, result)
}

// Test Factory Functions
func TestStringOperators_Factory(t *testing.T) {
	str := StringOperators{}

	op := str.Equals("test")
	assert.Equal(t, OperatorTypeEquals, op.GetType())

	op = str.Contains("substring")
	assert.Equal(t, OperatorTypeContains, op.GetType())

	op = str.StartsWith("prefix")
	assert.Equal(t, OperatorTypeStartsWith, op.GetType())

	op = str.EndsWith("suffix")
	assert.Equal(t, OperatorTypeEndsWith, op.GetType())

	op = str.Regex("pattern")
	assert.Equal(t, OperatorTypeRegex, op.GetType())

	op = str.Empty()
	assert.Equal(t, OperatorTypeEmpty, op.GetType())

	op = str.NotEmpty()
	assert.Equal(t, OperatorTypeNotEmpty, op.GetType())
}

func TestNumberOperators_Factory(t *testing.T) {
	num := NumberOperators{}

	op := num.Equals(200)
	assert.Equal(t, OperatorTypeEquals, op.GetType())

	op = num.GreaterThan(100)
	assert.Equal(t, OperatorTypeGreaterThan, op.GetType())

	op = num.LessThan(100)
	assert.Equal(t, OperatorTypeLessThan, op.GetType())

	op = num.Between(200, 299)
	assert.Equal(t, OperatorTypeBetween, op.GetType())
}

func TestBooleanOperators_Factory(t *testing.T) {
	bool := BooleanOperators{}

	op := bool.Equals(true)
	assert.Equal(t, OperatorTypeEquals, op.GetType())

	op = bool.IsTrue()
	assert.Equal(t, OperatorTypeEquals, op.GetType())

	op = bool.IsFalse()
	assert.Equal(t, OperatorTypeEquals, op.GetType())
}

// Test type conversions
func TestToFloat64(t *testing.T) {
	testCases := []struct {
		name     string
		input    interface{}
		expected float64
		ok       bool
	}{
		{"float64", float64(3.14), 3.14, true},
		{"float32", float32(3.14), float64(float32(3.14)), true},
		{"int", 42, 42.0, true},
		{"int32", int32(42), 42.0, true},
		{"int64", int64(42), 42.0, true},
		{"string", "not a number", 0, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, ok := toFloat64(tc.input)
			assert.Equal(t, tc.ok, ok)
			if ok {
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}
