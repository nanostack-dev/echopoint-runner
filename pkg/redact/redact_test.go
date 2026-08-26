package redact_test

import (
	"encoding/json"
	"errors"
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
