package domain_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"banka-backend/services/user-service/internal/domain"
)

func TestSentinelErrors_AreDistinct(t *testing.T) {
	errs := []error{
		domain.ErrUserNotFound,
		domain.ErrEmailTaken,
		domain.ErrInvalidCredentials,
	}

	for i, a := range errs {
		for j, b := range errs {
			if i != j {
				assert.False(t, errors.Is(a, b),
					"expected %v and %v to be distinct sentinel errors", a, b)
			}
		}
	}
}

func TestSentinelErrors_Messages(t *testing.T) {
	assert.Equal(t, "user not found", domain.ErrUserNotFound.Error())
	assert.Equal(t, "email already in use", domain.ErrEmailTaken.Error())
	assert.Equal(t, "invalid email or password", domain.ErrInvalidCredentials.Error())
}

func TestSentinelErrors_ErrorsIs(t *testing.T) {
	wrapped := errors.New("wrapped: " + domain.ErrUserNotFound.Error())
	// wrapped does NOT satisfy errors.Is with ErrUserNotFound (not wrapped via %w)
	assert.False(t, errors.Is(wrapped, domain.ErrUserNotFound))

	// Using fmt.Errorf with %w DOES satisfy errors.Is
	wrappedW := fmt.Errorf("context: %w", domain.ErrUserNotFound)
	assert.True(t, errors.Is(wrappedW, domain.ErrUserNotFound))
}
