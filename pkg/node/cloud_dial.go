package node

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"time"
)

const cloudLocalDialGuardEnv = "ECHOPOINT_CLOUD_LOCAL_DIAL_GUARD"

const (
	cloudDialMaxRedirects = 10
	cloudDialTimeout      = 30 * time.Second
	cloudDialKeepAlive    = 30 * time.Second
)

// errCloudLocalAddress is returned when a Cloud job tries to dial this machine.
var errCloudLocalAddress = errors.New("cloud egress refused a local address")

type dialGuard struct {
	lookupIP func(ctx context.Context, host string) ([]netip.Addr, error)
	localIPs func() ([]netip.Addr, error)
	dial     func(ctx context.Context, network, addr string) (net.Conn, error)
}

func nodeHTTPClient() *http.Client {
	if os.Getenv(cloudLocalDialGuardEnv) != "1" {
		return &http.Client{}
	}
	return guardedHTTPClient(defaultDialGuard())
}

func defaultDialGuard() dialGuard {
	dialer := &net.Dialer{
		Timeout:   cloudDialTimeout,
		KeepAlive: cloudDialKeepAlive,
	}
	return dialGuard{
		lookupIP: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
		localIPs: hostLocalIPs,
		dial:     dialer.DialContext,
	}
}

func guardedHTTPClient(guard dialGuard) *http.Client {
	transport := cloneDefaultTransport()
	transport.Proxy = nil
	transport.DialContext = guard.DialContext
	return &http.Client{
		Transport:     transport,
		CheckRedirect: guard.CheckRedirect,
	}
}

func cloneDefaultTransport() *http.Transport {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{}
	}
	return base.Clone()
}

func (g dialGuard) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("cloud egress address: %w", err)
	}
	ips, err := g.addresses(ctx, host)
	if err != nil {
		return nil, err
	}
	ip, err := pickDialIP(network, ips)
	if err != nil {
		return nil, err
	}
	return g.dial(ctx, network, net.JoinHostPort(ip.String(), port))
}

func (g dialGuard) CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= cloudDialMaxRedirects {
		return errors.New("stopped after 10 redirects")
	}
	_, err := g.addresses(req.Context(), req.URL.Hostname())
	return err
}

func (g dialGuard) addresses(ctx context.Context, host string) ([]netip.Addr, error) {
	if host == "" {
		return nil, fmt.Errorf("%w: empty host", errCloudLocalAddress)
	}
	ips, err := g.lookupIP(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("cloud egress lookup %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("cloud egress lookup %s: no addresses", host)
	}
	local, err := g.localIPs()
	if err != nil {
		return nil, fmt.Errorf("cloud egress local addresses: %w", err)
	}
	for _, ip := range ips {
		if cloudLocalIP(ip, local) {
			return nil, fmt.Errorf("%w: %s", errCloudLocalAddress, host)
		}
	}
	return ips, nil
}

func pickDialIP(network string, ips []netip.Addr) (netip.Addr, error) {
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
	return netip.Addr{}, fmt.Errorf("cloud egress found no %s address", network)
}

// cloudLocalIP reports whether ip belongs to this machine.
//
// Example: 127.0.0.1 is local. 93.184.216.34 is not. 10.0.0.5 is local only
// when local contains 10.0.0.5.
func cloudLocalIP(ip netip.Addr, local []netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	for _, own := range local {
		if own.Unmap() == ip {
			return true
		}
	}
	return false
}

func hostLocalIPs() ([]netip.Addr, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	local := make([]netip.Addr, 0, len(addrs))
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP == nil {
			continue
		}
		parsed, ok := netip.AddrFromSlice(ipNet.IP)
		if !ok {
			continue
		}
		parsed = parsed.Unmap()
		if !parsed.IsValid() || parsed.IsUnspecified() {
			continue
		}
		local = append(local, parsed)
	}
	return local, nil
}
