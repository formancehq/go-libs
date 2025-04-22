package grpcserver

import (
	"context"

	"github.com/formancehq/go-libs/v3/logging"
	"github.com/formancehq/go-libs/v3/serverport"
	"go.uber.org/fx"
	"google.golang.org/grpc"
)

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
	go func() {
		if err := grpcServer.Serve(s.Listener); err != nil {
			logging.FromContext(ctx).Errorf("failed to serve: %v", err)
		}
	}()

	return func(ctx context.Context) error {
		grpcServer.GracefulStop()

		return nil
	}, nil
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

func NewHook(serverOptionsModifiers ...ServerOptionModifier) fx.Hook {
	var (
		close func(ctx context.Context) error
		err   error
	)

	options := &ServerOptions{}
	for _, option := range serverOptionsModifiers {
		option(options)
	}

	server := serverport.NewServer(serverPortDiscr, options.serverPortOptions...)

	return fx.Hook{
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
