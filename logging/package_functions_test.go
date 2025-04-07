package logging

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGlobalFunctions tests the global logging functions
func TestGlobalFunctions(t *testing.T) {
	// Test that the global functions don't panic
	// We can't easily test their output without modifying unexported variables
	
	t.Run("global functions don't panic", func(t *testing.T) {
		// These should not panic
		Debugf("debug %s", "message")
		Debug("debug", "message")
		Infof("info %s", "message")
		Info("info", "message")
		Errorf("error %s", "message")
		Error("error", "message")
		
		// WithFields should return a non-nil logger
		logger := WithFields(map[string]any{"key": "value"})
		require.NotNil(t, logger, "WithFields devrait retourner un logger")
		
		// The returned logger should not panic when used
		logger.Info("test with fields")
	})
}
