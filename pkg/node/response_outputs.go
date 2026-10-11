package node

import (
	"net/http"
	"unicode/utf8"
)

// Response outputs every request node exposes without a declaration.
const (
	ResponseOutputBody    = "body"
	ResponseOutputStatus  = "status"
	ResponseOutputHeaders = "headers"
)

// ImplicitOutputNode is implemented by nodes that expose outputs without a
// declaration. Flow validation accepts references to them, and paths under
// them, like declared outputs.
type ImplicitOutputNode interface {
	ImplicitOutputSchema() []string
}

// ImplicitOutputResult is implemented by results that carry those outputs. The
// engine keeps them in run memory for downstream templates and assertions; they
// are not part of the result's outputs, the flow's final outputs, loop results
// or module bindings, so a response body is not stored a second time.
type ImplicitOutputResult interface {
	ImplicitOutputs() map[string]any
}

// ImplicitOutputSchema lists the response outputs. A declared output with the
// same name replaces the implicit one.
func (n *RequestNode) ImplicitOutputSchema() []string {
	return []string{ResponseOutputBody, ResponseOutputStatus, ResponseOutputHeaders}
}

// ImplicitOutputs exposes the received response: body is the parsed JSON value
// when the Content-Type is JSON and otherwise the text; a body that is not valid
// UTF-8 text is left out. headers hold the first value of each header under its
// canonical name, as a header output reads it. A request that received no
// response exposes nothing.
func (r *RequestExecutionResult) ImplicitOutputs() map[string]any {
	if r.ResponseStatusCode == 0 {
		return nil
	}
	outputs := map[string]any{
		ResponseOutputStatus:  r.ResponseStatusCode,
		ResponseOutputHeaders: firstHeaderValues(r.ResponseHeaders),
	}
	if text, isText := r.ResponseBodyParsed.(string); !isText || utf8.ValidString(text) {
		outputs[ResponseOutputBody] = r.ResponseBodyParsed
	}
	return outputs
}

func firstHeaderValues(headers map[string][]string) map[string]any {
	values := make(map[string]any, len(headers))
	for name, headerValues := range headers {
		if len(headerValues) > 0 {
			values[http.CanonicalHeaderKey(name)] = headerValues[0]
		}
	}
	return values
}
