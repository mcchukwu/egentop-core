package config

// Unit-level verification of the RATE_LIMIT_GENERAL_PER_MIN wiring: the
// general per-IP limiter must default to 100/min when unset, honor a valid
// numeric override, reject values below the 20/min floor at Validate (the
// startup gate), and abort Load on a non-numeric value (matching the JWT TTL
// parsing behavior).

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestLoadGeneralRateLimitDefaultsTo100: RATE_LIMIT_GENERAL_PER_MIN unset
// (or empty) falls back to the documented default of 100.
func TestLoadGeneralRateLimitDefaultsTo100(t *testing.T) {
	t.Setenv("RATE_LIMIT_GENERAL_PER_MIN", "")
	if got := Load().GeneralRateLimitPerMin; got != 100 {
		t.Fatalf("unset RATE_LIMIT_GENERAL_PER_MIN = %d, want default 100", got)
	}
}

// TestLoadGeneralRateLimitAcceptsValidOverride: a numeric value is parsed
// verbatim.
func TestLoadGeneralRateLimitAcceptsValidOverride(t *testing.T) {
	t.Setenv("RATE_LIMIT_GENERAL_PER_MIN", "600")
	if got := Load().GeneralRateLimitPerMin; got != 600 {
		t.Fatalf("RATE_LIMIT_GENERAL_PER_MIN=600 parsed as %d, want 600", got)
	}
}

// TestValidateRejectsGeneralRateLimitBelowFloor: a value under the 20/min
// floor fails startup validation with a clear error naming the floor, while
// exactly the floor value is accepted.
func TestValidateRejectsGeneralRateLimitBelowFloor(t *testing.T) {
	// A fully valid env so the only failing check is the rate-limit floor.
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_PORT", "8080")
	t.Setenv("DB_URL", "postgres://user:pass@localhost:5432/egentop")
	t.Setenv("JWT_SECRET", strings.Repeat("s", 32))
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000")

	t.Setenv("RATE_LIMIT_GENERAL_PER_MIN", "19")
	cfg := Load()
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() accepted 19/min, want floor rejection")
	}
	if !strings.Contains(err.Error(), "general rate limit") || !strings.Contains(err.Error(), "20") {
		t.Fatalf("floor error = %q, want a clear message naming the 20/min floor", err)
	}

	// Boundary: exactly the floor is valid.
	t.Setenv("RATE_LIMIT_GENERAL_PER_MIN", "20")
	if err := Load().Validate(); err != nil {
		t.Fatalf("Validate() rejected 20/min (the floor), want acceptance; got: %v", err)
	}
}

// TestLoadRejectsNonNumericGeneralRateLimit: a non-numeric value must abort
// startup via log.Fatal (exit code 1) rather than silently defaulting.
func TestLoadRejectsNonNumericGeneralRateLimit(t *testing.T) {
	if os.Getenv("RATE_LIMIT_CRASH_PROBE") == "1" {
		t.Setenv("RATE_LIMIT_GENERAL_PER_MIN", "abc")
		Load() // must not return
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestLoadRejectsNonNumericGeneralRateLimit$")
	cmd.Env = append(os.Environ(), "RATE_LIMIT_CRASH_PROBE=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("Load() with RATE_LIMIT_GENERAL_PER_MIN=abc did not exit nonzero")
	}
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
		t.Fatalf("Load() with a non-numeric value exited with %v, want exit code 1", err)
	}
}
