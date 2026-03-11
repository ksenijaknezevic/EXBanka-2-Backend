// Package domain defines business entities and interfaces for notification-service.
// Clean Architecture: innermost layer — no external dependencies.
package domain

// Notification represents a generic message to be delivered to a recipient.
// Used by the HTTP/gRPC handler layer.
type Notification struct {
	To      string
	Subject string
	Body    string
}

// NotificationService defines the application use-case contract for the HTTP/gRPC layer.
type NotificationService interface {
	SendEmail(n *Notification) error
}

// EmailEvent is the message payload consumed from the email_notifications queue.
// Mirrors the struct published by user-service (JSON tags: type, email, token).
type EmailEvent struct {
	Type  string `json:"type"`  // "ACTIVATION" | "RESET"
	Email string `json:"email"` // recipient address
	Token string `json:"token"` // JWT for the action link
}
