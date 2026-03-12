package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"banka-backend/services/user-service/internal/domain"
	"banka-backend/services/user-service/internal/service"
	"banka-backend/services/user-service/mocks"
)

const (
	testAccessSecret  = "test-access-secret-32-chars-long!"
	testRefreshSecret = "test-refresh-secret-32-chars-lon!"
)

func newService(repo *mocks.MockUserRepository) domain.UserService {
	return service.NewUserService(repo, testAccessSecret, testRefreshSecret)
}

// bcryptHash hashes a password in test setup without going through utils.
func bcryptHash(plain string) string {
	b, _ := bcrypt.GenerateFromPassword([]byte(plain), 12)
	return string(b)
}

// ─── Register ─────────────────────────────────────────────────────────────────

func TestRegister_Success(t *testing.T) {
	repo := &mocks.MockUserRepository{}
	svc := newService(repo)

	repo.On("FindByEmail", "new@test.com").Return(nil, domain.ErrUserNotFound)
	repo.On("Create", mock.AnythingOfType("*domain.User")).Return(nil)

	user, err := svc.Register("Alice", "new@test.com", "SecurePass1!")
	require.NoError(t, err)
	require.NotNil(t, user)
	assert.Equal(t, "Alice", user.Name)
	assert.Equal(t, "new@test.com", user.Email)
	repo.AssertExpectations(t)
}

func TestRegister_DuplicateEmail(t *testing.T) {
	repo := &mocks.MockUserRepository{}
	svc := newService(repo)

	existing := &domain.User{ID: "1", Email: "taken@test.com"}
	repo.On("FindByEmail", "taken@test.com").Return(existing, nil)

	_, err := svc.Register("Bob", "taken@test.com", "SecurePass1!")
	assert.ErrorIs(t, err, domain.ErrEmailTaken)
	repo.AssertExpectations(t)
}

func TestRegister_RepoCreateError(t *testing.T) {
	repo := &mocks.MockUserRepository{}
	svc := newService(repo)

	repo.On("FindByEmail", "new@test.com").Return(nil, domain.ErrUserNotFound)
	repo.On("Create", mock.AnythingOfType("*domain.User")).Return(errors.New("db error"))

	_, err := svc.Register("Charlie", "new@test.com", "SecurePass1!")
	assert.Error(t, err)
	repo.AssertExpectations(t)
}

// ─── Login ────────────────────────────────────────────────────────────────────

func TestLogin_Success(t *testing.T) {
	repo := &mocks.MockUserRepository{}
	svc := newService(repo)

	user := &domain.User{
		ID:           "42",
		Email:        "login@test.com",
		PasswordHash: bcryptHash("MyPassword1!"),
		CreatedAt:    time.Now(),
	}
	repo.On("FindByEmail", "login@test.com").Return(user, nil)

	access, refresh, err := svc.Login("login@test.com", "MyPassword1!")
	require.NoError(t, err)
	assert.NotEmpty(t, access)
	assert.NotEmpty(t, refresh)
	repo.AssertExpectations(t)
}

func TestLogin_UserNotFound(t *testing.T) {
	repo := &mocks.MockUserRepository{}
	svc := newService(repo)

	repo.On("FindByEmail", "ghost@test.com").Return(nil, domain.ErrUserNotFound)

	_, _, err := svc.Login("ghost@test.com", "anypass")
	assert.ErrorIs(t, err, domain.ErrInvalidCredentials)
	repo.AssertExpectations(t)
}

func TestLogin_WrongPassword(t *testing.T) {
	repo := &mocks.MockUserRepository{}
	svc := newService(repo)

	user := &domain.User{
		ID:           "1",
		Email:        "user@test.com",
		PasswordHash: bcryptHash("CorrectPass1!"),
	}
	repo.On("FindByEmail", "user@test.com").Return(user, nil)

	_, _, err := svc.Login("user@test.com", "WrongPassword!")
	assert.ErrorIs(t, err, domain.ErrInvalidCredentials)
	repo.AssertExpectations(t)
}

// ─── GetByID ──────────────────────────────────────────────────────────────────

func TestGetByID_Success(t *testing.T) {
	repo := &mocks.MockUserRepository{}
	svc := newService(repo)

	user := &domain.User{ID: "7", Name: "Dave", Email: "dave@test.com"}
	repo.On("FindByID", "7").Return(user, nil)

	result, err := svc.GetByID("7")
	require.NoError(t, err)
	assert.Equal(t, "7", result.ID)
	assert.Equal(t, "Dave", result.Name)
	repo.AssertExpectations(t)
}

func TestGetByID_NotFound(t *testing.T) {
	repo := &mocks.MockUserRepository{}
	svc := newService(repo)

	repo.On("FindByID", "999").Return(nil, domain.ErrUserNotFound)

	_, err := svc.GetByID("999")
	assert.ErrorIs(t, err, domain.ErrUserNotFound)
	repo.AssertExpectations(t)
}
