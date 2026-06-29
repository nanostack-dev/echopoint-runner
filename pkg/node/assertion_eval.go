package node

import (
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// AssertionContextProvider is the optional interface a node RESULT implements to
// expose the ResponseContext its assertions and outputs evaluate against. The
// engine drives the assertion/output pass uniformly for any result that
// implements it; results that do not (delay, module) are left untouched.
type AssertionContextProvider interface {
	AssertionContext() extractors.ResponseContext
}

// EvaluateAssertions evaluates every assertion against rc, assigning each result
// its Index. Evaluation stops at the first failing or erroring assertion — that
// one IS recorded — and the corresponding error is returned. The returned slice
// holds results for all assertions up to and including the first failure (or all
// of them on success). This is the single assertion-evaluation implementation;
// every node delegates to it.
func EvaluateAssertions(
	assertions []CompositeAssertion, rc extractors.ResponseContext,
) ([]spi.AssertionResult, error) {
	results := make([]spi.AssertionResult, 0, len(assertions))
	for i := range assertions {
		res := assertions[i].Evaluate(rc)
		res.Index = i
		results = append(results, res)

		if res.Error != "" {
			return results, fmt.Errorf("assertion %d (%s %s) evaluation error: %s",
				i, res.Extractor, res.Operator, res.Error)
		}

		if !res.Passed {
			return results, fmt.Errorf(
				"assertion %d failed: %s %s expected=%v actual=%v",
				i, res.Extractor, res.Operator, res.Expected, res.Actual)
		}
	}

	return results, nil
}

// ExtractOutputs runs every output extractor against rc, returning the produced
// name->value map. It fails fast on the first extractor error. This is the single
// output-extraction implementation; every node delegates to it.
func ExtractOutputs(
	outputs []Output, rc extractors.ResponseContext,
) (map[string]any, error) {
	produced := make(map[string]any, len(outputs))
	for _, outputItem := range outputs {
		value, err := outputItem.Extractor.Extract(rc)
		if err != nil {
			return nil, err
		}
		produced[outputItem.Name] = value
	}
	return produced, nil
}

// ValidateOutputs checks that every name in outputSchema was produced. It returns
// an error naming the first missing output. This is the single output-validation
// implementation; every node delegates to it.
func ValidateOutputs(outputSchema []string, produced map[string]any) error {
	for _, expectedKey := range outputSchema {
		if _, exists := produced[expectedKey]; !exists {
			return fmt.Errorf("failed to extract expected output: %s", expectedKey)
		}
	}
	return nil
}

// runAssertions delegates to EvaluateAssertions, logging at the node level. Kept
// so existing request-node test seams and call sites stay behavior-identical.
func (n *RequestNode) runAssertions(
	rc extractors.ResponseContext,
) ([]spi.AssertionResult, error) {
	assertions := n.GetAssertions()
	log.Debug().
		Str("nodeID", n.GetID()).
		Int("assertionCount", len(assertions)).
		Msg("Running assertions")

	results, err := EvaluateAssertions(assertions, rc)
	if err != nil {
		log.Error().
			Str("nodeID", n.GetID()).
			Err(err).
			Msg("Assertion evaluation failed")
		return results, err
	}

	log.Debug().
		Str("nodeID", n.GetID()).
		Msg("All assertions passed")
	return results, nil
}
