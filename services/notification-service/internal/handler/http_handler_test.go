package handler_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"banka-backend/services/notification-service/internal/domain"
	"banka-backend/services/notification-service/internal/handler"
	"banka-backend/services/notification-service/mocks"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupHTTPRouter(svc *mocks.MockNotificationService) *gin.Engine {
	r := gin.New()
	rg := r.Group("/api/v1/notifications")
	handler.NewNotificationHTTPHandler(rg, svc)
	return r
}

func doHTTPRequest(r *gin.Engine, method, path string, body interface{}) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ─── SendEmail ────────────────────────────────────────────────────────────────

func TestHTTP_SendEmail(t *testing.T) {
	tests := []struct {
		name     string
		body     interface{}
		setup    func(svc *mocks.MockNotificationService)
		wantCode int
		wantKey  string
	}{
		{
			name: "success",
			body: map[string]string{"to": "user@example.com", "subject": "Hello", "body": "<p>hi</p>"},
			setup: func(svc *mocks.MockNotificationService) {
				svc.On("SendEmail", &domain.Notification{
					To:      "user@example.com",
					Subject: "Hello",
					Body:    "<p>hi</p>",
				}).Return(nil)
			},
			wantCode: http.StatusOK,
			wantKey:  "status",
		},
		{
			name:     "missing to field",
			body:     map[string]string{"subject": "Hello"},
			setup:    func(_ *mocks.MockNotificationService) {},
			wantCode: http.StatusBadRequest,
			wantKey:  "error",
		},
		{
			name:     "invalid email address",
			body:     map[string]string{"to": "not-an-email", "subject": "Hello"},
			setup:    func(_ *mocks.MockNotificationService) {},
			wantCode: http.StatusBadRequest,
			wantKey:  "error",
		},
		{
			name:     "missing subject field",
			body:     map[string]string{"to": "user@example.com"},
			setup:    func(_ *mocks.MockNotificationService) {},
			wantCode: http.StatusBadRequest,
			wantKey:  "error",
		},
		{
			name: "service error",
			body: map[string]string{"to": "user@example.com", "subject": "Hello"},
			setup: func(svc *mocks.MockNotificationService) {
				svc.On("SendEmail", &domain.Notification{
					To:      "user@example.com",
					Subject: "Hello",
					Body:    "",
				}).Return(errors.New("smtp failure"))
			},
			wantCode: http.StatusInternalServerError,
			wantKey:  "error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &mocks.MockNotificationService{}
			tc.setup(svc)
			r := setupHTTPRouter(svc)

			w := doHTTPRequest(r, http.MethodPost, "/api/v1/notifications/email", tc.body)
			assert.Equal(t, tc.wantCode, w.Code)

			var resp map[string]interface{}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Contains(t, resp, tc.wantKey)

			svc.AssertExpectations(t)
		})
	}
}
