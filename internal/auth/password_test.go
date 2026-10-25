package auth

import (
	"bytes"
	"strings"
	"testing"
)

func TestPasswordsAreHashedAndValidated(t *testing.T) {
	password := "correct-password"
	hash, err := CreatePasswordHash(password)
	if err != nil || hash == password || !PasswordMatches(hash, password) || PasswordMatches(hash, "wrong-password") || PasswordMatches("invalid-hash", password) || PasswordMatches(hash, "") || PasswordMatches(hash, strings.Repeat("a", 73)) {
		t.Fatal("invalid password verification", err)
	}
	for _, value := range []string{strings.Repeat("a", 9), strings.Repeat("a", 73)} {
		if _, err := CreatePasswordHash(value); err == nil {
			t.Fatal("invalid password length accepted")
		}
	}
}

func TestOpaqueTokensAndHashes(t *testing.T) {
	first, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateToken()
	if err != nil || first == second || len(first) < 40 {
		t.Fatal("tokens must be random and opaque", err)
	}
	if len(HashToken(first)) != 32 || !bytes.Equal(HashToken(first), HashToken(first)) || bytes.Equal(HashToken(first), HashToken(second)) {
		t.Fatal("invalid token hash")
	}
}
