package redact

import (
	"context"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

type learnerKey struct{}

// WithLearner returns ctx carrying r, so code running anywhere under it,
// including nested module, loop and poll bodies, can add secret outputs to the
// run's redaction with LearnOutput.
func WithLearner(ctx context.Context, r *Redactor) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, learnerKey{}, r)
}

// LearnOutput adds a secret output to the Redactor ctx carries, and does
// nothing when ctx carries none. See Redactor.LearnOutput.
func LearnOutput(ctx context.Context, nodeID, name string, value any) {
	if r, ok := ctx.Value(learnerKey{}).(*Redactor); ok {
		r.LearnOutput(nodeID, name, value)
	}
}

// Withhold masks an output by name in the Redactor ctx carries.
func Withhold(ctx context.Context, nodeID, name string) {
	if r, ok := ctx.Value(learnerKey{}).(*Redactor); ok {
		r.Withhold(nodeID, name)
	}
}

// Withheld reports whether the Redactor ctx carries masks ref by name.
func Withheld(ctx context.Context, ref string) bool {
	r, ok := ctx.Value(learnerKey{}).(*Redactor)
	return ok && r.Withheld(ref)
}

// ErrorMessage returns spi.SafeErrorMessage(err) masked by the Redactor ctx
// carries, for a log line. A UserError message can quote a value a node
// compared, such as an assertion's actual value, and logs are written outside
// the reporting boundary that masks results.
func ErrorMessage(ctx context.Context, err error) string {
	message := spi.SafeErrorMessage(err)
	if r, ok := ctx.Value(learnerKey{}).(*Redactor); ok {
		return r.Text(message)
	}
	return message
}
