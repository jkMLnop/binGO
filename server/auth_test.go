package server

import (
	"strings"
	"testing"
	"time"
)

func TestNewTokenManager(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		want   bool // true if secret should be set
	}{
		{
			name:   "with provided secret",
			secret: "my-secret",
			want:   true,
		},
		{
			name:   "without secret - generates random",
			secret: "",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := NewTokenManager(tt.secret)
			if tm == nil {
				t.Fatal("NewTokenManager returned nil")
			}
			if tm.secret == "" {
				t.Fatal("TokenManager secret is empty")
			}
		})
	}
}

func TestIssueToken(t *testing.T) {
	tm := NewTokenManager("test-secret")
	username := "testuser"
	clientIP := "192.168.1.1"
	expirationHours := 24

	token, err := tm.IssueToken(username, clientIP, expirationHours)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	if token == "" {
		t.Fatal("IssueToken returned empty token")
	}

	// Token should be a valid JWT (have 3 parts separated by dots)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Errorf("IssueToken returned invalid JWT format: %s", token)
	}
}

func TestVerifyToken(t *testing.T) {
	tm := NewTokenManager("test-secret")
	username := "testuser"
	clientIP := "192.168.1.1"
	expirationHours := 24

	token, err := tm.IssueToken(username, clientIP, expirationHours)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	// Verify the token
	verifiedUsername, err := tm.VerifyToken(token, clientIP)
	if err != nil {
		t.Fatalf("VerifyToken failed: %v", err)
	}

	if verifiedUsername != username {
		t.Errorf("VerifyToken username = %s, want %s", verifiedUsername, username)
	}
}

func TestVerifyToken_IPMismatch(t *testing.T) {
	tm := NewTokenManager("test-secret")
	username := "testuser"
	issuedIP := "192.168.1.1"
	differentIP := "192.168.1.2"
	expirationHours := 24

	token, err := tm.IssueToken(username, issuedIP, expirationHours)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	// Try to verify with different IP
	_, err = tm.VerifyToken(token, differentIP)
	if err == nil {
		t.Fatal("VerifyToken should fail with IP mismatch")
	}

	if !strings.Contains(err.Error(), "IP mismatch") {
		t.Errorf("VerifyToken error = %v, want error containing 'IP mismatch'", err)
	}
}

func TestVerifyToken_ExpiredToken(t *testing.T) {
	tm := NewTokenManager("test-secret")
	username := "testuser"
	clientIP := "192.168.1.1"

	// Issue token that expires immediately
	token, err := tm.IssueToken(username, clientIP, 0)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	// Wait a bit to ensure expiration
	time.Sleep(100 * time.Millisecond)

	// Try to verify expired token
	_, err = tm.VerifyToken(token, clientIP)
	if err == nil {
		t.Fatal("VerifyToken should fail for expired token")
	}

	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("VerifyToken error = %v, want error containing 'expired'", err)
	}
}

func TestVerifyToken_InvalidSignature(t *testing.T) {
	tm1 := NewTokenManager("secret-1")
	tm2 := NewTokenManager("secret-2")

	username := "testuser"
	clientIP := "192.168.1.1"
	expirationHours := 24

	// Issue token with first manager
	token, err := tm1.IssueToken(username, clientIP, expirationHours)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	// Try to verify with second manager (different secret)
	_, err = tm2.VerifyToken(token, clientIP)
	if err == nil {
		t.Fatal("VerifyToken should fail with invalid signature")
	}
}

func TestVerifyToken_MalformedToken(t *testing.T) {
	tm := NewTokenManager("test-secret")
	clientIP := "192.168.1.1"

	tests := []struct {
		name  string
		token string
	}{
		{
			name:  "empty token",
			token: "",
		},
		{
			name:  "invalid format",
			token: "not.a.valid.jwt",
		},
		{
			name:  "garbage",
			token: "garbage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tm.VerifyToken(tt.token, clientIP)
			if err == nil {
				t.Errorf("VerifyToken should fail for malformed token: %s", tt.token)
			}
		})
	}
}

func TestGenerateRandomSecret(t *testing.T) {
	tests := []struct {
		name   string
		length int
	}{
		{
			name:   "32 bytes",
			length: 32,
		},
		{
			name:   "64 bytes",
			length: 64,
		},
		{
			name:   "1 byte",
			length: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret := generateRandomSecret(tt.length)

			if len(secret) != tt.length {
				t.Errorf("generateRandomSecret length = %d, want %d", len(secret), tt.length)
			}

			// Check that all characters are alphanumeric
			const validChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
			for _, c := range secret {
				if !strings.ContainsRune(validChars, c) {
					t.Errorf("generateRandomSecret contains invalid character: %c", c)
				}
			}
		})
	}
}

func TestGenerateRandomSecret_Randomness(t *testing.T) {
	// Generate multiple secrets and verify they're different
	secret1 := generateRandomSecret(32)
	secret2 := generateRandomSecret(32)
	secret3 := generateRandomSecret(32)

	if secret1 == secret2 {
		t.Error("generateRandomSecret generated identical secrets")
	}
	if secret2 == secret3 {
		t.Error("generateRandomSecret generated identical secrets")
	}
	if secret1 == secret3 {
		t.Error("generateRandomSecret generated identical secrets")
	}
}

func TestTokenClaims_JWTRegisteredClaims(t *testing.T) {
	tm := NewTokenManager("test-secret")
	username := "testuser"
	clientIP := "192.168.1.1"
	expirationHours := 24

	token, err := tm.IssueToken(username, clientIP, expirationHours)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	// Verify the token to check claims were set correctly
	verifiedUsername, err := tm.VerifyToken(token, clientIP)
	if err != nil {
		t.Fatalf("VerifyToken failed: %v", err)
	}

	if verifiedUsername != username {
		t.Errorf("Token username = %s, want %s", verifiedUsername, username)
	}
}
