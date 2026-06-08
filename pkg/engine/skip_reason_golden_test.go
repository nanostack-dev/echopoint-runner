//nolint:testpackage // white-box: locks the unexported skip-reason wire constants
package engine

import "testing"

// The skip_reason codes are a wire contract: echopoint persists them and the
// dashboards/root-cause UI switch on the exact strings. Renaming a constant is a
// silent breaking change, so lock the literal values here. If you intentionally
// change one, update echopoint's consumers (and this test) in the same change.
func TestSkipReasonCodes_Golden(t *testing.T) {
	golden := map[string]string{
		"dependency_failed":              skipReasonDependencyFailed,
		"dependency_skipped":             skipReasonDependencySkipped,
		"missing_inputs":                 skipReasonMissingInputs,
		"aborted_after_failure":          skipReasonAbortedAfterFail,
		"not_reachable_after_main_phase": skipReasonNotReachable,
	}
	for want, got := range golden {
		if got != want {
			t.Errorf("skip reason code drift: got %q, want %q", got, want)
		}
	}
}
