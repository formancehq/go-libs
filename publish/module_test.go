package publish

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/formancehq/go-libs/v2/logging"
	natsServer "github.com/nats-io/nats-server/v2/server"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestModule(t *testing.T) {
	t.Parallel()

	tracerProvider := tracesdk.NewTracerProvider()
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	type moduleTestCase struct {
		name         string
		setup        func(t *testing.T) fx.Option
		topicMapping map[string]string
		topic        string
	}

	testCases := []moduleTestCase{
		{
			name: "go-channels",
			setup: func(t *testing.T) fx.Option {
				return GoChannelModule()
			},
			topic: "topic",
		},
		{
			name: "nats",
			setup: func(t *testing.T) fx.Option {
				server, err := natsServer.NewServer(&natsServer.Options{
					Host:      "0.0.0.0",
					Port:      4322,
					JetStream: true,
					StoreDir:  os.TempDir(),
				})
				require.NoError(t, err)

				server.Start()
				require.Eventually(t, server.Running, 3*time.Second, 10*time.Millisecond)

				t.Cleanup(server.Shutdown)

				return fx.Options(
					NatsModule("nats://127.0.0.1:4322", "testing", true, nats.Name("example")),
				)
			},
			topicMapping: map[string]string{},
			topic:        "topic",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var (
				publisher      message.Publisher
				router         *message.Router
				messageHandled = make(chan *message.Message, 1)
			)
			options := []fx.Option{
				Module(tc.topicMapping),
				tc.setup(t),
				fx.Populate(&publisher, &router),
				fx.Supply(fx.Annotate(logging.Testing(), fx.As(new(logging.Logger)))),
				fx.Invoke(func(r *message.Router, subscriber message.Subscriber) {
					r.AddNoPublisherHandler("testing", tc.topic, subscriber, func(msg *message.Message) error {
						messageHandled <- msg
						close(messageHandled)
						return nil
					})
				}),
			}
			if !testing.Verbose() {
				options = append(options, fx.NopLogger)
			}
			app := fxtest.New(t, options...)
			app.RequireStart()
			defer func() {
				app.RequireStop()
			}()

			<-router.Running()

			tracer := otel.Tracer("main")
			ctx, span := tracer.Start(context.TODO(), "main")
			t.Cleanup(func() {
				span.End()
			})
			require.True(t, trace.SpanFromContext(ctx).SpanContext().IsValid())
			msg := NewMessage(ctx, EventMessage{})
			require.NoError(t, publisher.Publish(tc.topic, msg))

			select {
			case msg := <-messageHandled:
				span, event, err := UnmarshalMessage(msg)
				require.NoError(t, err)
				require.NotNil(t, event)
				require.NotNil(t, ctx)
				require.True(t, span.SpanContext().IsValid())
			case <-time.After(10 * time.Second):
				t.Fatal("timeout waiting message")
			}
		})
	}
}
