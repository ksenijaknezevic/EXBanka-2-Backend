package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"banka-backend/services/user-service/internal/domain"
	"banka-backend/services/user-service/internal/handler"
	"banka-backend/services/user-service/mocks"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupHTTPRouter(svc domain.UserService) *gin.Engine {
	r := gin.New()
	api := r.Group("/api/v1/users")
	handler.NewUserHTTPHandler(api, svc)
	return r
}

func doRequest(r *gin.Engine, method, path string, body interface{}) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ─── Register ─────────────────────────────────────────────────────────────────

func TestHTTP_Register(t *testing.T) {
	tests := []struct {
		name       string
		body       interface{}
		setup      func(svc *mocks.MockUserService)
		wantStatus int
	}{
		{
			name: "success → 201",
			body: map[string]string{"name": "Alice", "email": "alice@test.com", "password": "SecurePass1!"},
			setup: func(svc *mocks.MockUserService) {
				svc.On("Register", "Alice", "alice@test.com", "SecurePass1!").
					Return(&domain.User{ID: "1", Name: "Alice", Email: "alice@test.com"}, nil)
			},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "missing email → 400",
			body:       map[string]string{"name": "Bob", "password": "pass1234"},
			setup:      func(svc *mocks.MockUserService) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "password too short → 400",
			body:       map[string]string{"name": "Bob", "email": "b@c.com", "password": "short"},
			setup:      func(svc *mocks.MockUserService) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "email already taken → 409",
			body: map[string]string{"name": "Bob", "email": "taken@test.com", "password": "SecurePass1!"},
			setup: func(svc *mocks.MockUserService) {
				svc.On("Register", "Bob", "taken@test.com", "SecurePass1!").
					Return(nil, domain.ErrEmailTaken)
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "internal error → 500",
			body: map[string]string{"name": "Charlie", "email": "c@test.com", "password": "SecurePass1!"},
			setup: func(svc *mocks.MockUserService) {
				svc.On("Register", "Charlie", "c@test.com", "SecurePass1!").
					Return(nil, assert.AnError)
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &mocks.MockUserService{}
			tc.setup(svc)
			r := setupHTTPRouter(svc)

			w := doRequest(r, http.MethodPost, "/api/v1/users/register", tc.body)
			assert.Equal(t, tc.wantStatus, w.Code)
			svc.AssertExpectations(t)
		})
	}
}

// ─── Login ────────────────────────────────────────────────────────────────────

func TestHTTP_Login(t *testing.T) {
	tests := []struct {
		name       string
		body       interface{}
		setup      func(svc *mocks.MockUserService)
		wantStatus int
		checkBody  func(t *testing.T, body map[string]interface{})
	}{
		{
			name: "success → 200 with tokens",
			body: map[string]string{"email": "user@test.com", "password": "Pass123!"},
			setup: func(svc *mocks.MockUserService) {
				svc.On("Login", "user@test.com", "Pass123!").
					Return("access-tok", "refresh-tok", nil)
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, body map[string]interface{}) {
				assert.Equal(t, "access-tok", body["access_token"])
				assert.Equal(t, "refresh-tok", body["refresh_token"])
			},
		},
		{
			name:       "missing password → 400",
			body:       map[string]string{"email": "user@test.com"},
			setup:      func(svc *mocks.MockUserService) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid credentials → 401",
			body: map[string]string{"email": "user@test.com", "password": "wrong"},
			setup: func(svc *mocks.MockUserService) {
				svc.On("Login", "user@test.com", "wrong").
					Return("", "", domain.ErrInvalidCredentials)
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "internal error → 500",
			body: map[string]string{"email": "user@test.com", "password": "Pass123!"},
			setup: func(svc *mocks.MockUserService) {
				svc.On("Login", "user@test.com", "Pass123!").
					Return("", "", assert.AnError)
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &mocks.MockUserService{}
			tc.setup(svc)
			r := setupHTTPRouter(svc)

			w := doRequest(r, http.MethodPost, "/api/v1/users/login", tc.body)
			assert.Equal(t, tc.wantStatus, w.Code)

			if tc.checkBody != nil {
				var body map[string]interface{}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				tc.checkBody(t, body)
			}
			svc.AssertExpectations(t)
		})
	}
}

// ─── GetUser ──────────────────────────────────────────────────────────────────

func TestHTTP_GetUser(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		setup      func(svc *mocks.MockUserService)
		wantStatus int
	}{
		{
			name: "found → 200",
			id:   "7",
			setup: func(svc *mocks.MockUserService) {
				svc.On("GetByID", "7").Return(&domain.User{ID: "7", Name: "Dave"}, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "not found → 404",
			id:   "999",
			setup: func(svc *mocks.MockUserService) {
				svc.On("GetByID", "999").Return(nil, domain.ErrUserNotFound)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "internal error → 500",
			id:   "1",
			setup: func(svc *mocks.MockUserService) {
				svc.On("GetByID", "1").Return(nil, assert.AnError)
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &mocks.MockUserService{}
			tc.setup(svc)
			r := setupHTTPRouter(svc)

			w := doRequest(r, http.MethodGet, "/api/v1/users/"+tc.id, nil)
			assert.Equal(t, tc.wantStatus, w.Code)
			svc.AssertExpectations(t)
		})
	}
}
