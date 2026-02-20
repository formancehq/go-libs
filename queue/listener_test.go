package queue_test

import (
	"context" //nolint: gosec
	"os"
	"testing"

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
