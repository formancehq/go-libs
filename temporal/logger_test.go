package temporal

import (
	"testing"

	"github.com/formancehq/go-libs/v2/logging"
	"github.com/stretchr/testify/require"
)

func TestKeyvalsToMap(t *testing.T) {
	result := keyvalsToMap("key1", "value1", "key2", 42)
	require.Equal(t, map[string]any{
		"key1": "value1",
		"key2": 42,
	}, result, "La conversion en map devrait fonctionner correctement")

	result = keyvalsToMap()
	require.Empty(t, result, "Une map vide devrait être retournée pour aucun argument")
}

func TestLogger(t *testing.T) {
	testLogger := logging.Testing()
	logger := newLogger(testLogger)
	require.NotNil(t, logger, "Le logger ne devrait pas être nil")

	t.Run("Debug", func(t *testing.T) {
		logger.Debug("debug message", "key", "value")
	})

	t.Run("Info", func(t *testing.T) {
		logger.Info("info message", "key", "value")
	})

	t.Run("Warn", func(t *testing.T) {
		logger.Warn("warn message", "key", "value")
	})

	t.Run("Error", func(t *testing.T) {
		logger.Error("error message", "key", "value")
	})
}
