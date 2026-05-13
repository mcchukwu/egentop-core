package middleware

import "net/http"

type SecurityHeadersMiddleware struct{}

func NewSecurityHeadersMiddleware() *SecurityHeadersMiddleware {
	return &SecurityHeadersMiddleware{}
}

func (m *SecurityHeadersMiddleware) Secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent MIME sniffing
		w.Header().Set(
			"X-Content-Type-Options",
			"nosniff",
		)

		// Prevent clickjacking
		w.Header().Set(
			"X-Frame-Options",
			"DENY",
		)

		// Basic XSS protection
		w.Header().Set(
			"X-XSS-Protection",
			"1; mode=block",
		)

		// HTTPS enforcement hint
		w.Header().Set(
			"Strict-Transport-Security",
			"max-age=31536000; includeSubDomains",
		)

		// Referrer control
		w.Header().Set(
			"Referrer-Policy",
			"strict-origin-when-cross-origin",
		)

		// Disable powerful browser features
		w.Header().Set(
			"Permissions-Policy",
			"camera=(), microphone=(), geolocation=()",
		)

		next.ServeHTTP(w, r)
	})
}
