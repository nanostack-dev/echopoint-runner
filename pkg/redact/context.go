package redact

import "context"

type learnerKey struct{}

// WithLearner returns ctx carrying r, so code running anywhere under it,
// including nested module, loop and poll bodies, can add secret values to the
// run's redaction with Learn.
func WithLearner(ctx context.Context, r *Redactor) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, learnerKey{}, r)
}

// Learn adds value to the Redactor ctx carries, and does nothing when ctx
// carries none.
func Learn(ctx context.Context, value any) {
	if r, ok := ctx.Value(learnerKey{}).(*Redactor); ok {
		r.Learn(value)
	}
}
