// Package clouddial blocks Cloud job HTTP from local address ranges.
package clouddial

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"time"
)

const (
	cloudJobIDEnv = "CLOUD_JOB_ID"
	maxRedirects  = 10
	dialTimeout   = 30 * time.Second
	dialKeepAlive = 30 * time.Second
)

// ErrLocalAddress is returned when a Cloud job dials a local address range.
var ErrLocalAddress = errors.New("cloud job refused a local address")

// Job reports whether this process is a Cloud job.
// The Cloud worker sets CLOUD_JOB_ID before it runs the flow. No extra setting is required.
func Job() bool {
	return os.Getenv(cloudJobIDEnv) != ""
}

// Client is the HTTP client for a Cloud job.
// It refuses loopback, private, link-local, unspecified, and shared (100.64.0.0/10) addresses.
//
// 10.1.2.3 is refused. 1.1.1.1 is allowed. A redirect to 192.168.0.1 is refused.
// ::ffff:127.0.0.1 is refused.
func Client() *http.Client {
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: dialKeepAlive}
	transport := cloneDefaultTransport()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialChecked(ctx, dialer, network, addr)
	}
	return &http.Client{
		Transport:     transport,
		CheckRedirect: checkRedirect,
	}
}

// Local reports whether ip is in a local address range.
func Local(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	return ip.Is4() && netip.MustParsePrefix("100.64.0.0/10").Contains(ip)
}

func dialChecked(ctx context.Context, dialer *net.Dialer, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("cloud dial address: %w", err)
	}
	ips, err := hostIPs(ctx, host)
	if err != nil {
		return nil, err
	}
	ip, err := pick(network, ips)
	if err != nil {
		return nil, err
	}
	return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errors.New("stopped after 10 redirects")
	}
	_, err := hostIPs(req.Context(), req.URL.Hostname())
	return err
}

func hostIPs(ctx context.Context, host string) ([]netip.Addr, error) {
	if host == "" {
		return nil, fmt.Errorf("%w: empty host", ErrLocalAddress)
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("cloud dial lookup %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("cloud dial lookup %s: no addresses", host)
	}
	if slices.ContainsFunc(ips, Local) {
		return nil, fmt.Errorf("%w: %s", ErrLocalAddress, host)
	}
	return ips, nil
}

func pick(network string, ips []netip.Addr) (netip.Addr, error) {
	for _, ip := range ips {
		ip = ip.Unmap()
		if network == "tcp4" && !ip.Is4() {
			continue
		}
		if network == "tcp6" && !ip.Is6() {
			continue
		}
		return ip, nil
	}
	return netip.Addr{}, fmt.Errorf("cloud dial found no %s address", network)
}

func cloneDefaultTransport() *http.Transport {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{}
	}
	return base.Clone()
}
