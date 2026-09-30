package grpcserver

import (
	"context"

	"google.golang.org/grpc"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/transport/serverport"
)

// Hook represents a lifecycle hook for the gRPC server.
// OnStart starts the server, OnStop shuts it down gracefully: it drains
// in-flight RPCs until its context expires, then closes every transport, and
// returns only once the server has fully stopped.
type Hook struct {
	OnStart func(ctx context.Context) error
	OnStop  func(ctx context.Context) error
}

const serverPortDiscr = "grpc"

func ContextWithServerInfo(ctx context.Context) context.Context {
	return serverport.ContextWithServerInfo(ctx, serverPortDiscr)
}

func startServer(ctx context.Context, s *serverport.Server, serverOptions []grpc.ServerOption, setupOptions []func(*grpc.Server)) (func(ctx context.Context) error, error) {

	if err := s.Listen(ctx); err != nil {
		return nil, err
	}

	grpcServer := grpc.NewServer(serverOptions...)
	for _, option := range setupOptions {
		option(grpcServer)
	}
	served := make(chan struct{})
	go func() {
		defer close(served)
		if err := grpcServer.Serve(s.Listener); err != nil {
			logging.FromContext(ctx).Errorf("failed to serve: %v", err)
		}
	}()

	return func(ctx context.Context) error {
		return stopServer(ctx, grpcServer, served)
	}, nil
}

// stopServer drains in-flight RPCs until ctx expires, then closes every
// transport. It returns only once GracefulStop has returned (all handlers have
// exited) and Serve has returned (served is closed), including when ctx has
// already expired, so the caller never moves on mid-shutdown. It returns
// ctx.Err() when the drain was cut short.
func stopServer(ctx context.Context, grpcServer *grpc.Server, served <-chan struct{}) error {
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		grpcServer.GracefulStop()
	}()

	select {
	case <-stopped:
		<-served
		return nil
	case <-ctx.Done():
		grpcServer.Stop()
		<-stopped
		<-served
		return ctx.Err()
	}
}

func Address(ctx context.Context) string {
	return serverport.Address(ctx, serverPortDiscr)
}

type ServerOptions struct {
	serverPortOptions []serverport.ServerOpts
	grpcServerOpts    []grpc.ServerOption
	grpcSetupOpts     []func(server *grpc.Server)
}

type ServerOptionModifier func(server *ServerOptions)

func WithServerPortOptions(opts ...serverport.ServerOpts) ServerOptionModifier {
	return func(serverOptions *ServerOptions) {
		serverOptions.serverPortOptions = append(serverOptions.serverPortOptions, opts...)
	}
}

func WithGRPCServerOptions(opts ...grpc.ServerOption) ServerOptionModifier {
	return func(serverOptions *ServerOptions) {
		serverOptions.grpcServerOpts = append(serverOptions.grpcServerOpts, opts...)
	}
}

func WithGRPCSetupOptions(opts ...func(server *grpc.Server)) ServerOptionModifier {
	return func(serverOptions *ServerOptions) {
		serverOptions.grpcSetupOpts = append(serverOptions.grpcSetupOpts, opts...)
	}
}

func NewHook(serverOptionsModifiers ...ServerOptionModifier) Hook {
	var (
		close func(ctx context.Context) error
		err   error
	)

	options := &ServerOptions{}
	for _, option := range serverOptionsModifiers {
		option(options)
	}

	server := serverport.NewServer(serverPortDiscr, options.serverPortOptions...)

	return Hook{
		OnStart: func(ctx context.Context) error {
			logging.FromContext(ctx).Infof("starting GRPC server")
			close, err = startServer(
				ctx,
				server,
				options.grpcServerOpts,
				options.grpcSetupOpts,
			)
			return err
		},
		OnStop: func(ctx context.Context) error {
			if close == nil {
				return nil
			}
			logging.FromContext(ctx).Infof("Stop GRPC server")
			defer func() {
				logging.FromContext(ctx).Infof("GRPC server stopped")
			}()
			return close(ctx)
		},
	}
}
