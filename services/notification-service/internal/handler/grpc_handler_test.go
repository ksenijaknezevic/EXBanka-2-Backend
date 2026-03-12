package handler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"banka-backend/services/notification-service/internal/domain"
	"banka-backend/services/notification-service/internal/handler"
	"banka-backend/services/notification-service/mocks"
)

func grpcCode(err error) codes.Code {
	if err == nil {
		return codes.OK
	}
	s, _ := status.FromError(err)
	return s.Code()
}

// ─── HealthCheck ──────────────────────────────────────────────────────────────

func TestHealthCheck(t *testing.T) {
	svc := &mocks.MockNotificationService{}
	h := handler.NewNotificationGRPCHandler(svc)

	resp, err := h.HealthCheck(context.Background(), &handler.NotifHealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, "SERVING", resp.Status)
}

// ─── SendEmail ────────────────────────────────────────────────────────────────

func TestGRPC_SendEmail(t *testing.T) {
	tests := []struct {
		name     string
		req      *handler.SendEmailRequest
		setup    func(svc *mocks.MockNotificationService)
		wantCode codes.Code
		wantOK   bool
	}{
		{
			name: "success",
			req:  &handler.SendEmailRequest{To: "a@b.com", Subject: "Hello", Body: "<p>hi</p>"},
			setup: func(svc *mocks.MockNotificationService) {
				svc.On("SendEmail", &domain.Notification{To: "a@b.com", Subject: "Hello", Body: "<p>hi</p>"}).
					Return(nil)
			},
			wantCode: codes.OK,
			wantOK:   true,
		},
		{
			name: "service error → codes.Internal",
			req:  &handler.SendEmailRequest{To: "a@b.com", Subject: "Hello", Body: ""},
			setup: func(svc *mocks.MockNotificationService) {
				svc.On("SendEmail", &domain.Notification{To: "a@b.com", Subject: "Hello", Body: ""}).
					Return(errors.New("smtp error"))
			},
			wantCode: codes.Internal,
			wantOK:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &mocks.MockNotificationService{}
			tc.setup(svc)
			h := handler.NewNotificationGRPCHandler(svc)

			resp, err := h.SendEmail(context.Background(), tc.req)
			assert.Equal(t, tc.wantCode, grpcCode(err))
			if tc.wantOK {
				require.NoError(t, err)
				assert.True(t, resp.Success)
			} else {
				assert.Nil(t, resp)
			}
			svc.AssertExpectations(t)
		})
	}
}
