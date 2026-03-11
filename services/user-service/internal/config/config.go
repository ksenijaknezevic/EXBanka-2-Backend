// Package config loads all configuration from environment variables.
// Clean Architecture: infrastructure layer — no business logic here.
package config

import (
	"fmt"
	"os"
)

// Config holds all runtime configuration for user-service.
type Config struct {
	// HTTP
	HTTPAddr string // e.g. "0.0.0.0:8080"

	// gRPC
	GRPCAddr string // e.g. "0.0.0.0:50051"

	// PostgreSQL
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	// JWT
	JWTAccessSecret     string
	JWTRefreshSecret    string
	JWTActivationSecret string

	// Messaging
	RabbitMQURL string
}

// Load reads ENV vars and returns a populated Config.
// Required vars: DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME.
// Optional vars fall back to sensible defaults.
func Load() (*Config, error) {
	required := []string{"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME"}
	for _, key := range required {
		if os.Getenv(key) == "" {
			return nil, fmt.Errorf("missing required env var: %s", key)
		}
	}

	return &Config{
		HTTPAddr: getEnv("HTTP_ADDR", "0.0.0.0:8080"),
		GRPCAddr: getEnv("GRPC_ADDR", "0.0.0.0:50051"),

		DBHost:     os.Getenv("DB_HOST"),
		DBPort:     os.Getenv("DB_PORT"),
		DBUser:     os.Getenv("DB_USER"),
		DBPassword: os.Getenv("DB_PASSWORD"),
		DBName:     os.Getenv("DB_NAME"),

		JWTAccessSecret:     getEnv("JWT_ACCESS_SECRET", "change-me-access-secret"),
		JWTRefreshSecret:    getEnv("JWT_REFRESH_SECRET", "change-me-refresh-secret"),
		JWTActivationSecret: getEnv("JWT_ACTIVATION_SECRET", "change-me-activation-secret"),

		RabbitMQURL: getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
	}, nil
}

// DSN returns the PostgreSQL connection string for GORM.
func (c *Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName,
	)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
