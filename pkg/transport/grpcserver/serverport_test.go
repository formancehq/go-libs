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
		nil,
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
