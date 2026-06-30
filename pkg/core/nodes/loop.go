package nodes

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

const (
	defaultItemVar  = "item"
	defaultIndexVar = "index"
)

// LoopCfg configures a foreach loop: run an inline body flow once per item,
// injecting the item and its index as inputs, and aggregate each iteration's
// outputs into a results array. It is a provider node — it exposes the aggregate
// {results, count} for the engine's assertion/output post-step.
type LoopCfg struct {
	node.Base

	Items           json.RawMessage `json:"items"`
	Body            json.RawMessage `json:"body"`
	ItemVar         string          `json:"item_var"`
	IndexVar        string          `json:"index_var"`
	MaxIterations   int             `json:"max_iterations"`
	ContinueOnError bool            `json:"continue_on_error"`
}

func runLoop(ctx context.Context, cfg LoopCfg, _ value.Value, rt node.Runtime) (node.Result, error) {
	items, ok := value.JSON(cfg.Items).List()
	if !ok {
		return node.Result{}, fmt.Errorf("loop items must resolve to a list: %w", node.ErrUser)
	}
	body, err := flow.Parse(cfg.Body)
	if err != nil {
		return node.Result{}, fmt.Errorf("loop body: %w", node.ErrUser)
	}
	itemVar := orDefault(cfg.ItemVar, defaultItemVar)
	indexVar := orDefault(cfg.IndexVar, defaultIndexVar)

	count := len(items)
	if cfg.MaxIterations > 0 && cfg.MaxIterations < count {
		count = cfg.MaxIterations
	}

	results := make([]any, 0, count)
	for i := range count {
		if cerr := ctx.Err(); cerr != nil {
			return node.Result{}, cerr
		}
		in := value.Map{itemVar: items[i], indexVar: value.Of(i)}
		out, runErr := rt.Subflow.RunInline(ctx, body, in)
		if runErr != nil {
			if cfg.ContinueOnError {
				results = append(results, map[string]any{"index": i, "error": runErr.Error()})
				continue
			}
			return node.Result{}, fmt.Errorf("loop iteration %d failed: %w", i, node.ErrUser)
		}
		results = append(results, out.Value().Raw())
	}

	agg := value.Of(map[string]any{outKeyResults: results, outKeyCount: len(results)})
	return node.Result{
		Outputs: value.Map{outKeyResults: value.Of(results), outKeyCount: value.Of(len(results))},
		Assert:  agg,
	}, nil
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

//nolint:gochecknoinits // register the built-in node kind at package load
func init() { node.Register(spi.KindLoop, runLoop) }
