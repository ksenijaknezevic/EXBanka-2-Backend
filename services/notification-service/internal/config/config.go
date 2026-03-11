// Package config loads notification-service configuration from ENV vars.
package config

import "os"

// Config holds all runtime configuration for the notification service.
type Config struct {
	RabbitMQURL string
	SMTPHost    string
	SMTPPort    string
	SMTPUser    string
	SMTPPass    string
	FromEmail   string
	FrontendURL string
}

// LoadConfig returns a Config populated from environment variables with defaults.
func LoadConfig() *Config {
	return &Config{
		RabbitMQURL: getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		SMTPHost:    getEnv("SMTP_HOST", "sandbox.smtp.mailtrap.io"),
		SMTPPort:    getEnv("SMTP_PORT", "2525"),
		SMTPUser:    getEnv("SMTP_USER", ""),
		SMTPPass:    getEnv("SMTP_PASS", ""),
		FromEmail:   getEnv("FROM_EMAIL", "noreply@banka.rs"),
		FrontendURL: getEnv("FRONTEND_URL", "http://localhost:3000"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
