package utils

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashPassword_Success(t *testing.T) {
	hash, err := HashPassword("SecurePass123!")
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	// bcrypt hashes start with $2a$ or $2b$
	assert.True(t, strings.HasPrefix(hash, "$2"), "expected bcrypt hash, got: %s", hash)
}

func TestHashPassword_DifferentHashesForSameInput(t *testing.T) {
	// bcrypt is randomised — two hashes of the same password must differ
	h1, err1 := HashPassword("SamePassword1!")
	h2, err2 := HashPassword("SamePassword1!")
	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.NotEqual(t, h1, h2)
}

func TestCheckPassword_Correct(t *testing.T) {
	hash, err := HashPassword("MyPassword99!")
	require.NoError(t, err)
	assert.NoError(t, CheckPassword("MyPassword99!", hash))
}

func TestCheckPassword_Wrong(t *testing.T) {
	hash, err := HashPassword("MyPassword99!")
	require.NoError(t, err)
	assert.Error(t, CheckPassword("WrongPassword!", hash))
}

func TestCheckPassword_EmptyPassword(t *testing.T) {
	hash, err := HashPassword("SomePassword1!")
	require.NoError(t, err)
	assert.Error(t, CheckPassword("", hash))
}

func TestCheckPassword_InvalidHash(t *testing.T) {
	assert.Error(t, CheckPassword("any", "not-a-bcrypt-hash"))
}

func TestHashPassword_TableDriven(t *testing.T) {
	cases := []string{
		"short",
		"a very long password that exceeds typical length expectations!",
		"P@ssw0rd!",
		"unicode-ñ-password",
	}
	for _, pw := range cases {
		t.Run(pw, func(t *testing.T) {
			hash, err := HashPassword(pw)
			require.NoError(t, err)
			assert.NoError(t, CheckPassword(pw, hash))
		})
	}
}
