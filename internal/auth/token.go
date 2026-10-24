package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

const TOKEN_SIZE = 32

// Membuat token acak
func GenerateToken() (string, error) {
	randomBytes := make([]byte, TOKEN_SIZE)

	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("Gagal membuat token: %w", err)
	}

	token := base64.RawURLEncoding.EncodeToString(randomBytes)

	return token, nil
}

// Menghasilkan hash SHA-256 dari token
func HashToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))

	return hash[:]
}
