package httpserver_test

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/formancehq/go-libs/v2/httpserver"
	"github.com/formancehq/go-libs/v2/logging"
	"github.com/stretchr/testify/require"
)

func TestContextWithServerInfo(t *testing.T) {
	t.Parallel()

	// Create context with server info
	ctx := context.Background()
	ctx = httpserver.ContextWithServerInfo(ctx)

	// Verify server info is in context
	si := httpserver.GetActualServerInfo(ctx)
	require.NotNil(t, si)

	// Verify started channel is not closed yet
	select {
	case <-httpserver.Started(ctx):
		t.Fatal("Started channel should not be closed yet")
	default:
		// Expected
	}
}

func TestStartedServer(t *testing.T) {
	t.Parallel()

	// Create context with server info
	ctx := context.Background()
	ctx = httpserver.ContextWithServerInfo(ctx)

	// Create a listener
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	defer listener.Close()

	// Call StartedServer
	httpserver.StartedServer(ctx, listener)

	// Verify address is set
	address := httpserver.Address(ctx)
	require.NotEmpty(t, address)
	require.Equal(t, listener.Addr().String(), address)

	// Verify started channel is closed
	select {
	case <-httpserver.Started(ctx):
		// Expected
	default:
		t.Fatal("Started channel should be closed")
	}
}

func TestURL(t *testing.T) {
	t.Parallel()

	// Create context with server info
	ctx := context.Background()
	ctx = httpserver.ContextWithServerInfo(ctx)

	// Create a listener
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	defer listener.Close()

	// Call StartedServer
	httpserver.StartedServer(ctx, listener)

	// Verify URL is correct
	url := httpserver.URL(ctx)
	require.Equal(t, "http://"+listener.Addr().String(), url)
}

func TestServerHook(t *testing.T) {
	t.Parallel()

	// Create a simple handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Create context with server info and logger
	var buf bytes.Buffer
	logger := logging.NewDefaultLogger(&buf, false, false, false)
	ctx := logging.ContextWithLogger(context.Background(), logger)
	ctx = httpserver.ContextWithServerInfo(ctx)

	// Create a hook with address option
	hook := httpserver.NewHook(handler, httpserver.WithAddress("localhost:0"))

	// Start server
	err := hook.OnStart(ctx)
	require.NoError(t, err)

	// Wait for server to start
	select {
	case <-httpserver.Started(ctx):
		// Server started
	case <-time.After(time.Second):
		t.Fatal("Server did not start in time")
	}

	// Get server address
	address := httpserver.Address(ctx)
	require.NotEmpty(t, address)

	// Make a request to the server
	resp, err := http.Get("http://" + address)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Verify response
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Stop server
	err = hook.OnStop(ctx)
	require.NoError(t, err)
}
