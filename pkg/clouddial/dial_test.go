package clouddial_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/clouddial"
	"github.com/stretchr/testify/require"
)

func TestLocalRanges(t *testing.T) {
	local := []string{
		"0.1.2.3",
		"10.1.2.3",
		"100.64.0.1",
		"100.127.255.254",
		"127.0.0.1",
		"169.254.1.1",
		"172.16.5.5",
		"192.0.0.8",
		"192.0.2.1",
		"192.88.99.1",
		"192.168.1.9",
		"198.18.1.1",
		"198.51.100.10",
		"203.0.113.5",
		"224.0.0.1",
		"240.0.0.1",
		"255.255.255.255",
		"::",
		"::1",
		"64:ff9b::1",
		"64:ff9b:1::1",
		"100::1",
		"2001::1",
		"2001:20::1",
		"2001:db8::1",
		"2002::1",
		"3fff::1",
		"5f00::1",
		"fd00::1",
		"fe80::1",
		"ff02::1",
		"::ffff:10.1.2.3",
		"::ffff:203.0.113.1",
	}
	for _, raw := range local {
		t.Run(raw, func(t *testing.T) {
			require.True(t, clouddial.Local(netip.MustParseAddr(raw)))
		})
	}

	public := []string{
		"1.1.1.1",
		"8.8.8.8",
		"11.0.0.1",
		"100.128.0.1",
		"172.32.0.1",
		"192.0.3.1",
		"93.184.216.34",
		"2001:4860:4860::8888",
		"2606:4700:4700::1111",
		"::ffff:1.1.1.1",
	}
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

func TestLiveRedirectIsReturnedWithoutASecondDial(t *testing.T) {
	targetHit := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetHit = true
	}))
	t.Cleanup(target.Close)

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/hidden", http.StatusFound)
	}))
	t.Cleanup(first.Close)

	client := clouddial.Client()
	transport := client.Transport.(*http.Transport)
	original := transport.DialContext
	firstAddr := first.Listener.Addr().String()
	secondDials := 0
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == firstAddr {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}
		secondDials++
		return original(ctx, network, addr)
	}

	response, err := client.Get(first.URL)

	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	require.Equal(t, http.StatusFound, response.StatusCode)
	require.Equal(t, target.URL+"/hidden", response.Header.Get("Location"))
	require.Zero(t, secondDials)
	require.False(t, targetHit)
}
