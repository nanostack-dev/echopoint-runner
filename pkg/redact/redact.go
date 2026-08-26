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
	slices.SortFunc(secrets, func(a, b string) int { return len(b) - len(a) })
	return &Redactor{secrets: secrets}
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

func (r *Redactor) maskBase64(encoded string) string {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return r.Text(encoded)
	}
	masked := r.Text(string(decoded))
	if masked == string(decoded) {
		return encoded
	}
	return base64.StdEncoding.EncodeToString([]byte(masked))
}

// jsonTree normalizes v to its wire shape: maps, slices, strings, bools, nil
// and json.Number (which keeps a number's exact literal form).
func jsonTree(v any) (any, error) {
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var tree any
	if decodeErr := decoder.Decode(&tree); decodeErr != nil {
		return nil, decodeErr
	}
	return tree, nil
}
