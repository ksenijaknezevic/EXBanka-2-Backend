// Package handler wires HTTP routes for notification-service.
package handler

import (
	"net/http"

	"banka-backend/services/notification-service/internal/domain"

	"github.com/gin-gonic/gin"
)

// NotificationHTTPHandler handles inbound HTTP requests.
type NotificationHTTPHandler struct {
	svc domain.NotificationService
}

// NewNotificationHTTPHandler registers notification routes.
func NewNotificationHTTPHandler(rg *gin.RouterGroup, svc domain.NotificationService) {
	h := &NotificationHTTPHandler{svc: svc}
	rg.POST("/email", h.SendEmail)
}

// SendEmail godoc
// POST /api/v1/notifications/email
func (h *NotificationHTTPHandler) SendEmail(c *gin.Context) {
	var req struct {
		To      string `json:"to"      binding:"required,email"`
		Subject string `json:"subject" binding:"required"`
		Body    string `json:"body"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	n := &domain.Notification{To: req.To, Subject: req.Subject, Body: req.Body}
	if err := h.svc.SendEmail(n); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "sent"})
}
