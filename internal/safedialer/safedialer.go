package safedialer

import (
	"context"
	"fmt"
	"net"
	"time"
)

// blockedCIDRs contains private, loopback, link-local, and other non-routable
// address ranges that must never be reached by user-supplied job URLs.
var blockedCIDRs []*net.IPNet

func init() {
	for _, cidr := range []string{
		// IPv4
		"0.0.0.0/8",      // current network
		"10.0.0.0/8",     // RFC 1918
		"100.64.0.0/10",  // CGNAT / shared address space
		"127.0.0.0/8",    // loopback
		"169.254.0.0/16", // link-local
		"172.16.0.0/12",  // RFC 1918
		"192.168.0.0/16", // RFC 1918

		// IPv6
		"::1/128",  // loopback
		"fc00::/7", // unique local
		"fe80::/10", // link-local
	} {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("safedialer: bad CIDR %q: %v", cidr, err))
		}
		blockedCIDRs = append(blockedCIDRs, ipNet)
	}
}

// ErrBlockedAddress is returned when a resolved IP falls within a blocked range.
var ErrBlockedAddress = fmt.Errorf("address is in a blocked network range")

// isBlocked reports whether ip falls within any blocked CIDR.
func isBlocked(ip net.IP) bool {
	for _, cidr := range blockedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// NewSafeDialContext returns a DialContext function that resolves the host to
// IP addresses and rejects any that fall within private/loopback/link-local
// ranges. This prevents SSRF attacks including DNS rebinding, because the
// check happens after DNS resolution but before the TCP connection.
func NewSafeDialContext(timeout, keepAlive time.Duration) func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: keepAlive,
	}

	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("safedialer: split host/port: %w", err)
		}

		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("safedialer: resolve %q: %w", host, err)
		}

		for _, ipAddr := range ips {
			if isBlocked(ipAddr.IP) {
				return nil, fmt.Errorf("safedialer: %w: %s resolves to %s", ErrBlockedAddress, host, ipAddr.IP)
			}
		}

		// All resolved IPs are safe; connect to the original address so the OS
		// picks the best route (and respects Happy Eyeballs for dual-stack).
		return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
	}
}
