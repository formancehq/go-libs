package queue_test

import (
	"context" //nolint: gosec
	"os"
	"testing"

	"github.com/formancehq/go-libs/v3/logging"
	"github.com/formancehq/go-libs/v3/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
