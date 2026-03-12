package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"banka-backend/services/notification-service/internal/config"
)

func setEnv(t *testing.T, kvs map[string]string) {
	t.Helper()
	for k, v := range kvs {
		t.Setenv(k, v)
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	// Clear any overrides that might be present in CI
	t.Setenv("RABBITMQ_URL", "")
	t.Setenv("SMTP_HOST", "")
	t.Setenv("SMTP_PORT", "")
	t.Setenv("FRONTEND_URL", "")

	cfg := config.LoadConfig()

	assert.Equal(t, "amqp://guest:guest@localhost:5672/", cfg.RabbitMQURL)
	assert.Equal(t, "smtp.gmail.com", cfg.SMTPHost)
	assert.Equal(t, "587", cfg.SMTPPort)
	assert.Equal(t, "http://localhost:3001", cfg.FrontendURL)
}

func TestLoadConfig_EnvOverrides(t *testing.T) {
	setEnv(t, map[string]string{
		"RABBITMQ_URL": "amqp://user:pass@rabbit:5672/vhost",
		"SMTP_HOST":    "smtp.custom.com",
		"SMTP_PORT":    "465",
		"SMTP_USER":    "bot@custom.com",
		"SMTP_PASS":    "secret",
		"FROM_EMAIL":   "no-reply@custom.com",
		"FRONTEND_URL": "https://app.custom.com",
	})

	cfg := config.LoadConfig()

	assert.Equal(t, "amqp://user:pass@rabbit:5672/vhost", cfg.RabbitMQURL)
	assert.Equal(t, "smtp.custom.com", cfg.SMTPHost)
	assert.Equal(t, "465", cfg.SMTPPort)
	assert.Equal(t, "bot@custom.com", cfg.SMTPUser)
	assert.Equal(t, "secret", cfg.SMTPPass)
	assert.Equal(t, "no-reply@custom.com", cfg.FromEmail)
	assert.Equal(t, "https://app.custom.com", cfg.FrontendURL)
}

func TestLoadConfig_TrimsWhitespace(t *testing.T) {
	setEnv(t, map[string]string{
		"SMTP_HOST":  "  smtp.gmail.com  ",
		"SMTP_PORT":  " 587 ",
		"SMTP_USER":  " user@gmail.com ",
		"FROM_EMAIL": " from@gmail.com ",
	})

	cfg := config.LoadConfig()

	assert.Equal(t, "smtp.gmail.com", cfg.SMTPHost)
	assert.Equal(t, "587", cfg.SMTPPort)
	assert.Equal(t, "user@gmail.com", cfg.SMTPUser)
	assert.Equal(t, "from@gmail.com", cfg.FromEmail)
}

func TestLoadConfig_EmptyOptionalFields(t *testing.T) {
	t.Setenv("SMTP_USER", "")
	t.Setenv("SMTP_PASS", "")
	t.Setenv("FROM_EMAIL", "")

	cfg := config.LoadConfig()

	assert.Empty(t, cfg.SMTPUser)
	assert.Empty(t, cfg.SMTPPass)
	assert.Empty(t, cfg.FromEmail)
}
