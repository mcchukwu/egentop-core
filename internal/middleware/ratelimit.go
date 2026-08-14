package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type clientLimiter struct {
	requests int
	lastSeen time.Time
}

type RateLimiterMiddleware struct {
	mu sync.Mutex

	clients map[string]*clientLimiter

	maxRequests int
	window      time.Duration
}

func NewRateLimiterMiddleware(maxRequests int, window time.Duration) *RateLimiterMiddleware {
	rl := &RateLimiterMiddleware{
		clients:     make(map[string]*clientLimiter),
		maxRequests: maxRequests,
		window:      window,
	}

	go rl.cleanup()

	return rl
}

func (rl *RateLimiterMiddleware) Limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r)

		rl.mu.Lock()

		client, exists := rl.clients[ip]
		if !exists {
			client = &clientLimiter{
				requests: 0,
				lastSeen: time.Now(),
			}

			rl.clients[ip] = client
		}

		// reset window
		if time.Since(client.lastSeen) > rl.window {
			client.requests = 0
		}

		client.requests++
		client.lastSeen = time.Now()

		requests := client.requests

		rl.mu.Unlock()

		if requests > rl.maxRequests {
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiterMiddleware) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)

	for range ticker.C {
		rl.mu.Lock()

		for ip, client := range rl.clients {
			if time.Since(client.lastSeen) > 10*time.Minute {
				delete(rl.clients, ip)
			}
		}

		rl.mu.Unlock()
	}
}

// maxRateLimitKeyLen caps the rate-limit key so a malicious or malformed
// header can never grow the in-memory limiter map unboundedly (memory DoS).
const maxRateLimitKeyLen = 64

// getClientIP derives the rate-limit key. X-Forwarded-For is NEVER used: the
// deployment proxy owns that header, and a client-reachable endpoint would
// otherwise let attackers rotate the key to bypass the limit or inflate the
// limiter map. X-Real-IP is trusted only when it parses as a real IP; anything
// else falls back to the socket peer (RemoteAddr).
func getClientIP(r *http.Request) string {
	realIP := strings.TrimSpace(r.Header.Get("X-Real-IP"))
	if realIP != "" && net.ParseIP(realIP) != nil {
		return capRateLimitKey(realIP)
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	return capRateLimitKey(host)
}

// capRateLimitKey truncates an over-long key. IPs fit comfortably under the
// cap; this only guards against non-IP fallback strings.
func capRateLimitKey(key string) string {
	if len(key) > maxRateLimitKeyLen {
		return key[:maxRateLimitKeyLen]
	}
	return key
}
