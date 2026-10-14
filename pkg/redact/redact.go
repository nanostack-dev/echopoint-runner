// Package redact masks the values of secret flow inputs and secret node outputs
// in everything the runner reports back — node results, the flow result, and
// progress events.
// It is applied once, at the boundary where results leave the runner, so the
// engine keeps working with the real values while a flow runs.
package redact

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

// Mask is what a secret value is replaced with.
const Mask = "***"

// responseBodyField names the one reported field carrying base64-encoded bytes
// (node.RequestExecutionResult.ResponseBody). A server echoing a secret there
// is invisible to a plaintext scan, and base64 has no offset-stable encoding to
// search for, so the field is decoded, masked, and re-encoded.
const responseBodyField = "response_body"

// MinLearnedLength is the fewest characters a secret output value needs to be
// masked by value. Shorter text, such as true, 1 or a status word, occurs
// everywhere in a run's reports, so masking it by value would wipe them out
// while protecting nothing a guess could not recover.
const MinLearnedLength = 6

// MaxLearnedValues caps the distinct secret output values one run masks by
// value. Each one is matched against every string the run reports.
const MaxLearnedValues = 500

// Redactor masks a set of secret values. A nil or zero Redactor is a valid
// no-op. LearnOutput grows the set while a run executes, so a value a node
// extracts into a secret output is masked in everything reported after it.
type Redactor struct {
	mu      sync.RWMutex
	current *secretSet
	// known holds every value in current.secrets, for constant-time
	// de-duplication; learned counts the ones LearnOutput added.
	known   map[string]struct{}
	learned int
}

// secretSet is an immutable snapshot of the values a Redactor masks. Each
// public call masks against one snapshot, so a value learned concurrently never
// leaves a single report half masked.
type secretSet struct {
	secrets []string
	// bodyForms is secrets plus, for each, the forms a JSON encoder writes it in.
	// A response body is text the target server produced, so a secret it echoed
	// back is escaped there and has no literal form.
	bodyForms []string
	// learned holds the secret output values masked by value, to match a JSON
	// number leaf, which has no text to scan, by its whole literal.
	learned map[string]struct{}
	// withheld holds the references ("node.output") of secret outputs that are
	// masked by name, because a value of theirs is too short or past the cap.
	withheld map[string]struct{}
	// sealed holds the IDs of the nodes that produced a withheld value. Their
	// reports keep only the fields that cannot carry it.
	sealed map[string]struct{}
}

// Source identifies secret-bearing values by key. Callers may provide one
// source for root inputs and additional sources for referenced-flow overrides.
type Source struct {
	Inputs     map[string]any
	SecretKeys []string
}

// New returns a Redactor for the values of the inputs named by secretKeys, or
// nil when none of them holds a non-empty value.
func New(inputs map[string]any, secretKeys []string) *Redactor {
	return NewFromSources(Source{Inputs: inputs, SecretKeys: secretKeys})
}

// NewFromSources returns one Redactor for secret values drawn from multiple
// independent input maps. Duplicate values are kept only once.
func NewFromSources(sources ...Source) *Redactor {
	var secrets []string
	known := make(map[string]struct{})
	for _, source := range sources {
		for _, key := range source.SecretKeys {
			value, present := source.Inputs[key]
			if !present || value == nil {
				continue
			}
			text := fmt.Sprintf("%v", value)
			if _, seen := known[text]; text == "" || seen {
				continue
			}
			known[text] = struct{}{}
			secrets = append(secrets, text)
		}
	}
	if len(secrets) == 0 {
		return nil
	}
	return &Redactor{current: newSecretSet(secrets, nil, nil, nil), known: known}
}

// newSecretSet orders secrets longest first: a secret that contains a shorter
// one is masked whole, instead of being left as a recognizable fragment around
// the shorter mask.
func newSecretSet(secrets []string, learned, withheld, sealed map[string]struct{}) *secretSet {
	slices.SortFunc(secrets, longestFirst)
	return &secretSet{
		secrets: secrets, bodyForms: bodyForms(secrets),
		learned: learned, withheld: withheld, sealed: sealed,
	}
}

// LearnOutput makes the value of output name of node nodeID secret. Each leaf
// of a map or slice counts on its own. A leaf of at least MinLearnedLength
// characters is masked by value from now on, up to MaxLearnedValues per run.
// Any other leaf withholds the output instead: it is masked by name wherever
// the run reports it, and its node's report drops the fields that could carry
// it. Empty text is ignored, as is a value already masked.
func (r *Redactor) LearnOutput(nodeID, name string, value any) {
	leaves := leafTexts(value, nil)
	if r == nil || len(leaves) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.known == nil {
		r.known = make(map[string]struct{})
	}
	var fresh []string
	withhold := false
	for _, text := range leaves {
		if _, seen := r.known[text]; seen {
			continue
		}
		if utf8.RuneCountInString(text) < MinLearnedLength || r.learned+len(fresh) >= MaxLearnedValues {
			withhold = true
			continue
		}
		r.known[text] = struct{}{}
		fresh = append(fresh, text)
	}
	if len(fresh) == 0 && !withhold {
		return
	}
	r.learned += len(fresh)
	r.current = r.current.with(fresh, withhold, nodeID, name)
}

// Withhold masks output name of node nodeID by name, without dropping any of
// the node's report: a module output bound to a withheld child output.
func (r *Redactor) Withhold(nodeID, name string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current = r.current.withWithheld(outputRef(nodeID, name))
}

// Withheld reports whether a reference such as "mint.pin" names an output
// masked by name.
func (r *Redactor) Withheld(ref string) bool {
	set := r.load()
	if set == nil {
		return false
	}
	_, withheld := set.withheld[ref]
	return withheld
}

// with returns a new snapshot adding fresh to the masked values and, when
// withhold is set, the output to the withheld ones and its node to the sealed.
func (set *secretSet) with(fresh []string, withhold bool, nodeID, name string) *secretSet {
	var secrets []string
	var learned, withheld, sealed map[string]struct{}
	if set != nil {
		secrets = slices.Clone(set.secrets)
		learned = maps.Clone(set.learned)
		withheld = maps.Clone(set.withheld)
		sealed = maps.Clone(set.sealed)
	}
	secrets = append(secrets, fresh...)
	for _, text := range fresh {
		learned = addKey(learned, text)
	}
	if withhold {
		withheld = addKey(withheld, outputRef(nodeID, name))
		sealed = addKey(sealed, nodeID)
	}
	return newSecretSet(secrets, learned, withheld, sealed)
}

func (set *secretSet) withWithheld(ref string) *secretSet {
	if set == nil {
		return newSecretSet(nil, nil, addKey(nil, ref), nil)
	}
	return &secretSet{
		secrets:   set.secrets,
		bodyForms: set.bodyForms,
		learned:   set.learned,
		withheld:  addKey(maps.Clone(set.withheld), ref),
		sealed:    set.sealed,
	}
}

func addKey(set map[string]struct{}, key string) map[string]struct{} {
	if set == nil {
		set = make(map[string]struct{})
	}
	set[key] = struct{}{}
	return set
}

func outputRef(nodeID, name string) string { return nodeID + "." + name }

func leafTexts(value any, into []string) []string {
	switch typed := value.(type) {
	case nil:
		return into
	case map[string]any:
		for _, item := range typed {
			into = leafTexts(item, into)
		}
		return into
	case []any:
		for _, item := range typed {
			into = leafTexts(item, into)
		}
		return into
	}
	if text := leafText(value); text != "" {
		into = append(into, text)
	}
	return into
}

// leafText writes a number or bool as JSON does, the form reports carry it in:
// fmt writes a float64 of seven or more digits in exponent form.
func leafText(value any) string {
	switch value.(type) {
	case bool, float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, json.Number:
		if encoded, err := json.Marshal(value); err == nil {
			return string(encoded)
		}
	}
	return fmt.Sprintf("%v", value)
}

func (r *Redactor) load() *secretSet {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current
}

func longestFirst(a, b string) int { return len(b) - len(a) }

// bodyForms lists every literal form a secret can take inside body text: the
// value itself, and the two forms a JSON encoder writes it in — json.Marshal
// escapes &, < and > as \u00XX, an encoder with SetEscapeHTML(false) leaves
// them alone, and both escape " and \.
func bodyForms(secrets []string) []string {
	forms := make([]string, 0, len(secrets))
	seen := make(map[string]struct{}, len(secrets))
	for _, secret := range secrets {
		for _, form := range []string{secret, jsonEscape(secret, true), jsonEscape(secret, false)} {
			if _, dup := seen[form]; !dup {
				seen[form] = struct{}{}
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
	return r.load().text(s)
}

func (set *secretSet) text(s string) string {
	if set == nil {
		return s
	}
	for _, secret := range set.secrets {
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
	return r.load().value(v)
}

func (set *secretSet) value(v any) any {
	if set == nil || v == nil {
		return v
	}
	tree, err := jsonTree(v)
	if err != nil {
		return Mask
	}
	return set.mask(tree)
}

// Map is Value for a map, keeping the map type. A value that cannot be
// normalized yields an empty map rather than the original.
func (r *Redactor) Map(values map[string]any) map[string]any {
	return r.load().mapValues(values)
}

func (set *secretSet) mapValues(values map[string]any) map[string]any {
	if set == nil || values == nil {
		return values
	}
	masked, ok := set.value(values).(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return masked
}

// Error returns err with its message masked, preserving a UserError's code so
// the engine's user-fault classification survives.
func (r *Redactor) Error(err error) error {
	return r.load().maskError(err)
}

func (set *secretSet) maskError(err error) error {
	if set == nil || err == nil {
		return err
	}
	masked := set.text(err.Error())
	if masked == err.Error() {
		return err
	}
	if userErr, ok := spi.AsUserError(err); ok {
		return spi.NewUserError(userErr.Code, set.text(userErr.Message), nil)
	}
	return errors.New(masked)
}

// mask walks a decoded JSON tree in place, masking every string leaf and every
// number equal to a learned secret output value. The tree
// is always a fresh copy produced by jsonTree, so mutating it cannot reach the
// caller's value.
func (set *secretSet) mask(v any) any {
	switch value := v.(type) {
	case string:
		return set.text(value)
	case json.Number:
		if _, learned := set.learned[value.String()]; learned {
			return Mask
		}
		return value
	case []any:
		for i, item := range value {
			value[i] = set.mask(item)
		}
		return value
	case map[string]any:
		set.seal(value)
		for key, item := range value {
			if set.refersToWithheld(key) {
				value[key] = Mask
				continue
			}
			encoded, isText := item.(string)
			if key == responseBodyField && isText {
				value[key] = set.maskBase64(encoded)
				continue
			}
			value[key] = set.mask(item)
		}
		return value
	default:
		return v
	}
}

// keptWhenSealed reports whether a node result field stays in the report of a
// node that produced a withheld value: the runner writes it itself, or it is
// masked by name.
func keptWhenSealed(field string) bool {
	switch field {
	case "node_id", "display_name", "node_type", "run_when", "inputs", "outputs",
		"error_code", "error_message", "skip_reason", "missing_inputs", "executed_at",
		"duration_ms", "response_status_code", "http_calls", "assertion_results":
		return true
	}
	return false
}

// seal reduces a node result whose node produced a withheld value to the
// fields that cannot carry it, masks that node's withheld outputs by name and
// the values its assertions compared. A withheld value cannot be found by
// value, so this fails closed: the request, response and child detail go.
func (set *secretSet) seal(fields map[string]any) {
	nodeID, isText := fields["node_id"].(string)
	if _, isResult := fields["node_type"]; !isText || !isResult {
		return
	}
	if _, sealed := set.sealed[nodeID]; !sealed {
		set.maskOutputsByName(nodeID, fields["outputs"])
		return
	}
	for key := range fields {
		if !keptWhenSealed(key) {
			delete(fields, key)
		}
	}
	set.maskOutputsByName(nodeID, fields["outputs"])
	maskComparedValues(fields["assertion_results"])
}

// maskComparedValues masks the actual and expected values of assertion results.
func maskComparedValues(results any) {
	assertions, ok := results.([]any)
	if !ok {
		return
	}
	for _, assertion := range assertions {
		compared, isObject := assertion.(map[string]any)
		if !isObject {
			continue
		}
		for _, key := range []string{"actual", "expected"} {
			if _, present := compared[key]; present {
				compared[key] = Mask
			}
		}
	}
}

// maskOutputsByName masks each withheld output of nodeID in its outputs map.
func (set *secretSet) maskOutputsByName(nodeID string, outputs any) {
	values, ok := outputs.(map[string]any)
	if !ok {
		return
	}
	for name := range values {
		if _, withheld := set.withheld[outputRef(nodeID, name)]; withheld {
			values[name] = Mask
		}
	}
}

// refersToWithheld reports whether key is a withheld reference or a path into
// one, such as "mint.pin" or "mint.pin.0": the key of a consumer's input, a
// flow's final output or a module's child_final_outputs.
func (set *secretSet) refersToWithheld(key string) bool {
	if len(set.withheld) == 0 {
		return false
	}
	for i := range len(key) {
		if key[i] != '.' {
			continue
		}
		if _, withheld := set.withheld[key[:i]]; withheld {
			return true
		}
	}
	_, withheld := set.withheld[key]
	return withheld
}

// maskBase64 masks the bytes encoded in a response_body. The encoded string is
// scanned as text as well: not every value is base64 of something, and one that
// merely fits the alphabet decodes to bytes the secret is absent from, so it
// would otherwise ship whole.
func (set *secretSet) maskBase64(encoded string) string {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return set.maskText(encoded)
	}
	masked := set.maskBody(decoded)
	if masked != string(decoded) {
		return base64.StdEncoding.EncodeToString([]byte(masked))
	}
	if set.maskText(encoded) == encoded {
		return encoded
	}
	// Masking inside the encoded text would leave a body nobody can decode, so
	// the whole body is replaced instead.
	return base64.StdEncoding.EncodeToString([]byte(Mask))
}

// maskBody masks a decoded response body, returning it unchanged when nothing
// matched so a body carrying no secret keeps its exact bytes.
//
// A body that parses as JSON is masked as a tree, for the reason Value is: the
// server wrote it through its own encoder, so a secret holding &, <, >, " or \
// has no literal form in these bytes. A body that does not parse — an event
// stream, NDJSON, a truncated response — is masked as text against every form a
// JSON encoder writes a secret in.
func (set *secretSet) maskBody(decoded []byte) string {
	if !json.Valid(decoded) {
		return set.maskText(string(decoded))
	}
	tree, err := decodeJSON(decoded)
	if err != nil {
		return set.maskText(string(decoded))
	}
	before, err := json.Marshal(tree)
	if err != nil {
		return set.maskText(string(decoded))
	}
	after, err := json.Marshal(set.mask(tree))
	if err != nil {
		return Mask
	}
	if bytes.Equal(before, after) {
		return string(decoded)
	}
	return string(after)
}

// maskText masks every form of every secret in body text.
func (set *secretSet) maskText(s string) string {
	for _, form := range set.bodyForms {
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
