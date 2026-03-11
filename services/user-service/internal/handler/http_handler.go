// Package handler wires HTTP routes to use-case logic.
// Clean Architecture: interface / delivery layer.
package handler

import (
	"errors"
	"net/http"

	"banka-backend/services/user-service/internal/domain"

	"github.com/gin-gonic/gin"
)

// UserHTTPHandler holds the router group and service dependency.
type UserHTTPHandler struct {
	svc domain.UserService
}

// NewUserHTTPHandler registers all user routes on the provided router group.
func NewUserHTTPHandler(rg *gin.RouterGroup, svc domain.UserService) {
	h := &UserHTTPHandler{svc: svc}

	rg.POST("/register", h.Register)
	rg.POST("/login", h.Login)
	rg.GET("/:id", h.GetUser)
}

// ─── handlers ─────────────────────────────────────────────────────────────────

// Register godoc
// POST /api/v1/users/register
func (h *UserHTTPHandler) Register(c *gin.Context) {
	var req struct {
		Name     string `json:"name"     binding:"required"`
		Email    string `json:"email"    binding:"required,email"`
		Password string `json:"password" binding:"required,min=8"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.svc.Register(req.Name, req.Email, req.Password)
	if err != nil {
		if errors.Is(err, domain.ErrEmailTaken) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"user": user})
}

// Login godoc
// POST /api/v1/users/login
func (h *UserHTTPHandler) Login(c *gin.Context) {
	var req struct {
		Email    string `json:"email"    binding:"required,email"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	access, refresh, err := h.svc.Login(req.Email, req.Password)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCredentials) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  access,
		"refresh_token": refresh,
	})
}

// GetUser godoc
// GET /api/v1/users/:id
func (h *UserHTTPHandler) GetUser(c *gin.Context) {
	user, err := h.svc.GetByID(c.Param("id"))
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}
