package publish

import (
	"github.com/ThreeDotsLabs/watermill"
	wNats "github.com/ThreeDotsLabs/watermill-nats/v2/pkg/nats"
	"github.com/nats-io/nats.go"
	"github.com/pkg/errors"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

func NewNatsConn(config wNats.PublisherConfig) (*nats.Conn, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	conn, err := nats.Connect(config.URL, config.NatsOptions...)
	if err != nil {
		return nil, errors.Wrap(err, "cannot connect to nats-core")
	}

	return conn, nil
}

func NewNatsPublisherWithConn(conn *nats.Conn, logger watermill.LoggerAdapter, config wNats.PublisherConfig) (*wNats.Publisher, error) {
	return wNats.NewPublisherWithNatsConn(conn, config.GetPublisherPublishConfig(), logger)
}

func NewNatsSubscriberWithConn(conn *nats.Conn, logger watermill.LoggerAdapter, config wNats.SubscriberConfig) (*wNats.Subscriber, error) {
	return wNats.NewSubscriberWithNatsConn(conn, config.GetSubscriberSubscriptionConfig(), logger)
}

// NATSCallbacks provides hooks for NATS connection events.
type NATSCallbacks interface {
	ClosedCB(nc *nats.Conn)
	DisconnectedCB(nc *nats.Conn)
	DiscoveredServersCB(nc *nats.Conn)
	ReconnectedCB(nc *nats.Conn)
	DisconnectedErrCB(nc *nats.Conn, err error)
	ConnectedCB(nc *nats.Conn)
	AsyncErrorCB(nc *nats.Conn, sub *nats.Subscription, err error)
}

func AppendNatsCallBacks(natsOptions []nats.Option, c NATSCallbacks) []nats.Option {
	return append(natsOptions,
		nats.ConnectHandler(c.ConnectedCB),
		nats.DisconnectErrHandler(c.DisconnectedErrCB),
		nats.DiscoveredServersHandler(c.DiscoveredServersCB),
		nats.ErrorHandler(c.AsyncErrorCB),
		nats.ReconnectHandler(c.ReconnectedCB),
		nats.DisconnectHandler(c.DisconnectedCB),
		nats.ClosedHandler(c.ClosedCB),
	)
}

// ShutdownFunc is called when a fatal NATS event occurs (e.g. connection closed).
type ShutdownFunc func() error

type NatsDefaultCallbacks struct {
	logger   logging.Logger
	shutdown ShutdownFunc
}

func NewNatsDefaultCallbacks(logger logging.Logger, shutdown ShutdownFunc) NATSCallbacks {
	return &NatsDefaultCallbacks{
		logger:   logger,
		shutdown: shutdown,
	}
}

func (c *NatsDefaultCallbacks) ClosedCB(nc *nats.Conn) {
	c.logger.Infof("nats connection closed: %s", nc.Opts.Name)
	_ = c.shutdown()
}

func (c *NatsDefaultCallbacks) DisconnectedCB(nc *nats.Conn) {
	c.logger.Infof("nats connection disconnected: %s", nc.Opts.Name)
}

func (c *NatsDefaultCallbacks) DiscoveredServersCB(nc *nats.Conn) {
	c.logger.Infof("nats server discovered: %s", nc.Opts.Name)
}

func (c *NatsDefaultCallbacks) ReconnectedCB(nc *nats.Conn) {
	c.logger.Infof("nats connection reconnected: %s", nc.Opts.Name)
}

func (c *NatsDefaultCallbacks) DisconnectedErrCB(nc *nats.Conn, err error) {
	c.logger.Errorf("nats connection disconnected error for %s: %v", nc.Opts.Name, err)
}

func (c *NatsDefaultCallbacks) ConnectedCB(nc *nats.Conn) {
	c.logger.Infof("nats connection done: %s", nc.Opts.Name)
}

func (c *NatsDefaultCallbacks) AsyncErrorCB(nc *nats.Conn, sub *nats.Subscription, err error) {
	c.logger.Errorf("nats async error for %s with subject %s: %v", nc.Opts.Name, sub.Subject, err)
}
