package operators_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/operators"
)

// Test EqualsOperator.
func TestEqualsOperator_String(t *testing.T) {
	op := operators.EqualsOperator{Expected: "hello"}

	result, err := op.Validate("hello")
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate("world")
	require.NoError(t, err)
	assert.False(t, result)
}

func TestEqualsOperator_Number(t *testing.T) {
	op := operators.EqualsOperator{Expected: 200}

	result, err := op.Validate(200)
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate(404)
	require.NoError(t, err)
	assert.False(t, result)
}

func TestEqualsOperator_Boolean(t *testing.T) {
	op := operators.EqualsOperator{Expected: true}

	result, err := op.Validate(true)
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate(false)
	require.NoError(t, err)
	assert.False(t, result)
}

// Test ContainsOperator.
func TestContainsOperator(t *testing.T) {
	op := operators.ContainsOperator{Substring: "world"}

	result, err := op.Validate("hello world")
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate("hello")
	require.NoError(t, err)
	assert.False(t, result)
}

func TestContainsOperator_InvalidType(t *testing.T) {
	op := operators.ContainsOperator{Substring: "test"}

	result, err := op.Validate(123)
	require.Error(t, err)
	assert.False(t, result)
	assert.Contains(t, err.Error(), "requires string")
}

// Test GreaterThanOperator.
func TestGreaterThanOperator(t *testing.T) {
	op := operators.GreaterThanOperator{Expected: 100}

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

// Test LessThanOperator.
func TestLessThanOperator(t *testing.T) {
	op := operators.LessThanOperator{Expected: 100}

	result, err := op.Validate(50)
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate(200)
	require.NoError(t, err)
	assert.False(t, result)
}

// Test BetweenOperator.
func TestBetweenOperator(t *testing.T) {
	op := operators.BetweenOperator{Min: 200, Max: 299}

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

// Test StartsWithOperator.
func TestStartsWithOperator(t *testing.T) {
	op := operators.StartsWithOperator{Prefix: "hello"}

	result, err := op.Validate("hello world")
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate("world hello")
	require.NoError(t, err)
	assert.False(t, result)
}

// Test EndsWithOperator.
func TestEndsWithOperator(t *testing.T) {
	op := operators.EndsWithOperator{Suffix: "world"}

	result, err := op.Validate("hello world")
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate("world hello")
	require.NoError(t, err)
	assert.False(t, result)
}

// Test RegexOperator.
func TestRegexOperator(t *testing.T) {
	op := operators.RegexOperator{Pattern: `^[A-Z]{3}-\d{4}$`}

	result, err := op.Validate("ABC-1234")
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate("ABC-12")
	require.NoError(t, err)
	assert.False(t, result)
}

func TestRegexOperator_InvalidPattern(t *testing.T) {
	op := operators.RegexOperator{Pattern: `[invalid`}

	result, err := op.Validate("test")
	require.Error(t, err)
	assert.False(t, result)
	assert.Contains(t, err.Error(), "invalid regex pattern")
}

// Test EmptyOperator.
func TestEmptyOperator(t *testing.T) {
	op := operators.EmptyOperator{}

	result, err := op.Validate("")
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate("hello")
	require.NoError(t, err)
	assert.False(t, result)
}

// Test NotEmptyOperator.
func TestNotEmptyOperator(t *testing.T) {
	op := operators.NotEmptyOperator{}

	result, err := op.Validate("hello")
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate("")
	require.NoError(t, err)
	assert.False(t, result)
}

// Test NotEqualsOperator.
func TestNotEqualsOperator(t *testing.T) {
	op := operators.NotEqualsOperator{Expected: "hello"}

	result, err := op.Validate("world")
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate("hello")
	require.NoError(t, err)
	assert.False(t, result)
}

// Test NotContainsOperator.
func TestNotContainsOperator(t *testing.T) {
	op := operators.NotContainsOperator{Substring: "foo"}

	result, err := op.Validate("hello world")
	require.NoError(t, err)
	assert.True(t, result)

	result, err = op.Validate("hello foo world")
	require.NoError(t, err)
	assert.False(t, result)
}

// Test Factory Functions.
func TestStringOperators_Factory(t *testing.T) {
	str := operators.StringOperators{}

	op := str.Equals("test")
	assert.Equal(t, operators.OperatorTypeEquals, op.GetType())

	op = str.Contains("substring")
	assert.Equal(t, operators.OperatorTypeContains, op.GetType())

	op = str.StartsWith("prefix")
	assert.Equal(t, operators.OperatorTypeStartsWith, op.GetType())

	op = str.EndsWith("suffix")
	assert.Equal(t, operators.OperatorTypeEndsWith, op.GetType())

	op = str.Regex("pattern")
	assert.Equal(t, operators.OperatorTypeRegex, op.GetType())

	op = str.Empty()
	assert.Equal(t, operators.OperatorTypeEmpty, op.GetType())

	op = str.NotEmpty()
	assert.Equal(t, operators.OperatorTypeNotEmpty, op.GetType())
}

func TestNumberOperators_Factory(t *testing.T) {
	num := operators.NumberOperators{}

	op := num.Equals(200)
	assert.Equal(t, operators.OperatorTypeEquals, op.GetType())

	op = num.GreaterThan(100)
	assert.Equal(t, operators.OperatorTypeGreaterThan, op.GetType())

	op = num.LessThan(100)
	assert.Equal(t, operators.OperatorTypeLessThan, op.GetType())

	op = num.Between(200, 299)
	assert.Equal(t, operators.OperatorTypeBetween, op.GetType())
}

func TestBooleanOperators_Factory(t *testing.T) {
	boolOps := operators.BooleanOperators{}

	op := boolOps.Equals(true)
	assert.Equal(t, operators.OperatorTypeEquals, op.GetType())

	op = boolOps.IsTrue()
	assert.Equal(t, operators.OperatorTypeEquals, op.GetType())

	op = boolOps.IsFalse()
	assert.Equal(t, operators.OperatorTypeEquals, op.GetType())
}
