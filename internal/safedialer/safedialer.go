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

type (
	lookupFunc = func(ctx context.Context, host string) ([]net.IPAddr, error)
	dialFunc   = func(d *net.Dialer, ctx context.Context, network, addr string) (net.Conn, error)
)

// Test hooks. Overridden in tests to drive resolution and the underlying TCP
// connect deterministically (real DNS/sockets are otherwise required to
// exercise the resolve-then-dial path). Defaults call the real network. Each
// NewSafeDialContext captures these into locals at construction, so the racing
// dial goroutines never read the package vars directly (a test restoring a hook
// after the call returns can't data-race with a lingering goroutine).
var (
	lookupIPAddr lookupFunc = net.DefaultResolver.LookupIPAddr
	dialIP       dialFunc   = func(d *net.Dialer, ctx context.Context, network, addr string) (net.Conn, error) {
		return d.DialContext(ctx, network, addr)
	}
)

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
// IP addresses, rejects any that fall within private/loopback/link-local
// ranges, and then connects to one of the exact IPs it validated. Pinning the
// connection to a validated IP (rather than re-resolving the hostname) is what
// makes this resistant to DNS rebinding: there is no second lookup between the
// safety check and the TCP connection for an attacker to poison.
func NewSafeDialContext(timeout, keepAlive time.Duration) func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: keepAlive,
	}
	// Capture the hooks once so the racing dial goroutines close over stable
	// function values instead of re-reading the package vars.
	lookup, dial := lookupIPAddr, dialIP

	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("safedialer: split host/port: %w", err)
		}

		ips, err := lookup(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("safedialer: resolve %q: %w", host, err)
		}

		for _, ipAddr := range ips {
			if isBlocked(ipAddr.IP) {
				return nil, fmt.Errorf("safedialer: %w: %s resolves to %s", ErrBlockedAddress, host, ipAddr.IP)
			}
		}

		// Dial the exact IPs we just validated — never re-pass the hostname to the
		// dialer. Handing the hostname back would trigger a second, independent DNS
		// resolution inside net.Dialer, opening a rebinding window: an attacker with
		// a short-TTL record can return a public IP for the validation lookup and a
		// blocked one (169.254.169.254, 127.0.0.1, RFC-1918) for the connect lookup.
		// TLS verification is unaffected — the transport takes its ServerName from
		// the request URL, not from the address we connect to.
		conn, err := dialValidated(ctx, dial, dialer, network, port, ips)
		if err != nil {
			return nil, fmt.Errorf("safedialer: dial %q: %w", host, err)
		}
		return conn, nil
	}
}

// dialValidated connects to the pre-validated IPs, racing them concurrently and
// returning the first established connection (any later successes are closed).
// Racing — rather than dialing serially — preserves the Happy-Eyeballs-like
// latency the old single-hostname dial provided: a dual-stack host whose IPv6
// address is black-holed no longer stalls the whole per-call timeout before
// falling back to IPv4. Every address raced is one that already passed the
// block-list check, so the SSRF guarantee is unchanged.
func dialValidated(ctx context.Context, dial dialFunc, dialer *net.Dialer, network, port string, ips []net.IPAddr) (net.Conn, error) {
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses resolved")
	}

	// Cancelling this context aborts the still-racing dials once we have a
	// winner; it does not affect an already-established connection.
	dialCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Buffered to len(ips) so no dial goroutine can block on send after we
	// return — the drain goroutine below empties whatever is left.
	results := make(chan dialOutcome, len(ips))
	for _, ipAddr := range ips {
		go func(addr string) {
			conn, err := dial(dialer, dialCtx, network, addr)
			results <- dialOutcome{conn: conn, err: err}
		}(net.JoinHostPort(ipAddr.IP.String(), port))
	}

	var lastErr error
	for i := 0; i < len(ips); i++ {
		r := <-results
		if r.err == nil {
			// Winner. Close any connections that land after cancel in the
			// background so no socket leaks.
			go drainAndClose(results, len(ips)-i-1)
			return r.conn, nil
		}
		lastErr = r.err
	}
	return nil, lastErr
}

type dialOutcome struct {
	conn net.Conn
	err  error
}

// drainAndClose consumes n remaining dial results, closing any connections that
// still succeeded despite the context cancellation.
func drainAndClose(results <-chan dialOutcome, n int) {
	for i := 0; i < n; i++ {
		r := <-results
		if r.conn != nil {
			_ = r.conn.Close()
		}
	}
}
