package interceptor_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "banka-backend/proto/user"
	"banka-backend/services/user-service/internal/interceptor"
	"banka-backend/services/user-service/internal/testutil"
	"banka-backend/services/user-service/internal/utils"
)

const testAccessSecret = testutil.TestAccessSecret

// runInterceptor invokes the AuthInterceptor.Unary() for the given full-method path.
// handler is called only when the interceptor allows the request through.
func runInterceptor(ctx context.Context, fullMethod string, secret string) (interface{}, error) {
	ai := interceptor.NewAuthInterceptor(secret)
	info := &grpc.UnaryServerInfo{FullMethod: fullMethod}

	var capturedCtx context.Context
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		capturedCtx = ctx
		return "ok", nil
	}

	resp, err := ai.Unary()(ctx, nil, info, handler)
	if err == nil {
		_ = capturedCtx // used in sub-tests
	}
	return resp, err
}

// ─── Public method bypass ─────────────────────────────────────────────────────

func TestAuthInterceptor_PublicMethods_Bypass(t *testing.T) {
	publicMethods := []string{
		pb.UserService_HealthCheck_FullMethodName,
		pb.UserService_Login_FullMethodName,
		pb.UserService_SetPassword_FullMethodName,
		pb.UserService_ActivateAccount_FullMethodName,
		pb.UserService_RefreshToken_FullMethodName,
		pb.UserService_ForgotPassword_FullMethodName,
		pb.UserService_ResetPassword_FullMethodName,
	}

	for _, method := range publicMethods {
		t.Run(method, func(t *testing.T) {
			// Empty context — no metadata at all
			resp, err := runInterceptor(context.Background(), method, testAccessSecret)
			require.NoError(t, err)
			assert.Equal(t, "ok", resp)
		})
	}
}

// ─── Protected method: valid token ────────────────────────────────────────────

func TestAuthInterceptor_ValidToken_InjectsClaimsInContext(t *testing.T) {
	token := testutil.MakeAccessToken("42", "user@test.com", "EMPLOYEE", []string{"VIEW_ACCOUNTS"})
	ctx := testutil.GRPCIncomingContext(token)

	ai := interceptor.NewAuthInterceptor(testAccessSecret)
	info := &grpc.UnaryServerInfo{FullMethod: pb.UserService_GetEmployeeByID_FullMethodName}

	var gotClaims *utils.AccessClaims
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		claims, ok := interceptor.ClaimsFromContext(ctx)
		require.True(t, ok)
		gotClaims = claims
		return "ok", nil
	}

	resp, err := ai.Unary()(ctx, nil, info, handler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	require.NotNil(t, gotClaims)
	assert.Equal(t, "42", gotClaims.Subject)
	assert.Equal(t, "EMPLOYEE", gotClaims.UserType)
}

// ─── Protected method: error cases ────────────────────────────────────────────

func TestAuthInterceptor_Errors(t *testing.T) {
	protectedMethod := pb.UserService_GetAllEmployees_FullMethodName

	tests := []struct {
		name     string
		ctx      context.Context
		wantCode codes.Code
	}{
		{
			name:     "no metadata",
			ctx:      context.Background(),
			wantCode: codes.Unauthenticated,
		},
		{
			name:     "missing authorization header",
			ctx:      metadata.NewIncomingContext(context.Background(), metadata.Pairs()),
			wantCode: codes.Unauthenticated,
		},
		{
			name:     "non-bearer scheme",
			ctx:      metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Basic dXNlcjpwYXNz")),
			wantCode: codes.Unauthenticated,
		},
		{
			name:     "malformed token",
			ctx:      metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer not.a.real.token")),
			wantCode: codes.Unauthenticated,
		},
		{
			name: "refresh token used as access token",
			ctx: func() context.Context {
				_, refresh, _ := utils.GenerateTokens("1", "a@b.com", "EMPLOYEE", nil,
					testAccessSecret, testutil.TestRefreshSecret)
				return testutil.GRPCIncomingContext(refresh)
			}(),
			wantCode: codes.Unauthenticated,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runInterceptor(tc.ctx, protectedMethod, testAccessSecret)
			require.Error(t, err)
			s, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, s.Code())
		})
	}
}

// ─── ClaimsFromContext on empty context ────────────────────────────────────────

func TestClaimsFromContext_NoClaims(t *testing.T) {
	claims, ok := interceptor.ClaimsFromContext(context.Background())
	assert.False(t, ok)
	assert.Nil(t, claims)
}
