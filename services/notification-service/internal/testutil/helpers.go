// Package testutil provides shared helpers for notification-service unit tests.
// Nothing in this package is compiled into the production binary.
package testutil

import (
	"banka-backend/services/notification-service/internal/config"
)

// TestConfig returns a *config.Config populated with safe, non-functional values
// suitable for unit tests (no real SMTP server is contacted).
func TestConfig() *config.Config {
	return &config.Config{
		RabbitMQURL: "amqp://guest:guest@localhost:5672/",
		SMTPHost:    "smtp.example.com",
		SMTPPort:    "587",
		SMTPUser:    "test@example.com",
		SMTPPass:    "test-password",
		FromEmail:   "noreply@example.com",
		FrontendURL: "http://localhost:3001",
	}
}
