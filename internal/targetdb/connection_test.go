package targetdb

import (
	"errors"
	"strings"
	"testing"
)

func TestSanitizePostgresIdentifier(t *testing.T) {
	valid := []string{"toko_baru", "hr-2", "_internal", "SalesDB_2026"}
	for _, input := range valid {
		got, err := SanitizePostgresIdentifier(input)
		if err != nil || got != input {
			t.Fatalf("SanitizePostgresIdentifier(%q) = (%q, %v), want (%q, nil)", input, got, err, input)
		}
	}

	invalid := []string{
		"",
		"123abc",
		"-dashstart",
		`foo"; DROP TABLE users; --`,
		"bad space",
		"bad.dot",
		strings.Repeat("a", 64),
	}
	for _, input := range invalid {
		if _, err := SanitizePostgresIdentifier(input); !errors.Is(err, ErrInvalidPostgresIdentifier) {
			t.Fatalf("SanitizePostgresIdentifier(%q) err = %v, want ErrInvalidPostgresIdentifier", input, err)
		}
	}
}
