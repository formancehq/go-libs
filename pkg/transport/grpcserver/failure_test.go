package grpcserver

import (
	"context"
	"errors"
	"net"
	"testing"
	"testing/synctest"
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

func TestServeFailureWithoutHandlerPreservesHealth(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options []ServerOptionModifier
	}{
		{name: "no handler"},
		{name: "nil handler", options: []ServerOptionModifier{WithServeErrorHandler(nil)}},
		{name: "disabled handler", options: []ServerOptionModifier{
			WithServeErrorHandler(func(error) { t.Error("disabled handler invoked") }),
			WithServeErrorHandler(nil),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				require.NoError(t, err)
				t.Cleanup(func() { _ = listener.Close() })
				probe := health.NewServer()
				probe.SetServingStatus("sibling", healthpb.HealthCheckResponse_SERVING)
				options := append([]ServerOptionModifier{
					WithServerPortOptions(serverport.WithListener(failedListener{listener, errors.New("fatal listener error")})),
					WithHealthServer(probe),
				}, tc.options...)
				hook := NewHook(options...)
				require.NoError(t, hook.OnStart(t.Context()))
				// Accept fails immediately. Wait for the serving goroutine to finish before
				// asserting the old caller-controlled health state, without a timing delay.
				synctest.Wait()
				for _, service := range []string{"", "sibling"} {
					response, err := probe.Check(t.Context(), &healthpb.HealthCheckRequest{Service: service})
					require.NoError(t, err)
					require.Equal(t, healthpb.HealthCheckResponse_SERVING, response.GetStatus())
				}
				probe.SetServingStatus("sibling", healthpb.HealthCheckResponse_NOT_SERVING)
				response, err := probe.Check(t.Context(), &healthpb.HealthCheckRequest{Service: "sibling"})
				require.NoError(t, err)
				require.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, response.GetStatus())
				probe.SetServingStatus("sibling", healthpb.HealthCheckResponse_SERVING)
				response, err = probe.Check(t.Context(), &healthpb.HealthCheckRequest{Service: "sibling"})
				require.NoError(t, err)
				require.Equal(t, healthpb.HealthCheckResponse_SERVING, response.GetStatus(), "caller updates must remain effective")
				require.NoError(t, hook.OnStop(t.Context()))
				response, err = probe.Check(t.Context(), &healthpb.HealthCheckRequest{})
				require.NoError(t, err)
				require.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, response.GetStatus(), "OnStop must still withdraw health")
			})
		})
	}
}
