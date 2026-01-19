package client

import (
	"os"
	"strings"
	"testing"
)

func TestNewAuthManager(t *testing.T) {
	am := NewAuthManager()
	if am == nil {
		t.Fatal("NewAuthManager returned nil")
	}
	if am.configDir == "" {
		t.Fatal("configDir is empty")
	}
}

func TestSessionFilePath(t *testing.T) {
	am := NewAuthManager()

	tests := []struct {
		name       string
		clientIP   string
		wantSubstr string
	}{
		{
			name:       "localhost",
			clientIP:   "localhost",
			wantSubstr: "session-localhost.json",
		},
		{
			name:       "IPv4",
			clientIP:   "192.168.1.1",
			wantSubstr: "session-192.168.1.1.json",
		},
		{
			name:       "IPv6 with colons",
			clientIP:   "::1:8080",
			wantSubstr: "session---1-8080.json",
		},
		{
			name:       "IP with slashes",
			clientIP:   "192.168.1.1/24",
			wantSubstr: "session-192.168.1.1-24.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := am.sessionFilePath(tt.clientIP)
			if !strings.Contains(path, tt.wantSubstr) {
				t.Errorf("sessionFilePath(%s) = %s, want to contain %s", tt.clientIP, path, tt.wantSubstr)
			}
		})
	}
}

func TestSaveAndLoadSession(t *testing.T) {
	tempDir := t.TempDir()
	am := &AuthManager{configDir: tempDir}

	testUsername := "testuser"
	testToken := "test-jwt-token"
	testIP := "127.0.0.1"

	if err := am.SaveSession(testIP, testUsername, testToken); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	sessionFile := am.sessionFilePath(testIP)
	if _, err := os.Stat(sessionFile); err != nil {
		t.Fatalf("Session file not created: %v", err)
	}

	username, token, err := am.LoadSession(testIP)
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}

	if username != testUsername {
		t.Errorf("LoadSession username = %s, want %s", username, testUsername)
	}
	if token != testToken {
		t.Errorf("LoadSession token = %s, want %s", token, testToken)
	}
}

func TestLoadSessionNonExistent(t *testing.T) {
	tempDir := t.TempDir()
	am := &AuthManager{configDir: tempDir}

	username, token, err := am.LoadSession("nonexistent-ip")
	if err != nil {
		t.Fatalf("LoadSession should not error on missing file: %v", err)
	}

	if username != "" || token != "" {
		t.Errorf("LoadSession returned non-empty values for missing session: username=%s, token=%s", username, token)
	}
}

func TestGenerateRandomUsername(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		length   int
		checkLen int
	}{
		{
			name:     "player-6",
			prefix:   "player",
			length:   6,
			checkLen: len("player-") + 6,
		},
		{
			name:     "test-10",
			prefix:   "test",
			length:   10,
			checkLen: len("test-") + 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			username, err := generateRandomUsername(tt.prefix, tt.length)
			if err != nil {
				t.Fatalf("generateRandomUsername failed: %v", err)
			}

			if !strings.HasPrefix(username, tt.prefix+"-") {
				t.Errorf("username = %s, should start with %s-", username, tt.prefix)
			}

			if len(username) != tt.checkLen {
				t.Errorf("username length = %d, want %d", len(username), tt.checkLen)
			}

			randomPart := strings.TrimPrefix(username, tt.prefix+"-")
			for _, c := range randomPart {
				if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
					t.Errorf("username contains non-alphanumeric character: %c", c)
				}
			}
		})
	}
}

func TestSessionFilePermissions(t *testing.T) {
	tempDir := t.TempDir()
	am := &AuthManager{configDir: tempDir}

	if err := am.SaveSession("test-ip", "user", "token"); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	sessionFile := am.sessionFilePath("test-ip")
	info, err := os.Stat(sessionFile)
	if err != nil {
		t.Fatalf("Failed to stat session file: %v", err)
	}

	mode := info.Mode()
	if mode.Perm() != 0600 {
		t.Errorf("Session file permissions = %o, want 0600", mode.Perm())
	}
}
