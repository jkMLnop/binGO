package client

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
)

// LocalSession represents a saved session on disk
type LocalSession struct {
	Username string `json:"username"`
	Token    string `json:"token"`
}

// AuthManager handles local authentication (token storage/retrieval, username prompts)
type AuthManager struct {
	configDir string // ~/.config/binGO-CLI or similar
}

// NewAuthManager creates a new auth manager
func NewAuthManager() *AuthManager {
	// Determine config directory
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	configDir := filepath.Join(home, ".config", "binGO-CLI")

	// Create config dir if it doesn't exist
	_ = os.MkdirAll(configDir, 0700)

	return &AuthManager{configDir: configDir}
}

// SaveSession saves username and token locally by IP
func (am *AuthManager) SaveSession(clientIP, username, token string) error {
	session := LocalSession{
		Username: username,
		Token:    token,
	}

	sessionFile := am.sessionFilePath(clientIP)
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(sessionFile, data, 0600); err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	return nil
}

// LoadSession loads username and token from local storage
func (am *AuthManager) LoadSession(clientIP string) (username, token string, err error) {
	sessionFile := am.sessionFilePath(clientIP)

	data, err := os.ReadFile(sessionFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", nil // No saved session
		}
		return "", "", err
	}

	var session LocalSession
	if err := json.Unmarshal(data, &session); err != nil {
		return "", "", err
	}

	return session.Username, session.Token, nil
}

// sessionFilePath returns the path for a session file
func (am *AuthManager) sessionFilePath(clientIP string) string {
	// Sanitize IP for use in filename
	sanitizedIP := strings.ReplaceAll(clientIP, ":", "-")
	sanitizedIP = strings.ReplaceAll(sanitizedIP, "/", "-")
	return filepath.Join(am.configDir, fmt.Sprintf("session-%s.json", sanitizedIP))
}

// PromptForUsername asks user for username or generates random one
func (am *AuthManager) PromptForUsername(lastUsername string) (string, error) {
	if lastUsername != "" {
		fmt.Printf("Username (default: %s): ", lastUsername)
	} else {
		fmt.Print("Username (press Enter for random): ")
	}

	var input string
	_, err := fmt.Scanln(&input)
	if err != nil && err.Error() != "unexpected newline" {
		return "", err
	}

	// If empty input
	if strings.TrimSpace(input) == "" {
		// Use last username if available
		if lastUsername != "" {
			return lastUsername, nil
		}
		// Otherwise generate random
		return generateRandomUsername("player", 6)
	}

	return input, nil
}

// PromptForReconnect asks user if they want to use last username
func (am *AuthManager) PromptForReconnect(lastUsername string) (useLastUsername bool, err error) {
	fmt.Printf("Reconnect as %s? (y/n, default=y): ", lastUsername)

	var input string
	_, err = fmt.Scanln(&input)
	if err != nil && err.Error() != "unexpected newline" {
		return false, err
	}

	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" || input == "y" || input == "yes" {
		return true, nil
	}

	return false, nil
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
