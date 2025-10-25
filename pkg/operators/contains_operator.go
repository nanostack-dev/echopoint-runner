package operators

import (
	"fmt"
	"strings"
)

// ContainsOperator checks if the actual value contains the expected substring
type ContainsOperator struct {
	Substring string `json:"substring"`
}

func (o ContainsOperator) Validate(actual interface{}) (bool, error) {
	actualStr, ok := actual.(string)
	if !ok {
		return false, fmt.Errorf("contains operator requires string, got %T", actual)
	}

	return strings.Contains(actualStr, o.Substring), nil
}

func (o ContainsOperator) GetType() OperatorType {
	return OperatorTypeContains
}
