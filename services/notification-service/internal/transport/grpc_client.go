// Package transport manages outbound gRPC connections.
// notification-service is a gRPC CLIENT to user-service.
package transport

import (
	"context"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// UserServiceClient wraps the gRPC connection to user-service.
type UserServiceClient struct {
	conn         *grpc.ClientConn
	healthClient grpc_health_v1.HealthClient
	// TODO: add generated UserServiceClient after `make proto`:
	// userClient userv1.UserServiceClient
}

// NewUserServiceClient dials user-service and returns a ready client.
// target should be in the form "host:port" (e.g. "user-service:50051").
func NewUserServiceClient(target string) (*UserServiceClient, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(
		ctx,
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, err
	}

	log.Printf("[grpc-client] connected to user-service at %s", target)
	return &UserServiceClient{
		conn:         conn,
		healthClient: grpc_health_v1.NewHealthClient(conn),
	}, nil
}

// PingUserService performs a gRPC health check against user-service.
func (c *UserServiceClient) PingUserService(ctx context.Context) error {
	resp, err := c.healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return err
	}
	log.Printf("[grpc-client] user-service health: %s", resp.GetStatus())
	return nil
}

// Close releases the underlying connection.
func (c *UserServiceClient) Close() error {
	return c.conn.Close()
}
