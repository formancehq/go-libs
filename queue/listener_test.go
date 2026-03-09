package queue_test

import (
	"bytes"
	"context" //nolint: gosec
	"fmt"
	"os"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/formancehq/go-libs/v4/logging"
	"github.com/formancehq/go-libs/v4/queue"
)

func TestNewListenerWorkerCount(t *testing.T) {
	logger := logging.NewDefaultLogger(os.Stderr, true, true, false)
	listener, err := queue.NewListener(logger, func(ctx context.Context, meta map[string]string, msg []byte) error { return nil }, 0)
	require.NotNil(t, err)
	assert.ErrorContains(t, err, "workerCount")
	assert.Nil(t, listener)
}

func TestNewListenerInvalidCallback(t *testing.T) {
	logger := logging.NewDefaultLogger(os.Stderr, true, true, false)
	listener, err := queue.NewListener(logger, nil, 1)
	require.NotNil(t, err)
	assert.ErrorContains(t, err, "callback")
	assert.Nil(t, listener)
}

func TestHandleMessageInjectsLoggerInContext(t *testing.T) {
	// Create a logger that writes to a buffer so we can verify it's the one in context
	var buf bytes.Buffer
	l := logrus.New()
	l.SetOutput(&buf)
	l.SetLevel(logrus.DebugLevel)
	logger := logging.NewLogrus(l)

	done := make(chan struct{})
	callback := func(ctx context.Context, meta map[string]string, msg []byte) error {
		// Log using the logger from context — if ContextWithLogger was called,
		// this writes to our buffer; if not, it writes to stderr (default fallback)
		logging.FromContext(ctx).Infof("hello from callback")
		close(done)
		return nil
	}

	listener, err := queue.NewListener(logger, callback, 1)
	require.NoError(t, err)

	ch := make(chan *message.Message, 1)
	ch <- message.NewMessage("test-uuid", []byte("test-payload"))

	ctx, cancel := context.WithCancel(context.Background())
	listener.Listen(ctx, ch)

	<-done
	cancel()
	<-listener.Done()

	// The injected logger writes to buf; the default fallback writes to stderr.
	// If our buffer contains the callback message, the logger was properly injected.
	assert.Contains(t, buf.String(), "hello from callback")
}

func TestHandleMessagePropagatesTraceContext(t *testing.T) {
	// Register W3C TraceContext propagator so Extract parses traceparent from metadata
	otel.SetTextMapPropagator(propagation.TraceContext{})

	var buf bytes.Buffer
	l := logrus.New()
	l.SetOutput(&buf)
	l.SetLevel(logrus.DebugLevel)
	logger := logging.NewLogrus(l)

	expectedTraceID := "4bf92f3577b16e0f0e3dd97b8142ec4a"
	expectedSpanID := "00f067aa0ba902b7"

	done := make(chan struct{})
	callback := func(ctx context.Context, meta map[string]string, msg []byte) error {
		// Verify Extract() populated the context with the trace/span from the message metadata
		sc := trace.SpanFromContext(ctx).SpanContext()
		logging.FromContext(ctx).Infof(
			fmt.Sprintf("trace_id=%s span_id=%s", sc.TraceID().String(), sc.SpanID().String()),
		)
		close(done)
		return nil
	}

	listener, err := queue.NewListener(logger, callback, 1)
	require.NoError(t, err)

	ch := make(chan *message.Message, 1)
	msg := message.NewMessage("test-uuid", []byte("test-payload"))
	msg.Metadata["traceparent"] = "00-" + expectedTraceID + "-" + expectedSpanID + "-01"
	ch <- msg

	ctx, cancel := context.WithCancel(context.Background())
	listener.Listen(ctx, ch)

	<-done
	cancel()
	<-listener.Done()

	output := buf.String()
	assert.Contains(t, output, expectedTraceID, "trace ID should be propagated from message metadata via Extract")
	assert.Contains(t, output, expectedSpanID, "span ID should be propagated from message metadata via Extract")
}
