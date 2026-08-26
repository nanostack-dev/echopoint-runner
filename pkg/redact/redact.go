// Package redact masks the values of secret flow inputs in everything the
// runner reports back — node results, the flow result, and progress events.
// It is applied once, at the boundary where results leave the runner, so the
// engine keeps working with the real values while a flow runs.
package redact

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// Mask is what a secret value is replaced with.
const Mask = "***"

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

// Value masks every secret occurrence in v by scanning its JSON encoding, so no
// field of any shape can carry one through. Numbers survive the round trip as
// json.Number, keeping their exact literal form.
func (r *Redactor) Value(v any) any {
	if r == nil || v == nil {
		return v
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return v
	}
	decoder := json.NewDecoder(strings.NewReader(r.Text(string(encoded))))
	decoder.UseNumber()
	var decoded any
	if decodeErr := decoder.Decode(&decoded); decodeErr != nil {
		return v
	}
	return decoded
}

// Map is Value for a map, keeping the map type.
func (r *Redactor) Map(values map[string]any) map[string]any {
	if r == nil || values == nil {
		return values
	}
	masked, ok := r.Value(values).(map[string]any)
	if !ok {
		return values
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
