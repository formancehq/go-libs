package grpcserver

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/formancehq/go-libs/v5/pkg/transport/serverport"
)

type failedListener struct {
	net.Listener
	failure error
}

func (l failedListener) Accept() (net.Conn, error) { return nil, l.failure }

func TestUnexpectedServeFailureWithdrawsHealthBeforeNotification(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	failure := errors.New("listener failed")
	probe := health.NewServer()
	notified := make(chan error, 1)
	hook := NewHook(WithServerPortOptions(serverport.WithListener(failedListener{listener, failure})), WithHealthServer(probe), WithServeErrorHandler(func(err error) {
		response, checkErr := probe.Check(t.Context(), &healthpb.HealthCheckRequest{})
		if checkErr != nil {
			notified <- checkErr
			return
		}
		if response.GetStatus() != healthpb.HealthCheckResponse_NOT_SERVING {
			notified <- errors.New("health still serving")
			return
		}
		notified <- err
	}))
	require.NoError(t, hook.OnStart(t.Context()))
	select {
	case err := <-notified:
		require.ErrorIs(t, err, failure)
	case <-time.After(5 * time.Second):
		t.Fatal("missing failure notification")
	}
	response, err := probe.Check(t.Context(), &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	require.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, response.GetStatus())
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, hook.OnStop(ctx))
}

func TestNormalStopDoesNotNotifyServeFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	notified := make(chan error, 1)
	hook := NewHook(WithServerPortOptions(serverport.WithListener(listener)), WithServeErrorHandler(func(err error) { notified <- err }))
	require.NoError(t, hook.OnStart(t.Context()))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, hook.OnStop(ctx))
	select {
	case err := <-notified:
		t.Fatalf("normal stop notified: %v", err)
	default:
	}
}

func TestListenerStartupFailureReturnsWithoutServeNotification(t *testing.T) {
	notified := make(chan error, 1)
	hook := NewHook(WithServerPortOptions(serverport.WithAddress("127.0.0.1:invalid-port")), WithServeErrorHandler(func(err error) { notified <- err }))
	require.Error(t, hook.OnStart(t.Context()))
	require.NoError(t, hook.OnStop(t.Context()))
	select {
	case err := <-notified:
		t.Fatalf("startup failure notified as Serve failure: %v", err)
	default:
	}
}
