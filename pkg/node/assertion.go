package node

import (
	"encoding/json"
	"fmt"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	_ "github.com/nanostack-dev/echopoint-runner/pkg/extractors/http" // Register HTTP extractors in init()
	"github.com/nanostack-dev/echopoint-runner/pkg/operators"
)

// CompositeAssertion combines an extractor with an operator for validation.
type CompositeAssertion struct {
	Extractor     extractors.AnyExtractor `json:"-"`             // The actual extractor instance
	Operator      any                     `json:"-"`             // The actual operator instance (for future use)
	ExtractorType string                  `json:"extractorType"` // jsonPath, xmlPath, statusCode, header, body
	ExtractorData any                     `json:"extractorData"` // Configuration for the extractor
	OperatorType  string                  `json:"operatorType"`  // equals, contains, greaterThan, etc.
	OperatorData  any                     `json:"operatorData"`  // Configuration for the operator
	ExpectedValue any                     `json:"-"`             // Resolved expected value (operator_data.value)
}

// assertionWire is the on-the-wire shape produced by the echopoint backend.
// Assertions are stored snake_case (extractor_type/extractor_data,
// operator_type/operator_data); older callers may send a nested extractor object.
type assertionWire struct {
	Extractor json.RawMessage `json:"extractor"`
	Operator  json.RawMessage `json:"operator"`

	ExtractorType string          `json:"extractor_type"`
	ExtractorData json.RawMessage `json:"extractor_data"`
	OperatorType  string          `json:"operator_type"`
	OperatorData  json.RawMessage `json:"operator_data"`

	// camelCase fallbacks
	ExtractorTypeCamel string          `json:"extractorType"`
	ExtractorDataCamel json.RawMessage `json:"extractorData"`
	OperatorTypeCamel  string          `json:"operatorType"`
	OperatorDataCamel  json.RawMessage `json:"operatorData"`
}

// UnmarshalJSON implements custom unmarshaling for CompositeAssertion.
func (ca *CompositeAssertion) UnmarshalJSON(data []byte) error {
	var w assertionWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}

	ca.ExtractorType = firstNonEmpty(w.ExtractorType, w.ExtractorTypeCamel)
	ca.OperatorType = firstNonEmpty(w.OperatorType, w.OperatorTypeCamel)
	extractorData := firstRaw(w.ExtractorData, w.ExtractorDataCamel)

	// Build the extractor. Prefer a nested "extractor" object; otherwise
	// synthesize one from extractor_type + extractor_data (a flat {type, ...data}).
	extractorJSON := w.Extractor
	if len(extractorJSON) == 0 && ca.ExtractorType != "" {
		synth, err := synthesizeExtractorJSON(ca.ExtractorType, extractorData)
		if err != nil {
			return err
		}
		extractorJSON = synth
	}
	if len(extractorJSON) > 0 {
		extractor, err := extractors.UnmarshalExtractor(extractorJSON)
		if err != nil {
			return fmt.Errorf("failed to unmarshal assertion extractor: %w", err)
		}
		ca.Extractor = extractor
	}

	// Resolve the expected value from operator_data.value.
	operatorData := firstRaw(w.OperatorData, w.OperatorDataCamel)
	if len(operatorData) > 0 {
		var od struct {
			Value any `json:"value"`
		}
		if err := json.Unmarshal(operatorData, &od); err != nil {
			return fmt.Errorf("failed to unmarshal assertion operator data: %w", err)
		}
		ca.ExpectedValue = od.Value
		ca.OperatorData = od
	}

	return nil
}

// synthesizeExtractorJSON merges the legacy extractor_data object with the
// extractor type into the flat {"type": ..., ...data} shape UnmarshalExtractor expects.
func synthesizeExtractorJSON(extractorType string, extractorData json.RawMessage) (json.RawMessage, error) {
	merged := map[string]any{}
	if len(extractorData) > 0 {
		if err := json.Unmarshal(extractorData, &merged); err != nil {
			return nil, fmt.Errorf("failed to unmarshal extractor data: %w", err)
		}
	}
	merged["type"] = extractorType
	out, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Evaluate runs the assertion against the response and returns a full result
// record. Index is left zero for the caller to assign. Passed is true only when
// the extractor succeeds and the operator holds; Error is set (and Passed false,
// Actual possibly nil) when the extractor or operator errors. This is the single
// evaluation entry point — it both decides pass/fail and captures what was
// compared, so callers never re-derive the outcome.
func (ca *CompositeAssertion) Evaluate(ctx extractors.ResponseContext) AssertionResult {
	res := AssertionResult{
		Extractor: ca.ExtractorType,
		Operator:  ca.OperatorType,
		Expected:  ca.ExpectedValue,
	}
	if ca.Extractor == nil {
		res.Error = fmt.Sprintf("assertion has no extractor (type %q)", ca.ExtractorType)
		return res
	}
	actual, err := ca.Extractor.Extract(ctx)
	if err != nil {
		res.Error = fmt.Sprintf("extractor %q failed: %v", ca.ExtractorType, err)
		return res
	}
	res.Actual = actual

	passed, err := operators.Compare(operators.OperatorType(ca.OperatorType), actual, ca.ExpectedValue)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Passed = passed
	return res
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func firstRaw(a, b json.RawMessage) json.RawMessage {
	if len(a) > 0 {
		return a
	}
	return b
}
