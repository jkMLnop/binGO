package server

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"strings"
)

// GenerateGameCode generates a random alphanumeric code for a game session
func GenerateGameCode() string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const codeLength = 5 // 5 chars after BINGO-
	b := make([]byte, codeLength)
	for i := range b {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			panic(err)
		}
		b[i] = charset[num.Int64()]
	}
	// Format as BINGO-XXXXX
	return fmt.Sprintf("BINGO-%s", string(b))
}

// IPType represents the type of IP connection
type IPType int

const (
	Localhost IPType = iota
	LAN
	Remote
)

// ClassifyIP determines if an IP is localhost, LAN, or remote
func ClassifyIP(clientIP string, serverIP string) IPType {
	// Extract IP from [IP]:PORT format (IPv6 with port)
	clientIP = extractIPFromAddr(clientIP)
	serverIP = extractIPFromAddr(serverIP)

	// Check if localhost
	if clientIP == "127.0.0.1" || clientIP == "::1" || clientIP == "localhost" {
		return Localhost
	}

	// Check if same subnet (simple /24 check for IPv4)
	clientNet := net.ParseIP(clientIP)
	serverNet := net.ParseIP(serverIP)

	if clientNet != nil && serverNet != nil {
		// For IPv4, check first 3 octets (simple subnet detection)
		if clientNet.To4() != nil && serverNet.To4() != nil {
			clientParts := strings.Split(clientIP, ".")
			serverParts := strings.Split(serverIP, ".")
			if len(clientParts) == 4 && len(serverParts) == 4 {
				// Check first 3 octets match (192.168.1.x)
				if clientParts[0] == serverParts[0] &&
					clientParts[1] == serverParts[1] &&
					clientParts[2] == serverParts[2] {
					return LAN
				}
			}
		}
		// For IPv6, consider link-local addresses as LAN
		if clientNet.To4() == nil && serverNet.To4() == nil {
			// Both IPv6 - link-local is LAN
			if strings.HasPrefix(clientIP, "fe80:") && strings.HasPrefix(serverIP, "fe80:") {
				return LAN
			}
		}
	}

	return Remote
}

// extractIPFromAddr extracts IP from address that might include port
// Handles both "192.168.1.1:8080" and "[::1]:8080" formats
func extractIPFromAddr(addr string) string {
	// Handle IPv6 with brackets: [::1]:8080
	if strings.HasPrefix(addr, "[") {
		endBracket := strings.Index(addr, "]")
		if endBracket != -1 {
			return strings.TrimPrefix(addr[:endBracket+1], "[")
		}
	}
	// Handle IPv4 with port: 192.168.1.1:8080
	if strings.Contains(addr, ":") && !strings.Contains(addr, "::") {
		// Simple check: if it looks like IPv4:port, split on the last colon
		parts := strings.Split(addr, ":")
		if len(parts) == 2 {
			return parts[0]
		}
	}
	return addr
}

// IsLocalConnection checks if connection is from localhost or LAN
func IsLocalConnection(clientIP string, serverIP string) bool {
	ipType := ClassifyIP(clientIP, serverIP)
	return ipType == Localhost || ipType == LAN
}
