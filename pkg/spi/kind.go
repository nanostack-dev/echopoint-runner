package spi

// Kind identifies a node kind on the wire. The node-kind registry, not a fixed
// set, decides which kinds are valid; the constants below are the built-ins.
type Kind string

// Built-in node kinds.
const (
	KindRequest Kind = "request"
	KindDelay   Kind = "delay"
	KindModule  Kind = "module"
	KindAssert  Kind = "assert"
)

// RunWhen controls whether a node runs only on the success path or also after
// the main phase has already failed.
type RunWhen string

// RunWhen phases.
const (
	RunWhenOnSuccess RunWhen = "on_success"
	RunWhenAlways    RunWhen = "always"
)
