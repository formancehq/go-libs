package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/formancehq/go-libs/v3/logging"

	"go.uber.org/fx"
)

type serverInfo struct {
	started chan struct{}
	address string
}

type serverInfoContextKey string

var serverInfoKey serverInfoContextKey = "_serverInfo"

func GetActualServerInfo(ctx context.Context) *serverInfo {
	siAsAny := ctx.Value(serverInfoKey)
	if siAsAny == nil {
		return nil
	}
	return siAsAny.(*serverInfo)
}

func ContextWithServerInfo(ctx context.Context) context.Context {
	return context.WithValue(ctx, serverInfoKey, &serverInfo{
		started: make(chan struct{}),
	})
}

func Started(ctx context.Context) chan struct{} {
	si := GetActualServerInfo(ctx)
	if si == nil {
		return nil
	}
	return si.started
}

func Address(ctx context.Context) string {
	si := GetActualServerInfo(ctx)
	if si == nil {
		return ""
	}
	return si.address
}

func URL(ctx context.Context) string {
	return fmt.Sprintf("http://%s", Address(ctx))
}

func StartedServer(ctx context.Context, listener net.Listener) {
	si := GetActualServerInfo(ctx)
	if si == nil {
		return
	}

	si.address = listener.Addr().String()

	close(si.started)
}

func (s *server) StartServer(ctx context.Context, handler http.Handler, options ...func(server *http.Server)) (func(ctx context.Context) error, error) {

	if s.listener == nil {
		if s.address == "" {
			return nil, errors.New("either address or listener must be provided")
		}
		listener, err := net.Listen("tcp", s.address)
		if err != nil {
			return nil, err
		}
		s.listener = listener
	}

	StartedServer(ctx, s.listener)

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	for _, option := range options {
		option(srv)
	}

	go func() {
		err := srv.Serve(s.listener)
		if err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()

	return func(ctx context.Context) error {
		return srv.Shutdown(ctx)
	}, nil
}

type server struct {
	listener       net.Listener
	address        string
	httpServerOpts []func(server *http.Server)
}

type serverOpts func(server *server)

func WithListener(listener net.Listener) serverOpts {
	return func(server *server) {
		server.listener = listener
	}
}

func WithAddress(addr string) serverOpts {
	return func(server *server) {
		server.address = addr
	}
}

func WithHttpServerOpts(opts ...func(server *http.Server)) serverOpts {
	return func(server *server) {
		server.httpServerOpts = opts
	}
}

func NewHook(handler http.Handler, options ...serverOpts) fx.Hook {
	var (
		close func(ctx context.Context) error
		err   error
	)

	s := &server{}
	for _, option := range options {
		option(s)
	}

	return fx.Hook{
		OnStart: func(ctx context.Context) error {
			logging.FromContext(ctx).Infof("Start HTTP server")
			defer func() {
				logging.FromContext(ctx).Infof("HTTP server started")
			}()
			close, err = s.StartServer(ctx, handler, s.httpServerOpts...)
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
