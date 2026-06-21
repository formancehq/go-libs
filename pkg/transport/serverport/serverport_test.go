package serverport

import (
	"context"
	"net"
	"sync"
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

func TestStartedServerCanBeCalledConcurrently(t *testing.T) {
	t.Parallel()

	ctx := ContextWithServerInfo(context.Background(), "test")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, listener.Close())
	})

	var wg sync.WaitGroup
	start := make(chan struct{})
	panicCh := make(chan any, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCh <- r
				}
			}()
			<-start
			StartedServer(ctx, listener, "test")
		}()
	}
	close(start)
	wg.Wait()

	select {
	case p := <-panicCh:
		t.Fatalf("StartedServer panicked under concurrent calls: %v", p)
	default:
	}
	select {
	case <-Started(ctx, "test"):
	default:
		t.Fatal("server start channel was not closed")
	}
	require.Equal(t, listener.Addr().String(), Address(ctx, "test"))
}
