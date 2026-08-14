package middleware

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetClientIPNeverTrustsXForwardedFor: a forged X-Forwarded-For must
// never influence the rate-limit key, regardless of X-Real-IP state.
func TestGetClientIPNeverTrustsXForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/health", nil)
	req.RemoteAddr = "203.0.113.10:45678"
	req.Header.Set("X-Forwarded-For", "198.51.100.1, 10.0.0.1")
	req.Header.Set("X-Real-IP", "203.0.113.10")

	// X-Real-IP is present and valid -> used, forged XFF ignored.
	if got := getClientIP(req); got != "203.0.113.10" {
		t.Fatalf("getClientIP = %q, want X-Real-IP 203.0.113.10 (XFF must be ignored)", got)
	}

	// X-Real-IP absent -> RemoteAddr host, XFF still ignored.
	req.Header.Del("X-Real-IP")
	if got := getClientIP(req); got != "203.0.113.10" {
		t.Fatalf("getClientIP = %q, want RemoteAddr host 203.0.113.10 (XFF must be ignored)", got)
	}
}

func TestGetClientIPFallsBackToRemoteAddr(t *testing.T) {
	// No X-Real-IP at all -> RemoteAddr host.
	req := httptest.NewRequest("GET", "/v1/health", nil)
	req.RemoteAddr = "192.0.2.55:12345"
	if got := getClientIP(req); got != "192.0.2.55" {
		t.Fatalf("getClientIP = %q, want 192.0.2.55", got)
	}

	// Invalid X-Real-IP (not an IP) -> RemoteAddr host.
	req.Header.Set("X-Real-IP", "not-an-ip")
	if got := getClientIP(req); got != "192.0.2.55" {
		t.Fatalf("getClientIP with invalid X-Real-IP = %q, want 192.0.2.55", got)
	}

	// Empty X-Real-IP -> RemoteAddr host.
	req.Header.Set("X-Real-IP", "  ")
	if got := getClientIP(req); got != "192.0.2.55" {
		t.Fatalf("getClientIP with blank X-Real-IP = %q, want 192.0.2.55", got)
	}
}

func TestGetClientIPUsesValidXRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/health", nil)
	req.RemoteAddr = "203.0.113.10:45678"
	req.Header.Set("X-Real-IP", "198.51.100.77")

	if got := getClientIP(req); got != "198.51.100.77" {
		t.Fatalf("getClientIP = %q, want X-Real-IP 198.51.100.77", got)
	}

	// IPv6 X-Real-IP is accepted as a key (brackets from a host:port value
	// would not parse via net.ParseIP, so the proxy must send a bare address).
	req.Header.Set("X-Real-IP", "2001:db8::1")
	if got := getClientIP(req); got != "2001:db8::1" {
		t.Fatalf("getClientIP = %q, want IPv6 X-Real-IP 2001:db8::1", got)
	}
}

func TestGetClientIPUnparsableRemoteAddr(t *testing.T) {
	// RemoteAddr without a port -> SplitHostPort fails, raw string is the key.
	req := httptest.NewRequest("GET", "/v1/health", nil)
	req.RemoteAddr = "192.0.2.99"
	if got := getClientIP(req); got != "192.0.2.99" {
		t.Fatalf("getClientIP = %q, want raw RemoteAddr 192.0.2.99", got)
	}
}

func TestGetClientIPLengthCap(t *testing.T) {
	// A non-IP X-Real-IP is rejected, but a pathological RemoteAddr fallback
	// string is capped so the limiter map cannot be inflated.
	req := httptest.NewRequest("GET", "/v1/health", nil)
	req.RemoteAddr = strings.Repeat("a", 200)
	if got := getClientIP(req); len(got) != maxRateLimitKeyLen {
		t.Fatalf("getClientIP length = %d, want cap %d", len(got), maxRateLimitKeyLen)
	}

	// A valid (short) key is never truncated.
	req2 := httptest.NewRequest("GET", "/v1/health", nil)
	req2.RemoteAddr = "192.0.2.1:80"
	req2.Header.Set("X-Real-IP", "198.51.100.2")
	if got := getClientIP(req2); len(got) != len("198.51.100.2") {
		t.Fatalf("getClientIP truncated a short key to %q", got)
	}
}
