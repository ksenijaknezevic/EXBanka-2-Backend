// Package service contains notification use-case logic.
package service

import (
	"bytes"
	"fmt"
	"html/template"
	"net/smtp"

	"banka-backend/services/notification-service/internal/config"
	"banka-backend/services/notification-service/internal/domain"
)

// EmailService sends transactional emails via SMTP.
type EmailService struct {
	cfg *config.Config
}

// NewEmailService returns a ready EmailService.
func NewEmailService(cfg *config.Config) *EmailService {
	return &EmailService{cfg: cfg}
}

// SendEmail dispatches an HTML email based on the event type.
func (s *EmailService) SendEmail(event domain.EmailEvent) error {
	var subject, tmplStr string

	switch event.Type {
	case "ACTIVATION":
		subject = "Activate Your EXBanka Account"
		tmplStr = "<h1>Welcome to EXBanka!</h1><p>Click <a href='{{.FrontendURL}}/activate?token={{.Token}}'>here</a> to set your password and activate your account.</p>"
	case "RESET":
		subject = "Password Reset Request"
		tmplStr = "<h1>Password Reset</h1><p>Click <a href='{{.FrontendURL}}/reset-password?token={{.Token}}'>here</a> to reset your password.</p>"
	case "CONFIRMATION":
		subject = "Password Changed Successfully"
		tmplStr = "<h1>Password Changed</h1><p>Your password has been successfully updated. If you did not make this change, please contact support immediately.</p>"
	default:
		return fmt.Errorf("unknown email event type: %s", event.Type)
	}

	tmpl, err := template.New("email").Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("template parse: %w", err)
	}

	var body bytes.Buffer
	if err := tmpl.Execute(&body, struct {
		FrontendURL string
		Token       string
	}{
		FrontendURL: s.cfg.FrontendURL,
		Token:       event.Token,
	}); err != nil {
		return fmt.Errorf("template execute: %w", err)
	}

	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=\"UTF-8\"\r\n\r\n%s",
		s.cfg.FromEmail, event.Email, subject, body.String(),
	)

	addr := fmt.Sprintf("%s:%s", s.cfg.SMTPHost, s.cfg.SMTPPort)

	var auth smtp.Auth
	if s.cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", s.cfg.SMTPUser, s.cfg.SMTPPass, s.cfg.SMTPHost)
	}

	return smtp.SendMail(addr, auth, s.cfg.FromEmail, []string{event.Email}, []byte(msg))
}
