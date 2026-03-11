// Package handler — gRPC server handler for notification-service.
// Implements the NotificationService gRPC interface defined in
// proto/notification/notification.proto.
// Run `make proto` to generate the concrete interface; this file provides the
// implementation skeleton.
package handler

import (
	"context"

	"banka-backend/services/notification-service/internal/domain"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ─── Proto stubs (replace with protoc-generated types after `make proto`) ────

type NotifHealthCheckRequest  struct{}
type NotifHealthCheckResponse struct{ Status string }

type SendEmailRequest struct {
	To      string
	Subject string
	Body    string
}
type SendEmailResponse struct{ Success bool }

// ─── Handler ──────────────────────────────────────────────────────────────────

// NotificationGRPCHandler implements the NotificationService gRPC server.
type NotificationGRPCHandler struct {
	svc domain.NotificationService
}

// NewNotificationGRPCHandler creates a handler with the given service.
func NewNotificationGRPCHandler(svc domain.NotificationService) *NotificationGRPCHandler {
	return &NotificationGRPCHandler{svc: svc}
}

// HealthCheck returns SERVING when the service is ready.
func (h *NotificationGRPCHandler) HealthCheck(_ context.Context, _ *NotifHealthCheckRequest) (*NotifHealthCheckResponse, error) {
	return &NotifHealthCheckResponse{Status: "SERVING"}, nil
}

// SendEmail delivers a notification via the use-case layer.
func (h *NotificationGRPCHandler) SendEmail(ctx context.Context, req *SendEmailRequest) (*SendEmailResponse, error) {
	n := &domain.Notification{To: req.To, Subject: req.Subject, Body: req.Body}
	if err := h.svc.SendEmail(n); err != nil {
		return nil, status.Errorf(codes.Internal, "send email failed: %v", err)
	}
	return &SendEmailResponse{Success: true}, nil
}
