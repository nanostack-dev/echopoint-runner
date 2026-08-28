// Package redact masks the values of secret flow inputs in everything the
// runner reports back — node results, the flow result, and progress events.
// It is applied once, at the boundary where results leave the runner, so the
// engine keeps working with the real values while a flow runs.
package redact

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// Mask is what a secret value is replaced with.
const Mask = "***"

// responseBodyField names the one reported field carrying base64-encoded bytes
// (node.RequestExecutionResult.ResponseBody). A server echoing a secret there
// is invisible to a plaintext scan, and base64 has no offset-stable encoding to
// search for, so the field is decoded, masked, and re-encoded.
const responseBodyField = "response_body"

// Redactor masks a fixed set of secret values. A nil Redactor is a valid no-op.
type Redactor struct {
	secrets []string
	// bodyForms is secrets plus, for each, the forms a JSON encoder writes it in.
	// A response body is text the target server produced, so a secret it echoed
	// back is escaped there and has no literal form.
	bodyForms []string
}

// New returns a Redactor for the values of the inputs named by secretKeys, or
// nil when none of them holds a non-empty value.
func New(inputs map[string]any, secretKeys []string) *Redactor {
	secrets := make([]string, 0, len(secretKeys))
	for _, key := range secretKeys {
		value, present := inputs[key]
		if !present || value == nil {
			continue
		}
		if text := fmt.Sprintf("%v", value); text != "" {
			secrets = append(secrets, text)
		}
	}
	if len(secrets) == 0 {
		return nil
	}
	// Longest first: a secret that contains a shorter one is masked whole,
	// instead of being left as a recognizable fragment around the shorter mask.
	slices.SortFunc(secrets, longestFirst)
	return &Redactor{secrets: secrets, bodyForms: bodyForms(secrets)}
}

func longestFirst(a, b string) int { return len(b) - len(a) }

// bodyForms lists every literal form a secret can take inside body text: the
// value itself, and the two forms a JSON encoder writes it in — json.Marshal
// escapes &, < and > as \u00XX, an encoder with SetEscapeHTML(false) leaves
// them alone, and both escape " and \.
func bodyForms(secrets []string) []string {
	forms := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		for _, form := range []string{secret, jsonEscape(secret, true), jsonEscape(secret, false)} {
			if !slices.Contains(forms, form) {
				forms = append(forms, form)
			}
		}
	}
	slices.SortFunc(forms, longestFirst)
	return forms
}

// jsonEscape returns how a JSON encoder writes s, without the quotes.
func jsonEscape(s string, escapeHTML bool) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(escapeHTML)
	if err := encoder.Encode(s); err != nil {
		return s
	}
	// Encode writes a quoted string followed by a newline.
	quoted := strings.TrimSuffix(buffer.String(), "\n")
	return strings.TrimSuffix(strings.TrimPrefix(quoted, `"`), `"`)
}

// Text masks every secret occurrence in s.
func (r *Redactor) Text(s string) string {
	if r == nil {
		return s
	}
	for _, secret := range r.secrets {
		s = strings.ReplaceAll(s, secret, Mask)
	}
	return s
}

// Value returns a masked copy of v.
//
// v is first normalized to the JSON shape it reports on the wire, then every
// string leaf of that tree is masked. Scanning the decoded tree rather than the
// encoded text is what makes the scan sound: json.Marshal escapes &, <, >, "
// and \, so a secret holding any of them has no literal form in the encoded
// bytes. Object keys are left alone, so no contract field can be renamed or
// dropped, and the result is always valid JSON.
//
// A value that cannot be normalized is replaced by the mask. This is a security
// seam: it fails closed.
func (r *Redactor) Value(v any) any {
	if r == nil || v == nil {
		return v
	}
	tree, err := jsonTree(v)
	if err != nil {
		return Mask
	}
	return r.mask(tree)
}

// Map is Value for a map, keeping the map type. A value that cannot be
// normalized yields an empty map rather than the original.
func (r *Redactor) Map(values map[string]any) map[string]any {
	if r == nil || values == nil {
		return values
	}
	masked, ok := r.Value(values).(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return masked
}

// Error returns err with its message masked, preserving a UserError's code so
// the engine's user-fault classification survives.
func (r *Redactor) Error(err error) error {
	if r == nil || err == nil {
		return err
	}
	masked := r.Text(err.Error())
	if masked == err.Error() {
		return err
	}
	if userErr, ok := spi.AsUserError(err); ok {
		return spi.NewUserError(userErr.Code, r.Text(userErr.Message), nil)
	}
	return errors.New(masked)
}

// mask walks a decoded JSON tree in place, masking every string leaf. The tree
// is always a fresh copy produced by jsonTree, so mutating it cannot reach the
// caller's value.
func (r *Redactor) mask(v any) any {
	switch value := v.(type) {
	case string:
		return r.Text(value)
	case []any:
		for i, item := range value {
			value[i] = r.mask(item)
		}
		return value
	case map[string]any:
		for key, item := range value {
			encoded, isText := item.(string)
			if key == responseBodyField && isText {
				value[key] = r.maskBase64(encoded)
				continue
			}
			value[key] = r.mask(item)
		}
		return value
	default:
		return v
	}
}

// maskBase64 masks the bytes encoded in a response_body. The encoded string is
// scanned as text as well: not every value is base64 of something, and one that
// merely fits the alphabet decodes to bytes the secret is absent from, so it
// would otherwise ship whole.
func (r *Redactor) maskBase64(encoded string) string {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return r.maskText(encoded)
	}
	masked := r.maskBody(decoded)
	if masked == string(decoded) {
		return r.maskText(encoded)
	}
	return base64.StdEncoding.EncodeToString([]byte(masked))
}

// maskBody masks a decoded response body, returning it unchanged when nothing
// matched so a body carrying no secret keeps its exact bytes.
//
// A body that parses as JSON is masked as a tree, for the reason Value is: the
// server wrote it through its own encoder, so a secret holding &, <, >, " or \
// has no literal form in these bytes. A body that does not parse — an event
// stream, NDJSON, a truncated response — is masked as text against every form a
// JSON encoder writes a secret in.
func (r *Redactor) maskBody(decoded []byte) string {
	if !json.Valid(decoded) {
		return r.maskText(string(decoded))
	}
	tree, err := decodeJSON(decoded)
	if err != nil {
		return r.maskText(string(decoded))
	}
	before, err := json.Marshal(tree)
	if err != nil {
		return r.maskText(string(decoded))
	}
	after, err := json.Marshal(r.mask(tree))
	if err != nil {
		return Mask
	}
	if bytes.Equal(before, after) {
		return string(decoded)
	}
	return string(after)
}

// maskText masks every form of every secret in body text.
func (r *Redactor) maskText(s string) string {
	for _, form := range r.bodyForms {
		s = strings.ReplaceAll(s, form, Mask)
	}
	return s
}

// jsonTree normalizes v to its wire shape: maps, slices, strings, bools, nil
// and json.Number (which keeps a number's exact literal form).
func jsonTree(v any) (any, error) {
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return decodeJSON(encoded)
}

func decodeJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var tree any
	if err := decoder.Decode(&tree); err != nil {
		return nil, err
	}
	return tree, nil
}
