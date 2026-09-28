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

const (
	bits4   = 4
	bits7   = 7
	bits8   = 8
	bits10  = 10
	bits12  = 12
	bits15  = 15
	bits16  = 16
	bits20  = 20
	bits24  = 24
	bits28  = 28
	bits32  = 32
	bits48  = 48
	bits64  = 64
	bits96  = 96
	bits128 = 128
)

// reservedNets maps a prefix length to the network addresses of that length.
// Local masks the IP to each used length and reads that map. The table is not rebuilt.
// The blocks are the IANA special-purpose list:
// https://en.wikipedia.org/wiki/Reserved_IP_addresses
//
//nolint:gochecknoglobals // the table is read on every dial and must not be rebuilt
var reservedNets = struct {
	v4 [bits32 + 1]map[netip.Addr]struct{}
	v6 [bits128 + 1]map[netip.Addr]struct{}
}{
	v4: [bits32 + 1]map[netip.Addr]struct{}{
		bits4: {
			network("224.0.0.0/4"): {},
			network("240.0.0.0/4"): {},
		},
		bits8: {
			network("0.0.0.0/8"):   {},
			network("10.0.0.0/8"):  {},
			network("127.0.0.0/8"): {},
		},
		bits10: {
			network("100.64.0.0/10"): {},
		},
		bits12: {
			network("172.16.0.0/12"): {},
		},
		bits15: {
			network("198.18.0.0/15"): {},
		},
		bits16: {
			network("169.254.0.0/16"): {},
			network("192.168.0.0/16"): {},
		},
		bits24: {
			network("192.0.0.0/24"):    {},
			network("192.0.2.0/24"):    {},
			network("192.88.99.0/24"):  {},
			network("198.51.100.0/24"): {},
			network("203.0.113.0/24"):  {},
		},
	},
	v6: [bits128 + 1]map[netip.Addr]struct{}{
		bits7: {
			network("fc00::/7"): {},
		},
		bits8: {
			network("ff00::/8"): {},
		},
		bits10: {
			network("fe80::/10"): {},
		},
		bits16: {
			network("2002::/16"): {},
			network("5f00::/16"): {},
		},
		bits20: {
			network("3fff::/20"): {},
		},
		bits28: {
			network("2001:20::/28"): {},
		},
		bits32: {
			network("2001::/32"):     {},
			network("2001:db8::/32"): {},
		},
		bits48: {
			network("64:ff9b:1::/48"): {},
		},
		bits64: {
			network("100::/64"): {},
		},
		bits96: {
			network("64:ff9b::/96"): {},
		},
		bits128: {
			network("::/128"):  {},
			network("::1/128"): {},
		},
	},
}

func network(cidr string) netip.Addr {
	return netip.MustParsePrefix(cidr).Addr()
}

// Local reports whether ip is in a reserved address block.
//
// 10.1.2.3 and 203.0.113.5 are reserved. 1.1.1.1 is not.
// ::ffff:10.1.2.3 is reserved. ::ffff:1.1.1.1 is not.
func Local(ip netip.Addr) bool {
	if !ip.IsValid() {
		return true
	}
	ip = ip.Unmap()
	if ip.Is4() {
		return at(reservedNets.v4[bits4], ip, bits4) ||
			at(reservedNets.v4[bits8], ip, bits8) ||
			at(reservedNets.v4[bits10], ip, bits10) ||
			at(reservedNets.v4[bits12], ip, bits12) ||
			at(reservedNets.v4[bits15], ip, bits15) ||
			at(reservedNets.v4[bits16], ip, bits16) ||
			at(reservedNets.v4[bits24], ip, bits24)
	}
	return at(reservedNets.v6[bits7], ip, bits7) ||
		at(reservedNets.v6[bits8], ip, bits8) ||
		at(reservedNets.v6[bits10], ip, bits10) ||
		at(reservedNets.v6[bits16], ip, bits16) ||
		at(reservedNets.v6[bits20], ip, bits20) ||
		at(reservedNets.v6[bits28], ip, bits28) ||
		at(reservedNets.v6[bits32], ip, bits32) ||
		at(reservedNets.v6[bits48], ip, bits48) ||
		at(reservedNets.v6[bits64], ip, bits64) ||
		at(reservedNets.v6[bits96], ip, bits96) ||
		at(reservedNets.v6[bits128], ip, bits128)
}

func at(networks map[netip.Addr]struct{}, ip netip.Addr, bits int) bool {
	_, ok := networks[netip.PrefixFrom(ip, bits).Masked().Addr()]
	return ok
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
