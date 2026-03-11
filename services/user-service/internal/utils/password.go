// Package utils contains stateless helper functions shared across the service.
package utils

import "golang.org/x/crypto/bcrypt"

const bcryptCost = 12

// HashPassword salts and hashes a plaintext password using bcrypt.
func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword returns nil when plain matches the stored bcrypt hash.
func CheckPassword(plain, hash string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
}
