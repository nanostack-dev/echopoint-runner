// Package runner is the single entrypoint for executing a flow. Every transport
// — the in-process cloud control plane, the self-hosted job runner, and the
// ephemeral CLI — drives execution through Run so the orchestration (input
// merge, referenced-flow module resolution, engine wiring) lives in exactly one
// place. Transports differ only in their Observer (where progress events go) and
// in how they serialize the returned result.
package runner

import (
	"context"
	"maps"

	"github.com/nanostack-dev/echopoint-runner/pkg/engine"
	"github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/redact"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// Options configures a Run. Use the With* helpers; the zero value runs with a
// no-op observer, no referenced flows, and no dynamic variables.
type Options struct {
	Observer        engine.ExecutionObserver
	ReferencedFlows flow.ReferencedFlowRegistry
	DynamicVars     spi.DynamicResolver
	ModuleCallStack []string
	Ctx             context.Context
	Middleware      []engine.Middleware
	SecretInputKeys []string
}

// Option mutates Options.
type Option func(*Options)

// WithObserver streams progress/terminal events to obs (SSE+DB for cloud, job
// events for self-hosted, none for ephemeral).
func WithObserver(obs engine.ExecutionObserver) Option {
	return func(o *Options) { o.Observer = obs }
}

// WithReferencedFlows supplies the child flow definitions module nodes execute
// without calling back to the control plane.
func WithReferencedFlows(refs flow.ReferencedFlowRegistry) Option {
	return func(o *Options) { o.ReferencedFlows = refs }
}

// WithDynamicVars enables {{$name}} dynamic variable resolution.
func WithDynamicVars(d spi.DynamicResolver) Option {
	return func(o *Options) { o.DynamicVars = d }
}

// WithModuleCallStack seeds the module-cycle-detection call stack (for nested
// module execution).
func WithModuleCallStack(stack []string) Option {
	return func(o *Options) { o.ModuleCallStack = stack }
}

// WithContext propagates a request-scoped context to every node execution
// (cancellation + deadlines). Defaults to context.Background().
func WithContext(ctx context.Context) Option {
	return func(o *Options) { o.Ctx = ctx }
}

// WithMiddleware wraps each node's execution (outermost first). Use the Retry /
// Timeout helpers or any custom engine.Middleware.
func WithMiddleware(middleware ...engine.Middleware) Option {
	return func(o *Options) { o.Middleware = append(o.Middleware, middleware...) }
}

// WithSecretInputKeys names the inputs whose values are secret. Their values are
// masked in every result and progress event the run reports.
func WithSecretInputKeys(keys []string) Option {
	return func(o *Options) { o.SecretInputKeys = keys }
}

// Run executes flowDef. It overlays inputs on the flow's declared InitialInputs
// (inputs win), resolves referenced flows into the module resolver, and runs the
// engine. The returned result is the single source of truth; callers serialize
// it as their transport requires.
//
// Run is also the boundary where secret input values are masked: everything the
// engine hands back — the flow result and every progress event — passes through
// the redactor before any caller sees it.
func Run(flowDef flow.Flow, inputs map[string]any, opts ...Option) (*spi.FlowExecutionResult, error) {
	options := Options{Observer: engine.NoopObserver{}}
	for _, opt := range opts {
		opt(&options)
	}

	mergedInputs := MergeInputs(flowDef.InitialInputs, inputs)
	redactor := redact.New(mergedInputs, options.SecretInputKeys)

	result, err := engine.ExecuteFlowDefinition(flowDef, mergedInputs, &engine.Options{
		Observer:        redactingObserver{inner: options.Observer, redactor: redactor},
		ModuleResolver:  ModuleResolver(options.ReferencedFlows),
		ModuleCallStack: options.ModuleCallStack,
		DynamicVars:     options.DynamicVars,
		Ctx:             options.Ctx,
		Middleware:      options.Middleware,
	})
	return redactor.FlowResult(result), redactor.Error(err)
}

// redactingObserver masks secret values in the results carried by progress
// events, which leave the runner before Run returns.
type redactingObserver struct {
	inner    engine.ExecutionObserver
	redactor *redact.Redactor
}

func (o redactingObserver) FlowStarted(evt engine.FlowStartedEvent) {
	o.inner.FlowStarted(evt)
}

func (o redactingObserver) NodeStarted(evt engine.NodeStartedEvent) {
	o.inner.NodeStarted(evt)
}

func (o redactingObserver) NodeFinished(evt engine.NodeFinishedEvent) {
	evt.Result = o.redactor.Result(evt.Result)
	o.inner.NodeFinished(evt)
}

func (o redactingObserver) FlowFinished(evt engine.FlowFinishedEvent) {
	evt.Result = o.redactor.FlowResult(evt.Result)
	o.inner.FlowFinished(evt)
}

// MergeInputs overlays override onto base, returning a new map. base values fill
// keys the caller did not provide (e.g. flow-declared defaults); override wins.
func MergeInputs(base, override map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(override))
	maps.Copy(merged, base)
	maps.Copy(merged, override)
	return merged
}

type referencedFlowResolver struct {
	flows map[string]spi.ResolvedModuleFlow
}

func (r referencedFlowResolver) ResolveFlow(flowID string) (spi.ResolvedModuleFlow, bool) {
	resolved, ok := r.flows[flowID]
	return resolved, ok
}

// ModuleResolver builds a spi.ModuleResolver from referenced flows. Returns nil
// when there are none, which the engine treats as "no module targets". This is
// the single implementation; it replaces the copies that lived in the cloud,
// self-hosted, and ephemeral transports.
func ModuleResolver(refs flow.ReferencedFlowRegistry) spi.ModuleResolver {
	if len(refs) == 0 {
		return nil
	}
	resolved := make(map[string]spi.ResolvedModuleFlow, len(refs))
	for flowID, ref := range refs {
		resolved[flowID] = spi.ResolvedModuleFlow{
			FlowDefinition: ref.FlowDefinition,
			InputOverrides: cloneInputs(ref.InputOverrides),
		}
	}
	return referencedFlowResolver{flows: resolved}
}

func cloneInputs(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(src))
	maps.Copy(cloned, src)
	return cloned
}
