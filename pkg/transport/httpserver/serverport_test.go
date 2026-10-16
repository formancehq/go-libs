package httpserver

import (
	"bytes"
	"context"
	"fmt"
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

	stop, err := startServer(ctx, server, http.NewServeMux(), nil)
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

	stop, err := startServer(ctx, server, http.NewServeMux(), nil)
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
		&ServerOptions{httpServerOpts: []func(*http.Server){func(server *http.Server) {
			configured.readHeaderTimeout = server.ReadHeaderTimeout
			configured.readTimeout = server.ReadTimeout
			configured.writeTimeout = server.WriteTimeout
			configured.idleTimeout = server.IdleTimeout
		}}},
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stop(context.Background())
	})

	require.Equal(t, 10*time.Second, configured.readHeaderTimeout)
	require.Zero(t, configured.readTimeout)
	require.Zero(t, configured.writeTimeout)
	require.Equal(t, 120*time.Second, configured.idleTimeout)
}

func TestStartServerAllowsReadTimeoutOverride(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := serverport.NewServer(serverPortDiscr, serverport.WithListener(listener))
	var configured time.Duration

	stop, err := startServer(
		logging.TestingContext(),
		server,
		http.NewServeMux(),
		&ServerOptions{httpServerOpts: []func(*http.Server){func(server *http.Server) {
			server.ReadTimeout = time.Minute
			configured = server.ReadTimeout
		}}},
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stop(context.Background())
	})

	require.Equal(t, time.Minute, configured)
}

func TestHookShutdownFailureClosesOnlyWhenRequested(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprint(force), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			entered, disconnected, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				select {
				case <-r.Context().Done():
					close(disconnected)
				case <-release:
				}
			})
			options := []ServerOptionModifier{WithListener(listener)}
			if force {
				options = append(options, WithCloseOnShutdownError())
			}
			hook := NewHook(handler, options...)
			require.NoError(t, hook.OnStart(logging.TestingContext()))
			t.Cleanup(func() { close(release); _ = hook.OnStop(context.Background()) })
			requestCtx, cancelRequest := context.WithCancel(t.Context())
			defer cancelRequest()
			done := make(chan struct{})
			go func() {
				defer close(done)
				req, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, "http://"+listener.Addr().String(), nil)
				resp, err := http.DefaultClient.Do(req)
				if err == nil {
					_ = resp.Body.Close()
				}
			}()
			<-entered
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			require.ErrorIs(t, hook.OnStop(ctx), context.Canceled)
			if force {
				select {
				case <-disconnected:
				case <-time.After(time.Second):
					t.Fatal("active connection survived forced close")
				}
			} else {
				select {
				case <-disconnected:
					t.Fatal("default unexpectedly forced active connection closed")
				default:
				}
			}
			cancelRequest()
			<-done
		})
	}
}

func TestHookServeFailureCallbackAndNormalStop(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	failures := make(chan error, 1)
	hook := NewHook(http.NewServeMux(), WithListener(listener), WithServeErrorHandler(func(err error) { failures <- err }))
	require.NoError(t, hook.OnStart(logging.TestingContext()))
	require.NoError(t, listener.Close())
	select {
	case err := <-failures:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("missing Serve failure callback")
	}
	require.NoError(t, hook.OnStop(t.Context()))
	normal := NewHook(http.NewServeMux(), WithAddress("127.0.0.1:0"), WithServeErrorHandler(func(err error) { failures <- err }))
	require.NoError(t, normal.OnStart(logging.TestingContext()))
	require.NoError(t, normal.OnStop(t.Context()))
	select {
	case err := <-failures:
		t.Fatalf("normal stop reported failure: %v", err)
	default:
	}
}

func TestHookCanceledHostnameStartNeverListens(t *testing.T) {
	// This preserves the context-aware listener boundary when a host adopts NewHook.
	ctx, cancel := context.WithCancel(ContextWithServerInfo(logging.TestingContext()))
	cancel()
	hook := NewHook(http.NewServeMux(), WithAddress("localhost:0"))
	err := hook.OnStart(ctx)
	t.Cleanup(func() { _ = hook.OnStop(context.Background()) })
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, serverport.Address(ctx, serverPortDiscr))
	select {
	case <-serverport.Started(ctx, serverPortDiscr):
		t.Fatal("canceled hook advertised a listener")
	default:
	}
}

func TestCanceledHookStartLeavesSuppliedListenerCallerOwned(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	ctx, cancel := context.WithCancel(ContextWithServerInfo(logging.TestingContext()))
	cancel()
	hook := NewHook(http.NewServeMux(), WithListener(listener))
	require.ErrorIs(t, hook.OnStart(ctx), context.Canceled)
	require.NoError(t, hook.OnStop(t.Context()))
	require.Empty(t, serverport.Address(ctx, serverPortDiscr))
	select {
	case <-serverport.Started(ctx, serverPortDiscr):
		t.Fatal("failed start published listener")
	default:
	}
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	require.NoError(t, err)
	_ = conn.Close()
}
