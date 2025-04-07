package httpserver

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/formancehq/go-libs/v2/logging"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

func TestNewHook(t *testing.T) {
	handler := http.NewServeMux()
	hook := NewHook(handler, WithAddress(":0"))

	require.NotNil(t, hook, "Le hook ne devrait pas être nil")
	require.IsType(t, fx.Hook{}, hook, "Le hook devrait être de type fx.Hook")
}

func TestWithListener(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err, "La création du listener ne devrait pas échouer")
	defer listener.Close()

	s := &server{}
	opt := WithListener(listener)
	opt(s)

	require.Equal(t, listener, s.listener, "Le listener devrait être correctement défini")
}

func TestWithAddress(t *testing.T) {
	s := &server{}
	opt := WithAddress(":8080")
	opt(s)

	require.Equal(t, ":8080", s.address, "L'adresse devrait être correctement définie")
}

func TestWithHttpServerOpts(t *testing.T) {
	s := &server{}

	opt1 := func(server *http.Server) {
		server.ReadTimeout = 10 * time.Second
	}

	opt2 := func(server *http.Server) {
		server.WriteTimeout = 20 * time.Second
	}

	opt := WithHttpServerOpts(opt1, opt2)
	opt(s)

	require.Len(t, s.httpServerOpts, 2, "Les options HTTP devraient être correctement définies")
}

func TestStartServer(t *testing.T) {
	s := &server{
		address: ":0",
	}

	ctx := logging.ContextWithLogger(context.Background(), logging.Testing())
	ctx = ContextWithServerInfo(ctx)

	handler := http.NewServeMux()

	shutdown, err := s.StartServer(ctx, handler)
	require.NoError(t, err, "StartServer ne devrait pas échouer")
	require.NotNil(t, shutdown, "La fonction de shutdown ne devrait pas être nil")

	select {
	case <-Started(ctx):
	case <-time.After(1 * time.Second):
		t.Fatal("Le serveur n'a pas démarré à temps")
	}

	require.NotEmpty(t, Address(ctx), "L'adresse du serveur devrait être définie")

	err = shutdown(ctx)
	require.NoError(t, err, "L'arrêt du serveur ne devrait pas échouer")
}

func TestURL(t *testing.T) {
	ctx := context.Background()
	ctx = ContextWithServerInfo(ctx)

	si := GetActualServerInfo(ctx)
	si.address = "localhost:8080"

	url := URL(ctx)
	require.Equal(t, "http://localhost:8080", url, "L'URL devrait être correctement formée")
}
