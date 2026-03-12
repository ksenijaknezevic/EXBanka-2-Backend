package interceptor

import (
	"context"

	"banka-backend/services/user-service/internal/utils"
)

// NewContextWithClaims injects pre-built JWT access claims into ctx using the
// same context key that the production Unary() interceptor uses.
//
// This is intended exclusively for unit tests where you want to simulate an
// authenticated gRPC request without running the full interceptor chain.
//
// Example:
//
//	ctx := interceptor.NewContextWithClaims(context.Background(), &utils.AccessClaims{
//	    UserType: "ADMIN",
//	})
func NewContextWithClaims(ctx context.Context, claims *utils.AccessClaims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}
