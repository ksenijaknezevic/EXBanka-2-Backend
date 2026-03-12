package config_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"banka-backend/services/user-service/internal/config"
)

func setEnv(t *testing.T, kvs map[string]string) {
	t.Helper()
	for k, v := range kvs {
		t.Setenv(k, v)
	}
}

func requiredEnv(t *testing.T) {
	t.Helper()
	setEnv(t, map[string]string{
		"DB_HOST":     "localhost",
		"DB_PORT":     "5432",
		"DB_USER":     "user",
		"DB_PASSWORD": "pass",
		"DB_NAME":     "testdb",
	})
}

// ─── Load ─────────────────────────────────────────────────────────────────────

func TestLoad_Success_WithDefaults(t *testing.T) {
	requiredEnv(t)

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "localhost", cfg.DBHost)
	assert.Equal(t, "5432", cfg.DBPort)
	assert.Equal(t, "user", cfg.DBUser)
	assert.Equal(t, "pass", cfg.DBPassword)
	assert.Equal(t, "testdb", cfg.DBName)

	// defaults
	assert.Equal(t, "0.0.0.0:8080", cfg.HTTPAddr)
	assert.Equal(t, "0.0.0.0:50051", cfg.GRPCAddr)
	assert.Equal(t, "change-me-access-secret", cfg.JWTAccessSecret)
	assert.Equal(t, "change-me-refresh-secret", cfg.JWTRefreshSecret)
	assert.Equal(t, "change-me-activation-secret", cfg.JWTActivationSecret)
	assert.Equal(t, "amqp://guest:guest@localhost:5672/", cfg.RabbitMQURL)
}

func TestLoad_EnvOverridesDefaults(t *testing.T) {
	requiredEnv(t)
	setEnv(t, map[string]string{
		"HTTP_ADDR":             "0.0.0.0:9090",
		"GRPC_ADDR":             "0.0.0.0:9091",
		"JWT_ACCESS_SECRET":     "my-access-secret",
		"JWT_REFRESH_SECRET":    "my-refresh-secret",
		"JWT_ACTIVATION_SECRET": "my-activation-secret",
		"RABBITMQ_URL":          "amqp://user:pass@rabbit:5672/",
	})

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0:9090", cfg.HTTPAddr)
	assert.Equal(t, "0.0.0.0:9091", cfg.GRPCAddr)
	assert.Equal(t, "my-access-secret", cfg.JWTAccessSecret)
	assert.Equal(t, "my-refresh-secret", cfg.JWTRefreshSecret)
	assert.Equal(t, "my-activation-secret", cfg.JWTActivationSecret)
	assert.Equal(t, "amqp://user:pass@rabbit:5672/", cfg.RabbitMQURL)
}

func TestLoad_MissingRequiredVars(t *testing.T) {
	required := []string{"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME"}

	for _, missing := range required {
		t.Run("missing "+missing, func(t *testing.T) {
			// Set all required except the one we're testing
			requiredEnv(t)
			t.Setenv(missing, "") // override with empty to simulate missing

			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), missing)
		})
	}
}

// ─── DSN ──────────────────────────────────────────────────────────────────────

func TestDSN_Format(t *testing.T) {
	requiredEnv(t)
	cfg, err := config.Load()
	require.NoError(t, err)

	dsn := cfg.DSN()
	expected := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName,
	)
	assert.Equal(t, expected, dsn)
}
