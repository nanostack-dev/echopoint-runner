package secretegress_test

import (
	"net/url"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/secretegress"
	"github.com/stretchr/testify/require"
)

func TestCheckRefusesASecretInTheURL(t *testing.T) {
	err := secretegress.Check(
		map[string]string{"API_KEY": "s3cret"},
		map[string][]string{"API_KEY": {"api.example.com"}},
		"https://api.example.com/a?token=s3cret",
		nil,
		nil,
	)

	require.ErrorIs(t, err, secretegress.ErrInURL)
	require.NotContains(t, err.Error(), "s3cret")
}

func TestCheckRefusesASecretSentToAnotherHost(t *testing.T) {
	err := secretegress.Check(
		map[string]string{"API_KEY": "s3cret"},
		map[string][]string{"API_KEY": {"api.example.com"}},
		"https://evil.example/collect",
		map[string]string{"Authorization": "Bearer s3cret"},
		nil,
	)

	require.ErrorIs(t, err, secretegress.ErrHost)
	require.Contains(t, err.Error(), "evil.example")
	require.NotContains(t, err.Error(), "s3cret")
}

func TestCheckAllowsASecretHeaderForTheListedHost(t *testing.T) {
	err := secretegress.Check(
		map[string]string{"API_KEY": "s3cret"},
		map[string][]string{"API_KEY": {"API.Example.com"}},
		"https://api.example.com/v1",
		map[string]string{"Authorization": "Bearer s3cret"},
		map[string]any{"token": "s3cret"},
	)

	require.NoError(t, err)
}

func TestCheckRefusesWhenTheSecretHasNoHosts(t *testing.T) {
	err := secretegress.Check(
		map[string]string{"API_KEY": "s3cret"},
		nil,
		"https://api.example.com/v1",
		map[string]string{"Authorization": "Bearer s3cret"},
		nil,
	)

	require.ErrorIs(t, err, secretegress.ErrHost)
}

func TestPublicMessageOmitsTheRedirectURL(t *testing.T) {
	err := secretegress.Check(
		map[string]string{"API_KEY": "s3cret"},
		map[string][]string{"API_KEY": {"api.example.com"}},
		"https://evil.example/?token=s3cret",
		nil,
		nil,
	)
	wrapped := &url.Error{Op: "Get", URL: "https://evil.example/?token=s3cret", Err: err}

	message := secretegress.PublicMessage(wrapped)

	require.Contains(t, message, "API_KEY")
	require.NotContains(t, message, "s3cret")
	require.NotContains(t, message, "evil.example/?token")
}

func TestCheckIgnoresARequestWithoutSecrets(t *testing.T) {
	err := secretegress.Check(
		nil,
		nil,
		"https://evil.example",
		map[string]string{"Authorization": "Bearer s3cret"},
		nil,
	)

	require.NoError(t, err)
}
