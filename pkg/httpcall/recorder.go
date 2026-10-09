// Package httpcall records the outbound HTTP calls of one execution and
// enforces the HTTP call limit.
//
// runner.Run puts one Recorder in the execution context. Nested flows (loops,
// polls, modules) run on that same context, so their calls land in the same
// list and count against the same limit.
package httpcall

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// LimitExceededCode is the error code of the node whose call passed the limit.
const LimitExceededCode = "HTTP_CALL_LIMIT_EXCEEDED"

const maxPathTemplateLength = 512

type recorderKey struct{}

// Recorder collects the calls of one execution. It is safe for concurrent use
// by parallel nodes.
type Recorder struct {
	mu      sync.Mutex
	limit   int
	started int
	calls   []spi.HTTPCall
}

// NewRecorder returns a recorder that refuses the call past limit. A limit of
// zero or less records without refusing.
func NewRecorder(limit int) *Recorder {
	return &Recorder{limit: limit}
}

// WithRecorder returns ctx carrying recorder.
func WithRecorder(ctx context.Context, recorder *Recorder) context.Context {
	return context.WithValue(ctx, recorderKey{}, recorder)
}

// FromContext returns the recorder in ctx, or nil.
func FromContext(ctx context.Context) *Recorder {
	if ctx == nil {
		return nil
	}
	recorder, _ := ctx.Value(recorderKey{}).(*Recorder)
	return recorder
}

// Calls returns the finished calls ordered by Seq.
func (r *Recorder) Calls() []spi.HTTPCall {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	calls := slices.Clone(r.calls)
	slices.SortFunc(calls, func(a, b spi.HTTPCall) int { return a.Seq - b.Seq })
	return calls
}

// Target names the call a node is about to send.
type Target struct {
	NodeID string
	Method string
	// URL is the resolved URL the request is sent to.
	URL string
	// URLTemplate is the node's URL before template resolution.
	URLTemplate string
}

// Call is a started call. A call started without a recorder, and a nil *Call,
// record nothing, so nodes run without a recorder need no special case.
type Call struct {
	recorder *Recorder
	record   spi.HTTPCall
}

// Start reserves the next call of the execution in ctx. Past the limit it
// returns a user error with LimitExceededCode and the node must not send the
// request. Without a recorder in ctx it returns a call that records nothing.
func Start(ctx context.Context, target Target) (*Call, error) {
	recorder := FromContext(ctx)
	if recorder == nil {
		return &Call{}, nil
	}

	recorder.mu.Lock()
	if recorder.limit > 0 && recorder.started >= recorder.limit {
		recorder.mu.Unlock()
		return nil, spi.NewUserError(
			LimitExceededCode,
			fmt.Sprintf("The execution reached its limit of %d HTTP calls", recorder.limit),
			nil,
		)
	}
	recorder.started++
	seq := recorder.started
	recorder.mu.Unlock()

	return &Call{
		recorder: recorder,
		record: spi.HTTPCall{
			Seq:          seq,
			NodeID:       target.NodeID,
			Method:       strings.ToUpper(target.Method),
			Host:         host(target.URL),
			PathTemplate: PathTemplate(target.URLTemplate),
			StartedAt:    time.Now().UTC(),
		},
	}, nil
}

// Outcome is how a call ended. Leave StatusCode nil when no response arrived,
// and ResponseBytes nil for a streamed body.
type Outcome struct {
	StatusCode    *int
	ErrorClass    spi.HTTPCallErrorClass
	ResponseBytes *int64
}

// Finish records the call. A 4xx or 5xx status sets the error class when the
// outcome has none.
func (c *Call) Finish(outcome Outcome) {
	if c == nil || c.recorder == nil {
		return
	}
	record := c.record
	record.DurationMs = time.Since(record.StartedAt).Milliseconds()
	record.StatusCode = outcome.StatusCode
	record.ResponseBytes = outcome.ResponseBytes
	record.ErrorClass = outcome.ErrorClass
	if record.ErrorClass == "" && outcome.StatusCode != nil {
		record.ErrorClass = statusClass(*outcome.StatusCode)
	}

	c.recorder.mu.Lock()
	c.recorder.calls = append(c.recorder.calls, record)
	c.recorder.mu.Unlock()
}

func statusClass(status int) spi.HTTPCallErrorClass {
	switch {
	case status >= http.StatusInternalServerError:
		return spi.HTTPCallError5xx
	case status >= http.StatusBadRequest:
		return spi.HTTPCallError4xx
	default:
		return ""
	}
}

func host(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Host)
}

// PathTemplate returns the path of an unresolved URL template, with each
// {{ expression }} written as {expression}. The scheme and host, or a leading
// variable that holds them, the query string and the fragment are dropped.
//
//	{{baseUrl}}/users/{{ userId }}?expand=true  ->  /users/{userId}
//	https://api.example.com/orders/{{id}}       ->  /orders/{id}
//	https://api.example.com                     ->  /
func PathTemplate(template string) string {
	path := strings.TrimSpace(template)
	path = cutOutsideTemplates(path, '?')
	path = cutOutsideTemplates(path, '#')

	switch {
	case strings.HasPrefix(path, "{{"):
		if end := strings.Index(path, "}}"); end >= 0 {
			path = path[end+len("}}"):]
		}
	case strings.Contains(path, "://"):
		afterScheme := path[strings.Index(path, "://")+len("://"):]
		path = ""
		if slash := strings.IndexByte(afterScheme, '/'); slash >= 0 {
			path = afterScheme[slash:]
		}
	}
	if path == "" {
		path = "/"
	}

	path = normalizeExpressions(path)
	if len(path) > maxPathTemplateLength {
		path = path[:maxPathTemplateLength]
	}
	return path
}

// cutOutsideTemplates drops everything from the first sep that is not inside
// a {{ }} expression.
func cutOutsideTemplates(value string, sep byte) string {
	depth := 0
	for i := 0; i < len(value); i++ {
		switch {
		case strings.HasPrefix(value[i:], "{{"):
			depth++
			i++
		case strings.HasPrefix(value[i:], "}}") && depth > 0:
			depth--
			i++
		case value[i] == sep && depth == 0:
			return value[:i]
		}
	}
	return value
}

func normalizeExpressions(path string) string {
	var out strings.Builder
	for {
		start := strings.Index(path, "{{")
		if start < 0 {
			out.WriteString(path)
			return out.String()
		}
		end := strings.Index(path[start:], "}}")
		if end < 0 {
			out.WriteString(path)
			return out.String()
		}
		out.WriteString(path[:start])
		out.WriteString("{")
		out.WriteString(strings.TrimSpace(path[start+len("{{") : start+end]))
		out.WriteString("}")
		path = path[start+end+len("}}"):]
	}
}
