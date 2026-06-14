package httpserver

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/transport/serverport"
)

// safeBuffer is a goroutine-safe io.Writer used to capture log output emitted
// from the Serve goroutine.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// TestStartServerServeErrorDoesNotPanic is a regression test for EN-1160: an
// unexpected Serve error (anything other than http.ErrServerClosed) used to
// panic inside an unmonitored goroutine, killing the whole process and
// bypassing fx OnStop hooks. It must be logged instead.
func TestStartServerServeErrorDoesNotPanic(t *testing.T) {
	t.Parallel()

	buf := &safeBuffer{}
	ctx := logging.ContextWithLogger(context.Background(), logging.NewDefaultLogger(buf, false, false, false))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := serverport.NewServer(serverPortDiscr, serverport.WithListener(listener))

	stop, err := startServer(ctx, server, http.NewServeMux())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stop(context.Background())
	})

	// Close the listener out from under the server: Serve returns a
	// non-ErrServerClosed error. A panic in the Serve goroutine would crash
	// the test binary; instead the error must be logged.
	require.NoError(t, listener.Close())

	require.Eventually(t, func() bool {
		return strings.Contains(buf.String(), "failed to serve")
	}, 5*time.Second, 10*time.Millisecond)
}

// TestStartServerGracefulShutdownDoesNotLogError checks that a graceful
// shutdown (Serve returning http.ErrServerClosed) is not reported as an error.
func TestStartServerGracefulShutdownDoesNotLogError(t *testing.T) {
	t.Parallel()

	buf := &safeBuffer{}
	ctx := logging.ContextWithLogger(context.Background(), logging.NewDefaultLogger(buf, false, false, false))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := serverport.NewServer(serverPortDiscr, serverport.WithListener(listener))

	stop, err := startServer(ctx, server, http.NewServeMux())
	require.NoError(t, err)

	require.NoError(t, stop(context.Background()))

	// Give the Serve goroutine time to observe http.ErrServerClosed.
	time.Sleep(100 * time.Millisecond)
	require.NotContains(t, buf.String(), "failed to serve")
}

func TestStartServerSetsDefaultTimeouts(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := serverport.NewServer(serverPortDiscr, serverport.WithListener(listener))
	var configured struct {
		readHeaderTimeout time.Duration
		readTimeout       time.Duration
		writeTimeout      time.Duration
		idleTimeout       time.Duration
	}

	stop, err := startServer(
		logging.TestingContext(),
		server,
		http.NewServeMux(),
		func(server *http.Server) {
			configured.readHeaderTimeout = server.ReadHeaderTimeout
			configured.readTimeout = server.ReadTimeout
			configured.writeTimeout = server.WriteTimeout
			configured.idleTimeout = server.IdleTimeout
		},
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stop(context.Background())
	})

	require.Equal(t, 10*time.Second, configured.readHeaderTimeout)
	require.Equal(t, 30*time.Second, configured.readTimeout)
	require.Equal(t, 30*time.Second, configured.writeTimeout)
	require.Equal(t, 120*time.Second, configured.idleTimeout)
}
