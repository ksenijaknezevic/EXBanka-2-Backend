// Package domain contains pure business entities and interfaces.
// Clean Architecture: innermost layer — zero external dependencies.
package domain

import "time"

// User is the core business entity.
// It must NOT import GORM, gin, or any infrastructure package.
type User struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"` // never serialised to JSON
	CreatedAt    time.Time `json:"created_at"`
}

// UserRepository defines the persistence contract.
// The concrete implementation lives in internal/repository.
type UserRepository interface {
	Create(user *User) error
	FindByID(id string) (*User, error)
	FindByEmail(email string) (*User, error)
	Update(user *User) error
	Delete(id string) error
}

// UserService defines the application use-case contract.
// The concrete implementation lives in internal/service.
type UserService interface {
	Register(name, email, password string) (*User, error)
	Login(email, password string) (accessToken, refreshToken string, err error)
	GetByID(id string) (*User, error)
}

// TokenPair groups an access/refresh token pair.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}
