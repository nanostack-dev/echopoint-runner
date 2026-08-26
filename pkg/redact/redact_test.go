package redact_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/redact"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

func TestNew_NilWhenNoSecretValueIsUsable(t *testing.T) {
	inputs := map[string]any{"empty": "", "missing": nil}
	if redact.New(inputs, []string{"empty", "missing", "absent"}) != nil {
		t.Error("empty, nil and absent values must not produce a redactor")
	}
}

func TestNilRedactor_LeavesEverythingUntouched(t *testing.T) {
	var redactor *redact.Redactor
	if got := redactor.Text("token"); got != "token" {
		t.Errorf("Text: %q", got)
	}
	if got := redactor.Value(42); got != 42 {
		t.Errorf("Value: %v", got)
	}
	err := errors.New("token")
	if got := redactor.Error(err); !errors.Is(got, err) {
		t.Errorf("Error: %v", got)
	}
}

func TestText_MasksTheLongestSecretWhole(t *testing.T) {
	redactor := redact.New(
		map[string]any{"short": "abc", "long": "abcdef"},
		[]string{"short", "long"},
	)
	if got := redactor.Text("value=abcdef"); got != "value="+redact.Mask {
		t.Errorf("a secret containing another must be masked whole: %q", got)
	}
}

func TestValue_KeepsNumbersExact(t *testing.T) {
	redactor := redact.New(map[string]any{"token": "secret"}, []string{"token"})
	masked, ok := redactor.Value(map[string]any{"id": int64(9007199254740993)}).(map[string]any)
	if !ok {
		t.Fatal("expected a map back")
	}
	if got := masked["id"]; got != json.Number("9007199254740993") {
		t.Errorf("large integer lost precision: %v", got)
	}
}

func TestError_PreservesUserErrorCode(t *testing.T) {
	redactor := redact.New(map[string]any{"token": "secret"}, []string{"token"})
	masked := redactor.Error(spi.NewUserError("CONNECTION_FAILED", "cannot reach https://x/?t=secret", nil))

	userErr, ok := spi.AsUserError(masked)
	if !ok {
		t.Fatal("masked error must stay a UserError")
	}
	if userErr.Code != "CONNECTION_FAILED" {
		t.Errorf("code lost: %q", userErr.Code)
	}
	if userErr.Message != "cannot reach https://x/?t="+redact.Mask {
		t.Errorf("message not masked: %q", userErr.Message)
	}
}

// json.Marshal escapes &, <, >, " and \, so a secret holding any of them has no
// literal form in the encoded bytes. Masking has to run on the decoded tree.
func TestValue_MasksSecretsThatJSONEscapes(t *testing.T) {
	const secret = `p@ss&w"rd<x>\y`
	redactor := redact.New(map[string]any{"token": secret}, []string{"token"})

	masked := redactor.Value(map[string]any{
		"header": "Bearer " + secret,
		"nested": []any{map[string]any{"body": secret}},
	})

	encoded, err := json.Marshal(masked)
	if err != nil {
		t.Fatalf("masked value must stay serializable: %v", err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Errorf("escaped secret survived: %s", encoded)
	}
	if strings.Count(string(encoded), redact.Mask) != 2 {
		t.Errorf("both occurrences should carry the mask: %s", encoded)
	}
}

// A secret that also occurs as a number or as a field name must not corrupt the
// structure: masking numbers into "***" produced invalid JSON, and rewriting a
// key dropped a contract field.
func TestValue_LeavesStructureIntact(t *testing.T) {
	redactor := redact.New(map[string]any{"token": "123"}, []string{"token"})

	masked := redactor.Value(map[string]any{"123": "keep", "count": 123, "echo": "123"})

	encoded, err := json.Marshal(masked)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if decodeErr := json.Unmarshal(encoded, &decoded); decodeErr != nil {
		t.Fatalf("masked value must stay valid JSON, got %s: %v", encoded, decodeErr)
	}
	if _, ok := decoded["123"]; !ok {
		t.Errorf("a key matching the secret must survive: %s", encoded)
	}
	if decoded["count"] != float64(123) {
		t.Errorf("a number matching the secret must survive: %s", encoded)
	}
	if decoded["echo"] != redact.Mask {
		t.Errorf("the string leaf should be masked: %s", encoded)
	}
}

// response_body carries base64-encoded bytes, invisible to a plaintext scan.
func TestValue_MasksTheBase64ResponseBody(t *testing.T) {
	const secret = "sk-live-1"
	redactor := redact.New(map[string]any{"token": secret}, []string{"token"})

	body := base64.StdEncoding.EncodeToString([]byte(`{"echoed":"` + secret + `"}`))
	masked, ok := redactor.Value(map[string]any{"response_body": body}).(map[string]any)
	if !ok {
		t.Fatal("expected a map back")
	}

	encoded, ok := masked["response_body"].(string)
	if !ok {
		t.Fatalf("response_body should stay a base64 string, got %T", masked["response_body"])
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("response_body must stay decodable: %v", err)
	}
	if strings.Contains(string(decoded), secret) {
		t.Errorf("secret survived inside the encoded body: %s", decoded)
	}
	if !strings.Contains(string(decoded), redact.Mask) {
		t.Errorf("decoded body should carry the mask: %s", decoded)
	}
}

// A security seam fails closed: a value the redactor cannot inspect is replaced,
// never reported as it was.
func TestValue_FailsClosedOnAnUninspectableValue(t *testing.T) {
	redactor := redact.New(map[string]any{"token": "secret"}, []string{"token"})

	if got := redactor.Value(make(chan int)); got != redact.Mask {
		t.Errorf("uninspectable value should be masked, got %v", got)
	}
	if got := redactor.Map(map[string]any{"ch": make(chan int)}); len(got) != 0 {
		t.Errorf("uninspectable map should be dropped, got %v", got)
	}
}
