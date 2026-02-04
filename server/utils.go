package server

import (
	"crypto/rand"
	"fmt"
	"math/big"
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
