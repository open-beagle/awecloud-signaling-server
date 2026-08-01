package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

func TestHeartbeatResponseContextPreservesIncomingAuthorizationAfterDetach(t *testing.T) {
	incoming := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer fixture-token"))
	cancelable, cancel := context.WithCancel(incoming)
	cancel()

	detached := heartbeatResponseContext(cancelable)
	require.Nil(t, detached.Err())
	require.Nil(t, detached.Done())
	values, ok := metadata.FromIncomingContext(detached)
	require.True(t, ok)
	require.Equal(t, []string{"Bearer fixture-token"}, values.Get("authorization"))
}
