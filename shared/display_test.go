package shared

import (
	"strings"
	"testing"
)

// TestCenterText tests text centering functionality
func TestCenterText(t *testing.T) {
	testCases := []struct {
		name     string
		text     string
		width    int
		padding  string
		expected string
	}{
		{"Center short text", "Hi", 5, " ", " Hi  "},
		{"Center exact fit", "Hello", 5, " ", "Hello"},
		{"Center with dashes", "Hi", 5, "-", "-Hi--"},
		{"Large padding needed", "A", 7, " ", "   A   "},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := CenterText(tc.text, tc.width, tc.padding)
			if result != tc.expected {
				t.Errorf("CenterText(%q, %d, %q) = %q, want %q",
					tc.text, tc.width, tc.padding, result, tc.expected)
			}
		})
	}
}

// TestStrikethrough tests strikethrough formatting
func TestStrikethrough(t *testing.T) {
	text := "test"
	result := ApplyStrikethrough(text)

	// Each character gets a combining strikethrough character after it
	// So "test" (4 chars) becomes 8 characters (4 chars + 4 combining chars)
	runes := []rune(result)
	if len(runes) != len(text)*2 {
		t.Errorf("Expected %d runes (with combining), got %d", len(text)*2, len(runes))
	}

	// Verify it contains the strikethrough combining character (U+0336)
	if !strings.Contains(result, "\u0336") {
		t.Error("Strikethrough result should contain combining character")
	}
}

// TestVisualLength tests visual length calculation (excluding combining characters)
func TestVisualLength(t *testing.T) {
	testCases := []struct {
		name     string
		text     string
		expected int
	}{
		{"Plain text", "hello", 5},
		{"Empty string", "", 0},
		{"Single char", "x", 1},
		{"Text with strikethrough", ApplyStrikethrough("hi"), 2}, // visual length should ignore combining chars
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := VisualLength(tc.text)
			if result != tc.expected {
				t.Errorf("VisualLength(%q) = %d, want %d", tc.text, result, tc.expected)
			}
		})
	}
}
