package publish

import (
	"testing"

	"github.com/formancehq/go-libs/v2/logging"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

func TestNewNatsDefaultCallbacks(t *testing.T) {
	logger := logging.Testing()
	shutdowner := &mockShutdowner{}

	callbacks := NewNatsDefaultCallbacks(logger, shutdowner)
	require.NotNil(t, callbacks, "Les callbacks ne devraient pas être nil")

	nc := &nats.Conn{}

	callbacks.ClosedCB(nc)
	callbacks.DisconnectedCB(nc)
	callbacks.DiscoveredServersCB(nc)
	callbacks.ReconnectedCB(nc)
	callbacks.DisconnectedErrCB(nc, nats.ErrConnectionClosed)
	callbacks.ConnectedCB(nc)
	callbacks.AsyncErrorCB(nc, &nats.Subscription{}, nats.ErrBadSubscription)

	require.True(t, shutdowner.shutdownCalled, "Le shutdowner devrait être appelé lors d'une erreur de connexion")
}

func TestAppendNatsCallBacks(t *testing.T) {
	baseOptions := []nats.Option{
		nats.Name("test"),
	}

	callbacks := &mockCallbacks{}

	options := AppendNatsCallBacks(baseOptions, callbacks)

	require.Len(t, options, 8, "Il devrait y avoir 8 options (1 de base + 7 callbacks)")
}

type mockShutdowner struct {
	shutdownCalled bool
}

func (m *mockShutdowner) Shutdown(options ...fx.ShutdownOption) error {
	m.shutdownCalled = true
	return nil
}

type mockCallbacks struct {
	NATSCallbacks
}

func (c *mockCallbacks) ClosedCB(nc *nats.Conn)                                        {}
func (c *mockCallbacks) DisconnectedCB(nc *nats.Conn)                                  {}
func (c *mockCallbacks) DiscoveredServersCB(nc *nats.Conn)                             {}
func (c *mockCallbacks) ReconnectedCB(nc *nats.Conn)                                   {}
func (c *mockCallbacks) DisconnectedErrCB(nc *nats.Conn, err error)                    {}
func (c *mockCallbacks) ConnectedCB(nc *nats.Conn)                                     {}
func (c *mockCallbacks) AsyncErrorCB(nc *nats.Conn, sub *nats.Subscription, err error) {}
