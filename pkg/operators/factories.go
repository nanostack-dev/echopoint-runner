package operators

// StringOperators provides factory methods for creating string-specific operators.
type StringOperators struct{}

func (s StringOperators) Equals(expected string) Operator {
	return EqualsOperator{Expected: expected}
}

func (s StringOperators) NotEquals(expected string) Operator {
	return NotEqualsOperator{Expected: expected}
}

func (s StringOperators) Contains(substring string) Operator {
	return ContainsOperator{Substring: substring}
}

func (s StringOperators) NotContains(substring string) Operator {
	return NotContainsOperator{Substring: substring}
}

func (s StringOperators) StartsWith(prefix string) Operator {
	return StartsWithOperator{Prefix: prefix}
}

func (s StringOperators) EndsWith(suffix string) Operator {
	return EndsWithOperator{Suffix: suffix}
}

func (s StringOperators) Regex(pattern string) Operator {
	return RegexOperator{Pattern: pattern}
}

func (s StringOperators) Empty() Operator {
	return EmptyOperator{}
}

func (s StringOperators) NotEmpty() Operator {
	return NotEmptyOperator{}
}

// NumberOperators provides factory methods for creating number-specific operators.
type NumberOperators struct{}

func (n NumberOperators) Equals(expected float64) Operator {
	return EqualsOperator{Expected: expected}
}

func (n NumberOperators) NotEquals(expected float64) Operator {
	return NotEqualsOperator{Expected: expected}
}

func (n NumberOperators) GreaterThan(expected float64) Operator {
	return GreaterThanOperator{Expected: expected}
}

func (n NumberOperators) LessThan(expected float64) Operator {
	return LessThanOperator{Expected: expected}
}

func (n NumberOperators) GreaterThanOrEqual(expected float64) Operator {
	return GreaterThanOrEqualOperator{Expected: expected}
}

func (n NumberOperators) LessThanOrEqual(expected float64) Operator {
	return LessThanOrEqualOperator{Expected: expected}
}

func (n NumberOperators) Between(minVal, maxVal float64) Operator {
	return BetweenOperator{Min: minVal, Max: maxVal}
}

// BooleanOperators provides factory methods for creating boolean-specific operators.
type BooleanOperators struct{}

func (b BooleanOperators) Equals(expected bool) Operator {
	return EqualsOperator{Expected: expected}
}

func (b BooleanOperators) IsTrue() Operator {
	return EqualsOperator{Expected: true}
}

func (b BooleanOperators) IsFalse() Operator {
	return EqualsOperator{Expected: false}
}
