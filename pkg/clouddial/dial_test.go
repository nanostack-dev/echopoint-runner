package clouddial_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/clouddial"
	"github.com/stretchr/testify/require"
)

func TestLocalRanges(t *testing.T) {
	local := []string{
		"127.0.0.1",
		"10.1.2.3",
		"172.16.5.5",
		"192.168.1.9",
		"169.254.1.1",
		"100.64.0.1",
		"100.127.255.254",
		"0.0.0.0",
		"::1",
		"fd00::1",
		"fe80::1",
		"::ffff:10.1.2.3",
		"::ffff:127.0.0.1",
	}
	for _, raw := range local {
		t.Run(raw, func(t *testing.T) {
			require.True(t, clouddial.Local(netip.MustParseAddr(raw)))
		})
	}

	public := []string{"1.1.1.1", "8.8.8.8", "93.184.216.34", "2606:4700:4700::1111"}
	for _, raw := range public {
		t.Run(raw, func(t *testing.T) {
			require.False(t, clouddial.Local(netip.MustParseAddr(raw)))
		})
	}
}

func TestClientRefusesLocalServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("local server received a cloud request")
	}))
	t.Cleanup(server.Close)

	_, err := clouddial.Client().Get(server.URL)

	require.ErrorIs(t, err, clouddial.ErrLocalAddress)
}

func TestClientRefusesRedirectToLocalAddress(t *testing.T) {
	client := clouddial.Client()
	local, err := http.NewRequest(http.MethodGet, "http://10.1.2.3/status", nil)
	require.NoError(t, err)
	require.ErrorIs(t, client.CheckRedirect(local, nil), clouddial.ErrLocalAddress)

	mapped, err := http.NewRequest(http.MethodGet, "http://[::ffff:192.168.0.1]/", nil)
	require.NoError(t, err)
	require.ErrorIs(t, client.CheckRedirect(mapped, nil), clouddial.ErrLocalAddress)

	public, err := http.NewRequest(http.MethodGet, "http://1.1.1.1/health", nil)
	require.NoError(t, err)
	require.NoError(t, client.CheckRedirect(public, nil))

	via := make([]*http.Request, 10)
	require.Error(t, client.CheckRedirect(public, via))
}

func TestClientAllowsFollowWhenRedirectStaysPublic(t *testing.T) {
	client := clouddial.Client()
	next, err := http.NewRequest(http.MethodGet, "https://1.0.0.1/next", nil)
	require.NoError(t, err)
	require.NoError(t, client.CheckRedirect(next, []*http.Request{{}}))
}
