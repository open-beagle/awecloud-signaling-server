package worker

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/open-beagle/signal-worker/pkg/proto"
)

type tokenCredentials struct{ token string }

func (c tokenCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + c.token}, nil
}
func (tokenCredentials) RequireTransportSecurity() bool { return false }

type Client struct {
	conn *grpc.ClientConn
	grpc pb.WorkerServiceClient
}

func NewClient(address, token string) (*Client, error) {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithPerRPCCredentials(tokenCredentials{token: token}))
	if err != nil {
		return nil, fmt.Errorf("create Worker gRPC client: %w", err)
	}
	return &Client{conn: conn, grpc: pb.NewWorkerServiceClient(conn)}, nil
}

func (c *Client) Close() error { return c.conn.Close() }
func (c *Client) Submit(ctx context.Context, req *pb.SubmitUserDeletionRequest) (*pb.UserDeletionJob, error) {
	return c.grpc.SubmitUserDeletion(ctx, req)
}
func (c *Client) Get(ctx context.Context, jobID string) (*pb.UserDeletionJob, error) {
	return c.grpc.GetUserDeletion(ctx, &pb.GetUserDeletionRequest{JobId: jobID})
}
func (c *Client) List(ctx context.Context, userIDs []uint64) ([]*pb.UserDeletionJob, error) {
	result, err := c.grpc.ListUserDeletionSummaries(ctx, &pb.ListUserDeletionSummariesRequest{UserIds: userIDs})
	if err != nil {
		return nil, err
	}
	return result.Jobs, nil
}
func (c *Client) Retry(ctx context.Context, req *pb.RetryUserDeletionRequest) (*pb.UserDeletionJob, error) {
	return c.grpc.RetryUserDeletion(ctx, req)
}
