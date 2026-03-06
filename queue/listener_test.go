package queue_test

import (
	"bytes"
	"context" //nolint: gosec
	"os"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
