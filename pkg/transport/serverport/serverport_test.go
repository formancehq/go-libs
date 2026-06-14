package serverport

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStartedServerCanBeCalledMoreThanOnce(t *testing.T) {
	t.Parallel()

	ctx := ContextWithServerInfo(context.Background(), "test")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, listener.Close())
	})

	require.NotPanics(t, func() {
		StartedServer(ctx, listener, "test")
		StartedServer(ctx, listener, "test")
	})

	select {
	case <-Started(ctx, "test"):
	default:
		t.Fatal("server start channel was not closed")
	}
	require.Equal(t, listener.Addr().String(), Address(ctx, "test"))
}
