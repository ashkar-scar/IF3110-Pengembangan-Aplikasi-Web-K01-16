package auth

import (
	"errors"
	"golang.org/x/crypto/bcrypt"
)

// Panjang minimum password dalam byte (10 byte)
const MIN_PASSWORD_LENGTH = 10

// Batas maksimum 72 byte milik bcrypt
const MAX_PASSWORD_LENGTH = 72

// Menentukan tingkat kerja bcrypt saat membuat hash.
const PASSWORD_HASH_COST = 12

// Memvalidasi dan mengubah password menjadi hash bcrypt
func CreatePasswordHash(password string) (string, error) {
	if err := validatePassword(password); err != nil {
		return "", err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), PASSWORD_HASH_COST)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}

// Memeriksa kecocokan password dengan hash yang tersimpan
func PasswordMatches(passwordHash string, password string) bool {
	if password == "" || len(password) > MAX_PASSWORD_LENGTH {
		return false
	}

	err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password))

	return err == nil
}

// Memastikan panjang password sesuai batas yang ditentukan dan valid
func validatePassword(password string) error {
	passwordLength := len(password)

	if passwordLength < MIN_PASSWORD_LENGTH {
		return errors.New("Password minimal 10 byte")
	}

	if passwordLength > MAX_PASSWORD_LENGTH {
		return errors.New("Password maksimal 72 byte")
	}

	return nil
}