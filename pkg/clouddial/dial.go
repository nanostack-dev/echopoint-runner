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
// It refuses every reserved address block, and it returns a redirect response
// without dialing the next address.
//
// 10.1.2.3 is refused. 1.1.1.1 is allowed. A 302 from 1.1.1.1 is the result
// the flow sees. The Location target is not dialed.
func Client() *http.Client {
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: dialKeepAlive}
	transport := cloneDefaultTransport()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialChecked(ctx, dialer, network, addr)
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Local reports whether ip is in a reserved address block.
//
// The blocks are the IANA special-purpose list:
// https://en.wikipedia.org/wiki/Reserved_IP_addresses
//
// 10.1.2.3 and 203.0.113.5 are reserved. 1.1.1.1 is not.
// ::ffff:10.1.2.3 is reserved. ::ffff:1.1.1.1 is not.
func Local(ip netip.Addr) bool {
	if !ip.IsValid() {
		return true
	}
	ip = ip.Unmap()
	for _, prefix := range reservedPrefixes() {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func reservedPrefixes() []netip.Prefix {
	raw := []string{
		"0.0.0.0/8",
		"10.0.0.0/8",
		"100.64.0.0/10",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"172.16.0.0/12",
		"192.0.0.0/24",
		"192.0.2.0/24",
		"192.88.99.0/24",
		"192.168.0.0/16",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"224.0.0.0/4",
		"240.0.0.0/4",
		"::/128",
		"::1/128",
		"64:ff9b::/96",
		"64:ff9b:1::/48",
		"100::/64",
		"2001::/32",
		"2001:20::/28",
		"2001:db8::/32",
		"2002::/16",
		"3fff::/20",
		"5f00::/16",
		"fc00::/7",
		"fe80::/10",
		"ff00::/8",
	}
	prefixes := make([]netip.Prefix, 0, len(raw))
	for _, cidr := range raw {
		prefixes = append(prefixes, netip.MustParsePrefix(cidr))
	}
	return prefixes
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
