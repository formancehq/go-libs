package logging_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/formancehq/go-libs/v3/logging"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
)

func TestHcLogAdapterHook(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer

	hookKey := "hookKey"
	hookVal := "hookVal"

	ctx := context.WithValue(context.Background(), hookKey, hookVal)
	opts := &hclog.LoggerOptions{Level: hclog.Debug, Output: &buf}
	hl := hclog.New(opts)
	logger := logging.NewHcLogLoggerAdapter(hl, []string{hookKey})
	logger = logger.WithContext(ctx)

	logger.Error("this is the message")
	require.Regexp(t, fmt.Sprintf("%s=%s", hookKey, hookVal), buf.String())
}
