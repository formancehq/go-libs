package logging

import (
	"bytes"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestLogrusLogger_Writer(t *testing.T) {
	var buf bytes.Buffer
	logrusLogger := logrus.New()
	logrusLogger.SetOutput(&buf)
	
	adapter := NewLogrus(logrusLogger)
	
	writer := adapter.Writer()
	require.NotNil(t, writer, "Le writer ne devrait pas être nil")
	
	message := []byte("test writer message\n")
	n, err := writer.Write(message)
	require.NoError(t, err, "L'écriture ne devrait pas échouer")
	require.Equal(t, len(message), n, "Le nombre d'octets écrits devrait correspondre")
	
}

func TestSetHooks(t *testing.T) {
	t.Run("without otel traces", func(t *testing.T) {
		logger := logrus.New()
		initialHooksCount := len(logger.Hooks)
		
		SetHooks(logger, false)
		
		require.Equal(t, initialHooksCount, len(logger.Hooks), "Aucun hook ne devrait être ajouté")
	})
	
	t.Run("with otel traces", func(t *testing.T) {
		logger := logrus.New()
		initialHooksCount := len(logger.Hooks)
		
		SetHooks(logger, true)
		
		require.Greater(t, len(logger.Hooks), initialHooksCount, "Des hooks devraient être ajoutés")
		
		levels := []logrus.Level{
			logrus.PanicLevel,
			logrus.FatalLevel,
			logrus.ErrorLevel,
			logrus.WarnLevel,
		}
		
		for _, level := range levels {
			require.NotEmpty(t, logger.Hooks[level], "Un hook devrait être ajouté pour le niveau %s", level)
		}
	})
}
