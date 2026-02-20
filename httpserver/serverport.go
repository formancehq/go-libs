package httpserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v4/logging"
	"github.com/formancehq/go-libs/v4/serverport"
)

func ContextWithServerInfo(ctx context.Context) context.Context {
	return serverport.ContextWithServerInfo(ctx, serverPortDiscr)
}

const serverPortDiscr = "http"

func URL(ctx context.Context) string {
	return fmt.Sprintf("http://%s", serverport.Address(ctx, serverPortDiscr))
}

func startServer(ctx context.Context, s *serverport.Server, handler http.Handler, options ...func(server *http.Server)) (func(ctx context.Context) error, error) {

	if err := s.Listen(ctx); err != nil {
		return nil, err
	}

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	for _, option := range options {
		option(srv)
	}

	go func() {
		err := srv.Serve(s.Listener)
		if err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()

	return func(ctx context.Context) error {
		return srv.Shutdown(ctx)
	}, nil
}

type ServerOptions struct {
	serverOptions  []serverport.ServerOpts
	httpServerOpts []func(server *http.Server)
}

type ServerOptionModifier func(server *ServerOptions)

func WithServerPortOptions(opts ...serverport.ServerOpts) ServerOptionModifier {
	return func(serverOptions *ServerOptions) {
		serverOptions.serverOptions = append(serverOptions.serverOptions, opts...)
	}
}

func WithListener(listener net.Listener) ServerOptionModifier {
	return WithServerPortOptions(serverport.WithListener(listener))
}

func WithAddress(addr string) ServerOptionModifier {
	return WithServerPortOptions(serverport.WithAddress(addr))
}

func WithHttpServerOpts(opts ...func(server *http.Server)) ServerOptionModifier {
	return func(server *ServerOptions) {
		server.httpServerOpts = opts
	}
}

func NewHook(handler http.Handler, serverOptionModifiers ...ServerOptionModifier) fx.Hook {
	var (
		close func(ctx context.Context) error
		err   error
	)

	serverOptions := &ServerOptions{}
	for _, serverOptionModifier := range serverOptionModifiers {
		serverOptionModifier(serverOptions)
	}

	server := serverport.NewServer(serverPortDiscr)
	for _, option := range serverOptions.serverOptions {
		option(server)
	}

	return fx.Hook{
		OnStart: func(ctx context.Context) error {
			logging.FromContext(ctx).Infof("Start HTTP server")
			defer func() {
				logging.FromContext(ctx).Infof("HTTP server started")
			}()
			close, err = startServer(ctx, server, handler, serverOptions.httpServerOpts...)
			return err
		},
		OnStop: func(ctx context.Context) error {
			if close == nil {
				return nil
			}
			logging.FromContext(ctx).Infof("Stop HTTP server")
			defer func() {
				logging.FromContext(ctx).Infof("HTTP server stopped")
			}()
			return close(ctx)
		},
	}
}
