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

func TestClassifyIP(t *testing.T) {
	tests := []struct {
		name         string
		clientIP     string
		serverIP     string
		expectedType IPType
	}{
		// Localhost cases
		{"localhost 127.0.0.1", "127.0.0.1", "192.168.1.1", Localhost},
		{"localhost ::1", "::1", "fe80::1", Localhost},
		{"localhost string", "localhost", "192.168.1.1", Localhost},
		{"localhost with port", "127.0.0.1:8080", "192.168.1.1", Localhost},

		// LAN cases (same subnet)
		{"LAN same subnet IPv4", "192.168.1.100", "192.168.1.1", LAN},
		{"LAN same subnet different last octet", "10.0.0.50", "10.0.0.1", LAN},
		{"LAN same subnet 172", "172.16.5.100", "172.16.5.1", LAN},

		// Remote cases
		{"Remote different subnet", "192.168.1.100", "10.0.0.1", Remote},
		{"Remote very different", "1.1.1.1", "8.8.8.8", Remote},
		{"Remote single IP", "203.0.113.1", "198.51.100.1", Remote},

		// IPv6 cases
		{"IPv6 link-local LAN", "fe80::1", "fe80::2", LAN},
		{"IPv6 different prefixes Remote", "2001:db8::1", "2001:db9::1", Remote},

		// Port handling
		{"IPv4 with port LAN", "192.168.1.100:9000", "192.168.1.1", LAN},
		{"IPv4 with port Remote", "203.0.113.1:8080", "198.51.100.1", Remote},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyIP(tt.clientIP, tt.serverIP)
			if result != tt.expectedType {
				t.Errorf("ClassifyIP(%s, %s) = %v, want %v", tt.clientIP, tt.serverIP, result, tt.expectedType)
			}
		})
	}
}

func TestExtractIPFromAddr(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		expected string
	}{
		// IPv4 with port
		{"IPv4 with port", "192.168.1.1:8080", "192.168.1.1"},
		{"IPv4 with high port", "10.0.0.1:65535", "10.0.0.1"},

		// IPv6 with brackets and port
		{"IPv6 bracketed with port", "[::1]:8080", "::1"},
		{"IPv6 full with port", "[2001:db8::1]:9000", "2001:db8::1"},
		{"IPv6 link-local with port", "[fe80::1]:8080", "fe80::1"},

		// Plain IP addresses (no port)
		{"Plain IPv4", "192.168.1.1", "192.168.1.1"},
		{"Plain IPv6", "2001:db8::1", "2001:db8::1"},
		{"Plain localhost", "localhost", "localhost"},

		// IPv6 without brackets
		{"IPv6 without brackets", "::1", "::1"},
		{"IPv6 full address", "2001:db8:85a3::8a2e:370:7334", "2001:db8:85a3::8a2e:370:7334"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractIPFromAddr(tt.addr)
			if result != tt.expected {
				t.Errorf("extractIPFromAddr(%s) = %s, want %s", tt.addr, result, tt.expected)
			}
		})
	}
}

func TestIsLocalConnection(t *testing.T) {
	tests := []struct {
		name          string
		clientIP      string
		serverIP      string
		expectedLocal bool
	}{
		// Local connections (Localhost or LAN)
		{"localhost connection", "127.0.0.1", "192.168.1.1", true},
		{"LAN connection", "192.168.1.100", "192.168.1.1", true},
		{"IPv6 localhost", "::1", "fe80::1", true},
		{"IPv6 LAN", "fe80::100", "fe80::1", true},

		// Remote connections
		{"remote connection", "203.0.113.1", "198.51.100.1", false},
		{"different subnet", "192.168.1.1", "10.0.0.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsLocalConnection(tt.clientIP, tt.serverIP)
			if result != tt.expectedLocal {
				t.Errorf("IsLocalConnection(%s, %s) = %v, want %v", tt.clientIP, tt.serverIP, result, tt.expectedLocal)
			}
		})
	}
}
