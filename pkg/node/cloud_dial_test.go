package node_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/stretchr/testify/require"
)

func TestNodeHTTPClientRefusesLoopbackWhenGuardOn(t *testing.T) {
	t.Setenv(node.CloudLocalDialGuardEnvForTest(), "1")
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("loopback server received a cloud request")
	}))
	t.Cleanup(server.Close)

	_, err := node.NodeHTTPClientForTest().Get(server.URL)

	require.ErrorIs(t, err, node.CloudLocalAddressErrorForTest())
}

func TestNodeHTTPClientAllowsLoopbackWhenGuardOff(t *testing.T) {
	t.Setenv(node.CloudLocalDialGuardEnvForTest(), "")
	hit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	response, err := node.NodeHTTPClientForTest().Get(server.URL)

	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	_, _ = io.Copy(io.Discard, response.Body)
	require.True(t, hit)
}

func TestDialGuardRefusesLocalTargets(t *testing.T) {
	own := netip.MustParseAddr("10.0.0.5")
	tests := []struct {
		name string
		host string
		ips  []netip.Addr
	}{
		{name: "loopback", host: "127.0.0.1", ips: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		{name: "ipv6 loopback", host: "::1", ips: []netip.Addr{netip.MustParseAddr("::1")}},
		{name: "mapped loopback", host: "127.0.0.1", ips: []netip.Addr{netip.MustParseAddr("::ffff:127.0.0.1")}},
		{name: "unspecified", host: "0.0.0.0", ips: []netip.Addr{netip.MustParseAddr("0.0.0.0")}},
		{name: "link local", host: "169.254.1.1", ips: []netip.Addr{netip.MustParseAddr("169.254.1.1")}},
		{name: "this machine", host: "10.0.0.5", ips: []netip.Addr{own}},
		{
			name: "public name also returns loopback",
			host: "example.com",
			ips: []netip.Addr{
				netip.MustParseAddr("93.184.216.34"),
				netip.MustParseAddr("127.0.0.1"),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dialed := false
			guard := node.CloudDialHooksForTest{
				IPs:   test.ips,
				Local: []netip.Addr{own},
				Dial:  node.StoppedDialForTest(func(string) { dialed = true }),
			}

			_, err := guard.DialContext(context.Background(), "tcp", net.JoinHostPort(test.host, "8080"))

			require.ErrorIs(t, err, node.CloudLocalAddressErrorForTest())
			require.False(t, dialed)
		})
	}
}

func TestDialGuardPinsThePublicAddress(t *testing.T) {
	var dialed string
	guard := node.CloudDialHooksForTest{
		IPs:  []netip.Addr{netip.MustParseAddr("93.184.216.34")},
		Dial: node.StoppedDialForTest(func(addr string) { dialed = addr }),
	}

	_, err := guard.DialContext(context.Background(), "tcp", "example.com:443")

	require.Error(t, err)
	require.Equal(t, "93.184.216.34:443", dialed)
}

func TestDialGuardRedirect(t *testing.T) {
	public := node.CloudDialHooksForTest{IPs: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
	request, err := http.NewRequest(http.MethodGet, "https://example.com/next", nil)
	require.NoError(t, err)
	require.NoError(t, public.CheckRedirect(request, nil))

	local := node.CloudDialHooksForTest{IPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}
	loopback, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8080/status", nil)
	require.NoError(t, err)
	require.ErrorIs(t, local.CheckRedirect(loopback, nil), node.CloudLocalAddressErrorForTest())

	via := make([]*http.Request, node.CloudDialMaxRedirectsForTest())
	require.Error(t, public.CheckRedirect(request, via))
}

func TestDialGuardFailsClosedWhenLocalAddressesAreUnknown(t *testing.T) {
	guard := node.CloudDialHooksForTest{
		IPs:      []netip.Addr{netip.MustParseAddr("93.184.216.34")},
		LocalErr: errors.New("interfaces unavailable"),
		Dial: func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("dialed without a local address list")
			return nil, errors.New("dialed")
		},
	}

	_, err := guard.DialContext(context.Background(), "tcp", "example.com:443")

	require.Error(t, err)
	require.NotErrorIs(t, err, node.CloudLocalAddressErrorForTest())
}
