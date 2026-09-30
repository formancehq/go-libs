package grpcserver

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/transport/serverport"
)

func TestStartServerStopContextForcesGracefulStop(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := serverport.NewServer(serverPortDiscr, serverport.WithListener(listener))
	streamStarted := make(chan struct{})
	var closeStarted sync.Once

	stop, err := startServer(
		logging.TestingContext(),
		server,
		[]grpc.ServerOption{grpc.WaitForHandlers(true)},
		[]func(*grpc.Server){
			func(grpcServer *grpc.Server) {
				grpcServer.RegisterService(&grpc.ServiceDesc{
					ServiceName: "test.Blocking",
					HandlerType: (*any)(nil),
					Streams: []grpc.StreamDesc{
						{
							StreamName: "Watch",
							Handler: func(_ any, stream grpc.ServerStream) error {
								closeStarted.Do(func() {
									close(streamStarted)
								})
								<-stream.Context().Done()
								return stream.Context().Err()
							},
							ServerStreams: true,
							ClientStreams: true,
						},
					},
				}, &struct{}{})
			},
		},
	)
	require.NoError(t, err)

	connCtx, cancelConn := context.WithTimeout(context.Background(), time.Second)
	defer cancelConn()
	conn, err := grpc.DialContext(
		connCtx,
		listener.Addr().String(),
		grpc.WithBlock(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, conn.Close())
	})

	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()

	stream, err := conn.NewStream(
		streamCtx,
		&grpc.StreamDesc{
			ServerStreams: true,
			ClientStreams: true,
		},
		"/test.Blocking/Watch",
	)
	require.NoError(t, err)
	require.NoError(t, stream.SendMsg(&emptypb.Empty{}))

	select {
	case <-streamStarted:
	case <-time.After(time.Second):
		t.Fatal("stream handler did not start")
	}

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelStop()

	done := make(chan error, 1)
	go func() {
		done <- stop(stopCtx)
	}()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(time.Second):
		cancelStream()
		t.Fatal("stop did not return after context deadline")
	}
}

func TestStopServerFinishesShutdownBeforeReturning(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		stopCtx func() (context.Context, context.CancelFunc)
		wantErr error
	}{
		{
			name: "deadline reached while draining",
			stopCtx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 50*time.Millisecond)
			},
			wantErr: context.DeadlineExceeded,
		},
		{
			name: "context already expired",
			stopCtx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			wantErr: context.Canceled,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)

			handler := newBlockingHandler()
			grpcServer := grpc.NewServer()
			handler.register(grpcServer)
			served := make(chan struct{})
			go func() {
				defer close(served)
				_ = grpcServer.Serve(listener)
			}()

			handler.call(t, listener.Addr().String())

			stopCtx, cancelStop := tc.stopCtx()
			defer cancelStop()

			done := make(chan error, 1)
			go func() {
				done <- stopServer(stopCtx, grpcServer, served)
			}()

			// The handler ignores cancellation, so the forced Stop at the
			// deadline cannot end it: stopServer must still be waiting.
			<-stopCtx.Done()
			select {
			case err := <-done:
				close(handler.release)
				t.Fatalf("stop returned %v while a handler was still running", err)
			case <-time.After(150 * time.Millisecond):
			}

			close(handler.release)
			select {
			case err := <-done:
				require.ErrorIs(t, err, tc.wantErr)
			case <-time.After(5 * time.Second):
				t.Fatal("stop did not return after the handler did")
			}

			select {
			case <-handler.returned:
			default:
				t.Fatal("stop returned before the handler did")
			}
			select {
			case <-served:
			default:
				t.Fatal("stop returned before Serve did")
			}
		})
	}
}

func TestStartServerCooperativeStopReturnsNil(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	handler := newBlockingHandler()
	stop, err := startServer(
		logging.TestingContext(),
		serverport.NewServer(serverPortDiscr, serverport.WithListener(listener)),
		nil,
		[]func(*grpc.Server){handler.register},
	)
	require.NoError(t, err)

	handler.call(t, listener.Addr().String())

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStop()

	done := make(chan error, 1)
	go func() {
		done <- stop(stopCtx)
	}()

	// The drain waits for the in-flight RPC rather than cutting it short.
	select {
	case err := <-done:
		close(handler.release)
		t.Fatalf("stop returned %v before the in-flight RPC finished", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(handler.release)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not return after the in-flight RPC finished")
	}
}

// blockingHandler backs a single streaming RPC whose handler ignores its
// context and returns only once release is closed, standing in for a handler
// that does not honour cancellation.
type blockingHandler struct {
	inFlight chan struct{}
	release  chan struct{}
	returned chan struct{}
}

const blockingMethod = "/test.Holding/Hold"

func newBlockingHandler() *blockingHandler {
	return &blockingHandler{
		inFlight: make(chan struct{}),
		release:  make(chan struct{}),
		returned: make(chan struct{}),
	}
}

func (h *blockingHandler) register(grpcServer *grpc.Server) {
	grpcServer.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.Holding",
		HandlerType: (*any)(nil),
		Streams: []grpc.StreamDesc{
			{
				StreamName: "Hold",
				Handler: func(_ any, _ grpc.ServerStream) error {
					defer close(h.returned)
					close(h.inFlight)
					<-h.release
					return nil
				},
				ServerStreams: true,
				ClientStreams: true,
			},
		},
	}, &struct{}{})
}

// call opens the blocking RPC against addr and waits until its handler runs.
func (h *blockingHandler) call(t *testing.T, addr string) {
	t.Helper()

	conn := dial(t, addr)
	streamCtx, cancelStream := context.WithCancel(context.Background())
	t.Cleanup(cancelStream)

	stream, err := conn.NewStream(
		streamCtx,
		&grpc.StreamDesc{ServerStreams: true, ClientStreams: true},
		blockingMethod,
	)
	require.NoError(t, err)
	require.NoError(t, stream.SendMsg(&emptypb.Empty{}))

	select {
	case <-h.inFlight:
	case <-time.After(5 * time.Second):
		t.Fatal("blocking handler did not start")
	}
}

func dial(t *testing.T, addr string) *grpc.ClientConn {
	t.Helper()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, conn.Close())
	})
	return conn
}
