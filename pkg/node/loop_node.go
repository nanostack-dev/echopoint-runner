package node

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// LoopData configures a foreach loop node.
type LoopData struct {
	// Items is a template (or literal) that resolves to a []any. Each element is
	// iterated over and the body sub-flow is run once per element.
	Items any `json:"items"`
	// Body is an inline flow definition ({"nodes":[...],"edges":[...]}) executed
	// once per iteration via the shared spi.ModuleExecutor.
	Body json.RawMessage `json:"body"`
	// ItemVar is the FlowInputs key the current item is injected under (default "item").
	ItemVar string `json:"item_var"`
	// IndexVar is the FlowInputs key the current zero-based index is injected under
	// (default "index").
	IndexVar string `json:"index_var"`
	// MaxIterations, when > 0, caps how many items are iterated as a safety bound.
	MaxIterations int `json:"max_iterations"`
	// ContinueOnError keeps the loop running when an iteration fails, recording the
	// error in that iteration's result instead of failing the whole node.
	ContinueOnError bool `json:"continue_on_error"`
}

// LoopNode iterates a body sub-flow once per item in a resolved array (foreach),
// injecting the per-iteration item and index into the child flow inputs and
// collecting each iteration's final outputs into an array.
type LoopNode struct {
	BaseNode

	Data LoopData `json:"data"`
}

// AsLoopNode safely casts an AnyNode to a LoopNode.
func AsLoopNode(candidate AnyNode) (*LoopNode, bool) {
	loopNode, ok := candidate.(*LoopNode)
	return loopNode, ok
}

func (n *LoopNode) GetData() LoopData {
	return n.Data
}

func (n *LoopNode) itemVar() string {
	if n.Data.ItemVar == "" {
		return "item"
	}
	return n.Data.ItemVar
}

func (n *LoopNode) indexVar() string {
	if n.Data.IndexVar == "" {
		return "index"
	}
	return n.Data.IndexVar
}

// InputSchema infers inputs from the items template only. Body references are
// validated inside the child flow parse, not by the parent loop node.
func (n *LoopNode) InputSchema() []string {
	vars := (&SchemaInference{}).ExtractTemplateVariables(n.Data.Items)
	sort.Strings(vars)
	return vars
}

// aggregateOutputs are the loop's intrinsic outputs, exposed as default JSONPath
// extractors over the node's assertion context (the {results, count} value). The
// engine-level output pass extracts and validates them uniformly, the same way
// it would any user-declared output.
func (n *LoopNode) aggregateOutputs() []Output {
	return []Output{
		{Name: "results", Extractor: extractors.JSONPathExtractor{Path: "$.results"}},
		{Name: outputKeyCount, Extractor: extractors.JSONPathExtractor{Path: "$.count"}},
	}
}

// GetOutputs returns the loop's default aggregate extractors (results, count)
// followed by any user-declared outputs. A user-declared output with the same
// name overrides the default, so the loop's intrinsic outputs stay referenceable
// while remaining customizable.
func (n *LoopNode) GetOutputs() []Output {
	user := n.BaseNode.GetOutputs()
	declared := make(map[string]struct{}, len(user))
	for _, o := range user {
		declared[o.Name] = struct{}{}
	}

	defaults := n.aggregateOutputs()
	outputs := make([]Output, 0, len(defaults)+len(user))
	for _, o := range defaults {
		if _, overridden := declared[o.Name]; !overridden {
			outputs = append(outputs, o)
		}
	}
	return append(outputs, user...)
}

// OutputSchema lists every output the loop node produces: its intrinsic
// aggregates (results, count) plus any user-declared outputs.
func (n *LoopNode) OutputSchema() []string {
	outputs := n.GetOutputs()
	schema := make([]string, 0, len(outputs))
	for _, o := range outputs {
		schema = append(schema, o.Name)
	}
	return schema
}

// Execute resolves the items array and runs the body sub-flow once per item,
// injecting item/index into the child flow inputs and aggregating per-iteration
// final outputs into a results array.
func (n *LoopNode) Execute(ctx spi.ExecutionContext) (spi.AnyResult, error) {
	startTime := time.Now()

	if ctx.ModuleExecutor == nil {
		err := errors.New("module executor unavailable")
		return n.createErrorResult(ctx.Inputs, err, startTime, 0), err
	}
	if len(n.Data.Body) == 0 {
		err := errors.New("loop body is required")
		return n.createErrorResult(ctx.Inputs, err, startTime, 0), err
	}

	items, err := n.resolveItems(ctx.Inputs)
	if err != nil {
		return n.createErrorResult(ctx.Inputs, err, startTime, 0), err
	}

	if n.Data.MaxIterations > 0 && len(items) > n.Data.MaxIterations {
		items = items[:n.Data.MaxIterations]
	}

	log.Debug().
		Str("nodeID", n.GetID()).
		Int("iterations", len(items)).
		Msg("Starting loop node execution")

	results := make([]map[string]any, 0, len(items))
	for i, item := range items {
		if cancelErr := ctx.Context().Err(); cancelErr != nil {
			log.Warn().
				Str("nodeID", n.GetID()).
				Int("index", i).
				Err(cancelErr).
				Msg("Loop node cancelled before completion")
			return n.createErrorResult(ctx.Inputs, cancelErr, startTime, len(items)), cancelErr
		}

		childInputs := cloneMap(ctx.FlowInputs)
		childInputs[n.itemVar()] = item
		childInputs[n.indexVar()] = i

		res, iterErr := ctx.ModuleExecutor.ExecuteModule(spi.ModuleExecutionRequest{
			FlowID:         n.GetID() + "#iter",
			FlowDefinition: n.Data.Body,
			Inputs:         childInputs,
		})
		if iterErr != nil {
			if n.Data.ContinueOnError {
				log.Warn().
					Str("nodeID", n.GetID()).
					Int("index", i).
					Err(iterErr).
					Msg("Loop iteration failed; continuing")
				results = append(results, map[string]any{"index": i, "error": iterErr.Error()})
				continue
			}
			wrapped := fmt.Errorf("loop iteration %d failed: %w", i, iterErr)
			return n.createErrorResult(ctx.Inputs, wrapped, startTime, len(items)), wrapped
		}

		var iterOutputs map[string]any
		if res != nil {
			iterOutputs = cloneMap(res.FinalOutputs)
		} else {
			iterOutputs = map[string]any{}
		}
		results = append(results, iterOutputs)
	}

	outputs := map[string]any{
		"results":      results,
		outputKeyCount: len(results),
	}

	log.Info().
		Str("nodeID", n.GetID()).
		Int("iterations", len(items)).
		Int("results", len(results)).
		Msg("Loop node executed successfully")

	return &LoopExecutionResult{
		BaseExecutionResult: spi.BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    spi.KindLoop,
			Inputs:      ctx.Inputs,
			Outputs:     outputs,
			ExecutedAt:  time.Now(),
		},
		Iterations: len(items),
		DurationMs: time.Since(startTime).Milliseconds(),
		// Expose the aggregate outputs to the engine-level assertion/output pass
		// so users can assert on the collected results (e.g. results/count).
		assertionCtx: extractors.NewValueResponseContext(outputs),
	}, nil
}

// resolveItems resolves the items template against the node inputs and coerces
// the result into a []any, erroring when it does not resolve to a list.
func (n *LoopNode) resolveItems(inputs map[string]any) ([]any, error) {
	resolver := NewTemplateResolver(inputs)
	resolved, err := resolver.Resolve(n.Data.Items)
	if err != nil {
		return nil, fmt.Errorf("resolve loop items: %w", err)
	}

	items, ok := resolved.([]any)
	if !ok {
		return nil, fmt.Errorf("loop items must resolve to a list, got %T", resolved)
	}
	return items, nil
}

func (n *LoopNode) createErrorResult(
	inputs map[string]any,
	err error,
	startedAt time.Time,
	iterations int,
) spi.AnyResult {
	errMsg := err.Error()
	errCode := "LOOP_FAILED"

	return &LoopExecutionResult{
		BaseExecutionResult: spi.BaseExecutionResult{
			NodeID:      n.GetID(),
			DisplayName: n.GetDisplayName(),
			NodeType:    spi.KindLoop,
			Inputs:      inputs,
			Outputs:     nil,
			Error:       err,
			ErrorMsg:    &errMsg,
			ErrorCode:   &errCode,
			ExecutedAt:  time.Now(),
		},
		Iterations: iterations,
		DurationMs: time.Since(startedAt).Milliseconds(),
	}
}
