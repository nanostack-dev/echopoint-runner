package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/echopoint-runner/internal/config"
)

func TestMaxResponseBytesDefaultsToTenMebibytes(t *testing.T) {
	t.Setenv("ECHOPOINT_MAX_RESPONSE_BYTES", "")

	limit, err := config.MaxResponseBytes()

	require.NoError(t, err)
	assert.Equal(t, int64(10<<20), limit)
}

func TestMaxResponseBytesReadsTheEnvironment(t *testing.T) {
	t.Setenv("ECHOPOINT_MAX_RESPONSE_BYTES", "2097152")

	limit, err := config.MaxResponseBytes()

	require.NoError(t, err)
	assert.Equal(t, int64(2<<20), limit)
}

func TestMaxResponseBytesRejectsZero(t *testing.T) {
	t.Setenv("ECHOPOINT_MAX_RESPONSE_BYTES", "0")

	_, err := config.MaxResponseBytes()

	require.EqualError(t, err, "ECHOPOINT_MAX_RESPONSE_BYTES must be positive")
}

func TestMaxBufferedResponseBytesDefaultsTo128Mebibytes(t *testing.T) {
	t.Setenv("ECHOPOINT_MAX_BUFFERED_RESPONSE_BYTES", "")

	budget, err := config.MaxBufferedResponseBytes()

	require.NoError(t, err)
	assert.Equal(t, int64(128<<20), budget)
}

func TestMaxBufferedResponseBytesReadsTheEnvironment(t *testing.T) {
	t.Setenv("ECHOPOINT_MAX_BUFFERED_RESPONSE_BYTES", "67108864")

	budget, err := config.MaxBufferedResponseBytes()

	require.NoError(t, err)
	assert.Equal(t, int64(64<<20), budget)
}

func TestMaxBufferedResponseBytesRejectsZero(t *testing.T) {
	t.Setenv("ECHOPOINT_MAX_BUFFERED_RESPONSE_BYTES", "0")

	_, err := config.MaxBufferedResponseBytes()

	require.EqualError(t, err, "ECHOPOINT_MAX_BUFFERED_RESPONSE_BYTES must be positive")
}

func TestLoadMsBetweenRequests(t *testing.T) {
	t.Setenv("ECHOPOINT_ORGANIZATION_ID", "test-org")
	t.Setenv("ECHOPOINT_RUNNER_API_KEY", "test-key")
	t.Setenv("ECHOPOINT_RUNNER_ID", "test-runner")
	for _, tc := range []struct {
		value   string
		want    int
		invalid bool
	}{
		{"", 0, false}, {"0", 0, false}, {"100", 100, false},
		{"-1", 0, true}, {"100ms", 0, true}, {"1.5", 0, true}, {"9223372036855", 0, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("ECHOPOINT_MS_BETWEEN_REQUESTS", tc.value)
			cfg, err := config.Load()
			if tc.invalid {
				require.ErrorContains(t, err, "ECHOPOINT_MS_BETWEEN_REQUESTS")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, cfg.MsBetweenRequests)
		})
	}
}
