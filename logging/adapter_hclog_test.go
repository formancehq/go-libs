package logging_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/formancehq/go-libs/v2/logging"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/assert"
)

func TestHcLogAdapterHook(t *testing.T) {
	var buf bytes.Buffer

	hookKey := "hookKey"
	hookVal := "hookVal"

	ctx := context.WithValue(context.Background(), hookKey, hookVal)
	opts := &hclog.LoggerOptions{Level: hclog.Debug, Output: &buf}
	logger := logging.NewHcLogLoggerAdapter(opts, []string{hookKey})
	logger = logger.WithContext(ctx)

	logger.Error("this is the message")
	assert.Regexp(t, fmt.Sprintf("%s=%s", hookKey, hookVal), buf.String())
}
