package logging

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStreamReader(t *testing.T) {
	var logBuf bytes.Buffer
	logger := NewDefaultLogger(&logBuf, true, false, false)
	
	t.Run("normal operation", func(t *testing.T) {
		testData := "line1\nline2\nline3"
		reader := strings.NewReader(testData)
		
		var capturedLines []string
		
		callback := func(l Logger, args ...any) {
			require.Len(t, args, 1, "La fonction de callback devrait recevoir un argument")
			line, ok := args[0].(string)
			require.True(t, ok, "L'argument devrait être une chaîne")
			capturedLines = append(capturedLines, line)
		}
		
		StreamReader(logger, reader, callback)
		
		require.Len(t, capturedLines, 3, "Toutes les lignes devraient être capturées")
		require.Equal(t, "line1", capturedLines[0], "La première ligne devrait correspondre")
		require.Equal(t, "line2", capturedLines[1], "La deuxième ligne devrait correspondre")
		require.Equal(t, "line3", capturedLines[2], "La troisième ligne devrait correspondre")
	})
	
	t.Run("empty reader", func(t *testing.T) {
		reader := strings.NewReader("")
		
		callCount := 0
		
		callback := func(l Logger, args ...any) {
			callCount++
		}
		
		StreamReader(logger, reader, callback)
		
		require.Equal(t, 0, callCount, "Le callback ne devrait pas être appelé pour un reader vide")
	})
	
	t.Run("reader with error", func(t *testing.T) {
		reader := &errorReader{err: io.ErrUnexpectedEOF}
		
		callback := func(l Logger, args ...any) {
		}
		
		StreamReader(logger, reader, callback)
		
		require.Contains(t, logBuf.String(), "error reading container logs buffer", "L'erreur devrait être loggée")
		require.Contains(t, logBuf.String(), "unexpected EOF", "Le message d'erreur devrait être inclus")
	})
}

type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, r.err
}
