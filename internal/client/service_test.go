package client

import (
	"strings"
	"testing"
)

// TestGenerateOneTimePassword pins the one-time credential generator: length,
// charset membership, uniformity of length and non-repetition.
func TestGenerateOneTimePassword(t *testing.T) {
	const wantLength = 16

	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		pw, err := GenerateOneTimePassword()
		if err != nil {
			t.Fatalf("GenerateOneTimePassword: %v", err)
		}
		if len(pw) != wantLength {
			t.Fatalf("password length = %d, want %d", len(pw), wantLength)
		}
		for _, c := range pw {
			if !strings.ContainsRune(oneTimePasswordAlphabet, c) {
				t.Fatalf("password %q contains character %q outside the alphabet", pw, c)
			}
		}
		if seen[pw] {
			t.Fatalf("duplicate password generated: %q", pw)
		}
		seen[pw] = true
	}
}

// TestGenerateOneTimePasswordExcludesAmbiguousCharacters ensures the alphabet
// omits 0/O/1/l/I so agency staff can relay credentials over WhatsApp without
// transcription errors.
func TestGenerateOneTimePasswordExcludesAmbiguousCharacters(t *testing.T) {
	for _, c := range []rune{'0', 'O', '1', 'l', 'I'} {
		if strings.ContainsRune(oneTimePasswordAlphabet, c) {
			t.Fatalf("alphabet must exclude ambiguous character %q", c)
		}
	}
}
