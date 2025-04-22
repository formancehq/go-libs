package serverport

import (
	"context"
	"errors"
	"net"
)

type ServerInfo struct {
	started chan struct{}
	address string
}

type serverInfoContextKey string

func GetActualServerInfo(ctx context.Context, discr string) *ServerInfo {
	siAsAny := ctx.Value(serverInfoContextKey(discr))
	if siAsAny == nil {
		return nil
	}
	return siAsAny.(*ServerInfo)
}

func ContextWithServerInfo(ctx context.Context, discr string) context.Context {
	return context.WithValue(ctx, serverInfoContextKey(discr), &ServerInfo{
		started: make(chan struct{}),
	})
}

func Started(ctx context.Context, discr string) chan struct{} {
	si := GetActualServerInfo(ctx, discr)
	if si == nil {
		return nil
	}
	return si.started
}

func Address(ctx context.Context, discr string) string {
	si := GetActualServerInfo(ctx, discr)
	if si == nil {
		return ""
	}
	return si.address
}

func StartedServer(ctx context.Context, listener net.Listener, discr string) {
	si := GetActualServerInfo(ctx, discr)
	if si == nil {
		return
	}

	si.address = listener.Addr().String()

	close(si.started)
}

type Server struct {
	Listener net.Listener
	Address  string
	Discr    string
}

func (s *Server) Listen(ctx context.Context) error {
	if s.Listener == nil {
		if s.Address == "" {
			return errors.New("either address or listener must be provided")
		}
		listener, err := net.Listen("tcp", s.Address)
		if err != nil {
			return err
		}
		s.Listener = listener
	}

	StartedServer(ctx, s.Listener, s.Discr)

	return nil
}

func NewServer(discr string, opts ...ServerOpts) *Server {
	server := &Server{
		Discr: discr,
	}

	for _, opt := range opts {
		opt(server)
	}

	return server
}

type ServerOpts func(server *Server)

func WithListener(listener net.Listener) ServerOpts {
	return func(server *Server) {
		server.Listener = listener
	}
}

func WithAddress(addr string) ServerOpts {
	return func(server *Server) {
		server.Address = addr
	}
}
