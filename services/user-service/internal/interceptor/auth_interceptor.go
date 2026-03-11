// Package interceptor provides gRPC server-side interceptors.
package interceptor

import (
	"context"
	"strings"

	pb "banka-backend/proto/user"
	"banka-backend/services/user-service/internal/utils"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// contextKey is an unexported type to prevent key collisions in context values.
type contextKey string

// claimsKey is the key under which *utils.AccessClaims is stored in the context.
const claimsKey contextKey = "jwt_claims"

// publicMethods lists RPC full-method paths that do NOT require a valid JWT.
// Uses proto-generated constants to avoid typos and keep this in sync with the proto.
var publicMethods = map[string]struct{}{
	pb.UserService_HealthCheck_FullMethodName:     {},
	pb.UserService_Login_FullMethodName:           {},
	pb.UserService_SetPassword_FullMethodName:     {}, // activation token is the credential, no access token
	pb.UserService_ActivateAccount_FullMethodName: {},
	pb.UserService_RefreshToken_FullMethodName:    {}, // carries a refresh token, not an access token
	pb.UserService_ForgotPassword_FullMethodName:  {}, // unauthenticated — only an email is provided
	pb.UserService_ResetPassword_FullMethodName:   {}, // reset token is the credential, no access token
}

// AuthInterceptor verifies JWT access tokens on all protected RPCs.
type AuthInterceptor struct {
	accessSecret string
}

// NewAuthInterceptor constructs an AuthInterceptor using the given HMAC secret.
func NewAuthInterceptor(accessSecret string) *AuthInterceptor {
	return &AuthInterceptor{accessSecret: accessSecret}
}

// Unary returns a grpc.UnaryServerInterceptor that:
//  1. Skips auth for whitelisted public methods.
//  2. Extracts the Bearer token from the incoming "authorization" metadata header.
//  3. Verifies the token and injects *utils.AccessClaims into the context.
func (a *AuthInterceptor) Unary() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		if _, public := publicMethods[info.FullMethod]; public {
			return handler(ctx, req)
		}

		claims, err := a.extractClaims(ctx)
		if err != nil {
			return nil, err
		}

		return handler(context.WithValue(ctx, claimsKey, claims), req)
	}
}

// ClaimsFromContext retrieves the access claims injected by AuthInterceptor.
// Returns (nil, false) on public routes where no claims are present.
func ClaimsFromContext(ctx context.Context) (*utils.AccessClaims, bool) {
	claims, ok := ctx.Value(claimsKey).(*utils.AccessClaims)
	return claims, ok
}

// extractClaims reads and validates the Bearer token from incoming gRPC metadata.
// gRPC-Gateway forwards the HTTP Authorization header as lowercase "authorization".
func (a *AuthInterceptor) extractClaims(ctx context.Context) (*utils.AccessClaims, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}

	vals := md.Get("authorization")
	if len(vals) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing authorization header")
	}

	raw := vals[0]
	if !strings.HasPrefix(raw, "Bearer ") {
		return nil, status.Error(codes.Unauthenticated, "authorization header must use Bearer scheme")
	}
	tokenStr := strings.TrimPrefix(raw, "Bearer ")

	claims, err := utils.VerifyToken(tokenStr, a.accessSecret)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
	}
	return claims, nil
}
