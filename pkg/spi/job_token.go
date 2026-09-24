package spi

import "context"

type jobTokenContextKey struct{}

// WithJobToken attaches the token of the claimed job to the run context. A node
// that calls the control plane for its own job, such as the webhook wait, reads
// it with JobTokenFromContext. The token never enters the flow inputs, so a flow
// template cannot copy it into a request.
func WithJobToken(ctx context.Context, token string) context.Context {
	if token == "" {
		return ctx
	}
	return context.WithValue(ctx, jobTokenContextKey{}, token)
}

// JobTokenFromContext returns the job token that WithJobToken attached.
func JobTokenFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	token, ok := ctx.Value(jobTokenContextKey{}).(string)
	return token, ok && token != ""
}
