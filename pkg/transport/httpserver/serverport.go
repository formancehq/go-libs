package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/transport/serverport"
)

// Hook represents a lifecycle hook for the HTTP server.
// OnStart starts the server, OnStop shuts it down gracefully.
type Hook struct {
	OnStart func(ctx context.Context) error
	OnStop  func(ctx context.Context) error
}

func ContextWithServerInfo(ctx context.Context) context.Context {
	return serverport.ContextWithServerInfo(ctx, serverPortDiscr)
}

const serverPortDiscr = "http"

func URL(ctx context.Context) string {
	return fmt.Sprintf("http://%s", serverport.Address(ctx, serverPortDiscr))
}

func startServer(ctx context.Context, s *serverport.Server, handler http.Handler, options *ServerOptions) (func(context.Context) error, error) {
	if options == nil {
		options = &ServerOptions{}
	}

	if err := s.Listen(ctx); err != nil {
		return nil, err
	}

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	for _, option := range options.httpServerOpts {
		option(srv)
	}

	go func() {
		if err := srv.Serve(s.Listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logging.FromContext(ctx).Errorf("failed to serve: %v", err)
			if options.serveErrorHandler != nil {
				options.serveErrorHandler(err)
			}
		}
	}()

	return func(ctx context.Context) error {
		err := srv.Shutdown(ctx)
		if err != nil && options.closeOnShutdownError {
			_ = srv.Close()
		}
		return err
	}, nil
}

type ServerOptions struct {
	closeOnShutdownError bool
	serveErrorHandler    func(error)
	serverOptions        []serverport.ServerOpts
	httpServerOpts       []func(server *http.Server)
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

// WithCloseOnShutdownError closes active connections if graceful shutdown fails.
// OnStop still returns the original shutdown error. The default leaves connections
// open so the caller can decide how to recover from a failed drain.
func WithCloseOnShutdownError() ServerOptionModifier {
	return func(options *ServerOptions) { options.closeOnShutdownError = true }
}

// WithServeErrorHandler observes an unexpected Serve failure after it is logged.
// It is not called for normal shutdown or listen errors. The callback runs on
// the serving goroutine and must return promptly; it must not wait for OnStop.
func WithServeErrorHandler(handler func(error)) ServerOptionModifier {
	return func(options *ServerOptions) { options.serveErrorHandler = handler }
}

func NewHook(handler http.Handler, serverOptionModifiers ...ServerOptionModifier) Hook {
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

	return Hook{
		OnStart: func(ctx context.Context) error {
			logging.FromContext(ctx).Infof("Start HTTP server")
			defer func() {
				logging.FromContext(ctx).Infof("HTTP server started")
			}()
			close, err = startServer(ctx, server, handler, serverOptions)
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
