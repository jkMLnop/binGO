package server

import (
	"regexp"
	"strings"
	"testing"
)

func TestGenerateGameCode(t *testing.T) {
	// Test format
	code := GenerateGameCode()
	if !strings.HasPrefix(code, "BINGO-") {
		t.Errorf("Expected code to start with 'BINGO-', got %s", code)
	}

	// Test length (BINGO- is 6 chars, then 5 random chars = 11 total)
	if len(code) != 11 {
		t.Errorf("Expected code length 11, got %d", len(code))
	}

	// Test valid characters (A-Z and 0-9 only)
	matched, _ := regexp.MatchString(`^BINGO-[A-Z0-9]{5}$`, code)
	if !matched {
		t.Errorf("Code format invalid: %s", code)
	}

	// Test randomness - generate multiple codes and verify they're different
	codes := make(map[string]bool)
	for i := 0; i < 100; i++ {
		codes[GenerateGameCode()] = true
	}
	if len(codes) < 90 { // Should have at least 90 unique codes out of 100
		t.Errorf("GenerateGameCode not random enough, only got %d unique codes out of 100", len(codes))
	}
}
