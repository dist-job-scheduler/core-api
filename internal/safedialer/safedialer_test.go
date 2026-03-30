package safedialer

import (
	"net"
	"testing"
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
