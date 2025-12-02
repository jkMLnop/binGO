package server

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenClaims represents the JWT claims with IP binding
type TokenClaims struct {
	Username string `json:"username"`
	IP       string `json:"ip"`
	jwt.RegisteredClaims
}

// TokenManager handles JWT token generation and validation
type TokenManager struct {
	secret string
}

// NewTokenManager creates a new token manager with a secret key
func NewTokenManager(secret string) *TokenManager {
	if secret == "" {
		// Generate a random secret if none provided (for testing)
		secret = generateRandomSecret(32)
	}
	return &TokenManager{secret: secret}
}

// IssueToken creates a new JWT token bound to a username and IP
func (tm *TokenManager) IssueToken(username, clientIP string, expirationHours int) (string, error) {
	now := time.Now()
	expiresAt := now.Add(time.Hour * time.Duration(expirationHours))

	claims := TokenClaims{
		Username: username,
		IP:       clientIP,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			Issuer:    "binGO-CLI",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(tm.secret))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// VerifyToken validates a JWT token and checks IP binding
// Returns username and error (nil if valid)
func (tm *TokenManager) VerifyToken(tokenString, clientIP string) (string, error) {
	claims := &TokenClaims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(tm.secret), nil
	})

	if err != nil {
		return "", fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return "", fmt.Errorf("invalid token")
	}

	// Verify IP binding
	if claims.IP != clientIP {
		return "", fmt.Errorf("token IP mismatch: token IP=%s, client IP=%s", claims.IP, clientIP)
	}

	// Check expiration
	if time.Now().After(claims.ExpiresAt.Time) {
		return "", fmt.Errorf("token expired")
	}

	return claims.Username, nil
}

// generateRandomSecret creates a random secret key for JWT signing
func generateRandomSecret(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	secret := make([]byte, length)
	for i := 0; i < length; i++ {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			panic(err)
		}
		secret[i] = charset[num.Int64()]
	}
	return string(secret)
}

// generateRandomUsername creates a random username like "player-abc123"
func generateRandomUsername(prefix string, length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	randomPart := make([]byte, length)
	for i := 0; i < length; i++ {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		randomPart[i] = charset[num.Int64()]
	}
	return fmt.Sprintf("%s-%s", prefix, string(randomPart)), nil
}
