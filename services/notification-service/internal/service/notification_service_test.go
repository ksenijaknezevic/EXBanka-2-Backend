package service_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"banka-backend/services/notification-service/internal/domain"
	"banka-backend/services/notification-service/internal/service"
	"banka-backend/services/notification-service/internal/testutil"
	"banka-backend/services/notification-service/mocks"
)

func newService(sender *mocks.MockSMTPSender) *service.EmailService {
	return service.NewEmailService(testutil.TestConfig(), sender)
}

// ─── SendEmail ────────────────────────────────────────────────────────────────

func TestSendEmail(t *testing.T) {
	tests := []struct {
		name      string
		event     domain.EmailEvent
		setup     func(sender *mocks.MockSMTPSender)
		wantErr   bool
		errSubstr string
	}{
		{
			name:  "ACTIVATION success",
			event: domain.EmailEvent{Type: "ACTIVATION", Email: "user@test.com", Token: "act-token-123"},
			setup: func(s *mocks.MockSMTPSender) {
				s.On("Send", "user@test.com", "Activate Your EXBanka Account",
					mock.MatchedBy(func(body string) bool { return strings.Contains(body, "act-token-123") })).Return(nil)
			},
			wantErr: false,
		},
		{
			name:  "RESET success",
			event: domain.EmailEvent{Type: "RESET", Email: "user@test.com", Token: "reset-token-456"},
			setup: func(s *mocks.MockSMTPSender) {
				s.On("Send", "user@test.com", "Password Reset Request",
					mock.MatchedBy(func(body string) bool { return strings.Contains(body, "reset-token-456") })).Return(nil)
			},
			wantErr: false,
		},
		{
			name:  "CONFIRMATION success (no token needed)",
			event: domain.EmailEvent{Type: "CONFIRMATION", Email: "user@test.com", Token: ""},
			setup: func(s *mocks.MockSMTPSender) {
				s.On("Send", "user@test.com", "Password Changed Successfully",
					mock.MatchedBy(func(body string) bool { return strings.Contains(body, "password has been successfully updated") })).Return(nil)
			},
			wantErr: false,
		},
		{
			name:      "empty email returns error before sending",
			event:     domain.EmailEvent{Type: "ACTIVATION", Email: "  ", Token: "tok"},
			setup:     func(s *mocks.MockSMTPSender) {},
			wantErr:   true,
			errSubstr: "recipient email is required",
		},
		{
			name:      "unknown event type",
			event:     domain.EmailEvent{Type: "UNKNOWN", Email: "user@test.com", Token: ""},
			setup:     func(s *mocks.MockSMTPSender) {},
			wantErr:   true,
			errSubstr: "unknown email event type",
		},
		{
			name:  "SMTP sender error is propagated",
			event: domain.EmailEvent{Type: "ACTIVATION", Email: "user@test.com", Token: "tok"},
			setup: func(s *mocks.MockSMTPSender) {
				s.On("Send", "user@test.com", "Activate Your EXBanka Account",
					mock.MatchedBy(func(body string) bool { return strings.Contains(body, "tok") })).Return(errors.New("smtp connection refused"))
			},
			wantErr:   true,
			errSubstr: "smtp connection refused",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sender := &mocks.MockSMTPSender{}
			tc.setup(sender)
			svc := newService(sender)

			err := svc.SendEmail(tc.event)
			if tc.wantErr {
				require.Error(t, err)
				if tc.errSubstr != "" {
					assert.Contains(t, err.Error(), tc.errSubstr)
				}
			} else {
				require.NoError(t, err)
			}
			sender.AssertExpectations(t)
		})
	}
}

// ─── Link generation ──────────────────────────────────────────────────────────

func TestSendEmail_ActivationLinkContainsFrontendURL(t *testing.T) {
	sender := &mocks.MockSMTPSender{}
	cfg := testutil.TestConfig()

	var capturedBody string
	sender.On("Send",
		"user@test.com",
		"Activate Your EXBanka Account",
		mock.MatchedBy(func(body string) bool {
			capturedBody = body
			return true
		}),
	).Return(nil)

	svc := service.NewEmailService(cfg, sender)
	err := svc.SendEmail(domain.EmailEvent{Type: "ACTIVATION", Email: "user@test.com", Token: "my-token"})
	require.NoError(t, err)

	assert.Contains(t, capturedBody, cfg.FrontendURL)
	assert.Contains(t, capturedBody, "my-token")
}

