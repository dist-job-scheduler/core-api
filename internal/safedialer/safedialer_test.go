package safedialer

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestIsBlocked(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		blocked bool
	}{
		// IPv4 blocked
		{"loopback", "127.0.0.1", true},
		{"loopback high", "127.255.255.254", true},
		{"rfc1918 10", "10.0.0.1", true},
		{"rfc1918 172.16", "172.16.0.1", true},
		{"rfc1918 172.31", "172.31.255.255", true},
		{"rfc1918 192.168", "192.168.1.1", true},
		{"link-local", "169.254.169.254", true},
		{"cgnat", "100.64.0.1", true},
		{"current network", "0.0.0.1", true},

		// IPv4 allowed
		{"public 8.8.8.8", "8.8.8.8", false},
		{"public 1.1.1.1", "1.1.1.1", false},
		{"public 93.184.216.34", "93.184.216.34", false},
		{"172.15 is public", "172.15.255.255", false},
		{"172.32 is public", "172.32.0.1", false},
		{"100.63 is public", "100.63.255.255", false},
		{"100.128 is public", "100.128.0.1", false},

		// IPv6 blocked
		{"ipv6 loopback", "::1", true},
		{"ipv6 unique local", "fd00::1", true},
		{"ipv6 link-local", "fe80::1", true},

		// IPv6 allowed
		{"ipv6 public", "2606:4700:4700::1111", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("invalid test IP: %s", tt.ip)
			}
			if got := isBlocked(ip); got != tt.blocked {
				t.Errorf("isBlocked(%s) = %v, want %v", tt.ip, got, tt.blocked)
			}
		})
	}
}

// withHooks swaps the resolver/dialer test hooks for the duration of a test.
func withHooks(t *testing.T, lookup func(context.Context, string) ([]net.IPAddr, error), dial func(*net.Dialer, context.Context, string, string) (net.Conn, error)) {
	t.Helper()
	origLookup, origDial := lookupIPAddr, dialIP
	lookupIPAddr, dialIP = lookup, dial
	t.Cleanup(func() { lookupIPAddr, dialIP = origLookup, origDial })
}

// TestSafeDial_ConnectsToValidatedIP is the core DNS-rebinding regression test:
// the address handed to the low-level dialer must be the exact IP that passed
// validation, never the hostname (which would trigger a second, poisonable
// lookup).
func TestSafeDial_ConnectsToValidatedIP(t *testing.T) {
	const publicIP = "93.184.216.34"
	var dialedAddr string

	withHooks(t,
		func(_ context.Context, host string) ([]net.IPAddr, error) {
			if host != "example.com" {
				t.Fatalf("unexpected lookup host %q", host)
			}
			return []net.IPAddr{{IP: net.ParseIP(publicIP)}}, nil
		},
		func(_ *net.Dialer, _ context.Context, _, addr string) (net.Conn, error) {
			dialedAddr = addr
			return nil, errors.New("dial stubbed") // we only assert the target
		},
	)

	dial := NewSafeDialContext(time.Second, time.Second)
	_, _ = dial(context.Background(), "tcp", "example.com:443")

	if want := net.JoinHostPort(publicIP, "443"); dialedAddr != want {
		t.Fatalf("dialed %q, want the validated IP %q (hostname re-resolution = rebinding hole)", dialedAddr, want)
	}
}

// TestSafeDial_RejectsRebindToBlockedIP: even if the hostname looks benign, a
// resolution that includes a blocked IP is refused before any connection.
func TestSafeDial_RejectsRebindToBlockedIP(t *testing.T) {
	dialCalled := false
	withHooks(t,
		func(_ context.Context, _ string) ([]net.IPAddr, error) {
			// Simulates a rebinding record resolving to the cloud metadata IP.
			return []net.IPAddr{{IP: net.ParseIP("169.254.169.254")}}, nil
		},
		func(_ *net.Dialer, _ context.Context, _, addr string) (net.Conn, error) {
			dialCalled = true
			return nil, nil
		},
	)

	dial := NewSafeDialContext(time.Second, time.Second)
	_, err := dial(context.Background(), "tcp", "rebind.evil.example:80")

	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("expected ErrBlockedAddress, got %v", err)
	}
	if dialCalled {
		t.Fatal("dialer was called for a blocked address — must reject before connecting")
	}
}
