package logger

// Unit-level verification of the LOG_LEVEL wiring: SetLevel must act as a
// package-wide threshold that filters Info/Warn/Error, with unknown values
// defaulting to the info threshold (the documented default).

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// captureLogs redirects the default logger output to a buffer for the
// duration of fn and returns what was written.
func captureLogs(fn func()) string {
	var buf bytes.Buffer
	prevOut := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prevOut)

	fn()
	return buf.String()
}

func emitAll() {
	Info("info-marker %d", 1)
	Warn("warn-marker %d", 2)
	Error("error-marker %d", 3)
}

// TestSetLevelErrorSuppressesInfoAndWarn: at level "error" only Error lines
// are emitted; Info and Warn are suppressed.
func TestSetLevelErrorSuppressesInfoAndWarn(t *testing.T) {
	SetLevel("error")
	defer SetLevel("info") // restore the package default

	out := captureLogs(emitAll)

	if !strings.Contains(out, "error-marker") {
		t.Fatalf("level=error must keep Error, got output: %q", out)
	}
	if strings.Contains(out, "info-marker") {
		t.Fatalf("level=error must suppress Info, got output: %q", out)
	}
	if strings.Contains(out, "warn-marker") {
		t.Fatalf("level=error must suppress Warn, got output: %q", out)
	}
}

// TestSetLevelDebugKeepsEverything: at level "debug" Info, Warn and Error all
// pass the threshold.
func TestSetLevelDebugKeepsEverything(t *testing.T) {
	SetLevel("debug")
	defer SetLevel("info")

	out := captureLogs(emitAll)

	for _, marker := range []string{"info-marker", "warn-marker", "error-marker"} {
		if !strings.Contains(out, marker) {
			t.Fatalf("level=debug must keep %s, got output: %q", marker, out)
		}
	}
}

// TestSetLevelWarnSuppressesInfo: at level "warn" Info is suppressed while
// Warn and Error still emit (threshold semantics).
func TestSetLevelWarnSuppressesInfo(t *testing.T) {
	SetLevel("warn")
	defer SetLevel("info")

	out := captureLogs(emitAll)

	if strings.Contains(out, "info-marker") {
		t.Fatalf("level=warn must suppress Info, got output: %q", out)
	}
	if !strings.Contains(out, "warn-marker") || !strings.Contains(out, "error-marker") {
		t.Fatalf("level=warn must keep Warn and Error, got output: %q", out)
	}
}

// TestSetLevelUnknownDefaultsToInfo: an unknown value falls back to the info
// threshold (Info/Warn/Error all emit), matching the documented default.
func TestSetLevelUnknownDefaultsToInfo(t *testing.T) {
	for _, bad := range []string{"", "verbose", "INFO "} {
		SetLevel(bad)
		defer SetLevel("info")

		out := captureLogs(emitAll)
		if !strings.Contains(out, "info-marker") || !strings.Contains(out, "warn-marker") || !strings.Contains(out, "error-marker") {
			t.Fatalf("SetLevel(%q) must default to info threshold, got output: %q", bad, out)
		}
	}
}

// TestSetLevelTrimsAndLowercases: whitespace and case must not matter.
func TestSetLevelTrimsAndLowercases(t *testing.T) {
	SetLevel("  ERROR ")
	defer SetLevel("info")

	out := captureLogs(emitAll)
	if strings.Contains(out, "info-marker") {
		t.Fatalf("SetLevel(\"  ERROR \") must suppress Info, got output: %q", out)
	}
}
