package logging

import (
	"fmt"
	"testing"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestNewZapLoggerAdapter(t *testing.T) {
	logger := zaptest.NewLogger(t)
	adapter := NewZapLoggerAdapter(logger)

	require.NotNil(t, adapter, "L'adaptateur ne devrait pas être nil")
	require.IsType(t, &ZapLoggerAdapter{}, adapter, "L'adaptateur devrait être de type ZapLoggerAdapter")
}

func TestZapLoggerAdapter_Error(t *testing.T) {
	logger := zaptest.NewLogger(t)
	adapter := NewZapLoggerAdapter(logger).(*ZapLoggerAdapter)

	testErr := fmt.Errorf("test error")
	fields := watermill.LogFields{"key": "value"}

	adapter.Error("test error message", testErr, fields)
}

func TestZapLoggerAdapter_Info(t *testing.T) {
	logger := zaptest.NewLogger(t)
	adapter := NewZapLoggerAdapter(logger).(*ZapLoggerAdapter)

	fields := watermill.LogFields{"key": "value"}

	adapter.Info("test info message", fields)
}

func TestZapLoggerAdapter_Debug(t *testing.T) {
	logger := zaptest.NewLogger(t)
	adapter := NewZapLoggerAdapter(logger).(*ZapLoggerAdapter)

	fields := watermill.LogFields{"key": "value"}

	adapter.Debug("test debug message", fields)
}

func TestZapLoggerAdapter_Trace(t *testing.T) {
	logger := zaptest.NewLogger(t)
	adapter := NewZapLoggerAdapter(logger).(*ZapLoggerAdapter)

	fields := watermill.LogFields{"key": "value"}

	adapter.Trace("test trace message", fields)
}

func TestZapLoggerAdapter_With(t *testing.T) {
	logger := zaptest.NewLogger(t)
	adapter := NewZapLoggerAdapter(logger).(*ZapLoggerAdapter)

	fields := watermill.LogFields{"key": "value"}

	newAdapter := adapter.With(fields)

	require.NotNil(t, newAdapter, "Le nouvel adaptateur ne devrait pas être nil")
	require.IsType(t, &ZapLoggerAdapter{}, newAdapter, "Le nouvel adaptateur devrait être de type ZapLoggerAdapter")

	zapAdapter := newAdapter.(*ZapLoggerAdapter)
	require.Equal(t, fields, zapAdapter.fields, "Les champs devraient être correctement définis")
}
