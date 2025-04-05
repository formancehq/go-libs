package bundebug

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/formancehq/go-libs/v2/logging"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestNewQueryHook(t *testing.T) {
	hook := NewQueryHook()
	require.NotNil(t, hook, "Le hook ne devrait pas être nil")
	require.False(t, hook.Debug, "Le mode debug devrait être désactivé par défaut")
}

func TestBeforeQuery(t *testing.T) {
	hook := NewQueryHook()
	ctx := context.Background()
	event := &bun.QueryEvent{}
	
	resultCtx := hook.BeforeQuery(ctx, event)
	require.Equal(t, ctx, resultCtx, "Le contexte retourné devrait être le même")
}

func TestAfterQuery_NoDebug(t *testing.T) {
	hook := NewQueryHook()
	ctx := context.Background()
	event := &bun.QueryEvent{
		Query:     "SELECT * FROM test",
		StartTime: time.Now().Add(-time.Second),
	}
	
	hook.AfterQuery(ctx, event)
}

func TestAfterQuery_WithDebug(t *testing.T) {
	hook := &QueryHook{Debug: true}
	
	var buf strings.Builder
	logger := logging.NewDefaultLogger(&buf, true, false, false)
	ctx := logging.ContextWithLogger(context.Background(), logger)
	
	event := &bun.QueryEvent{
		Query:     "SELECT * FROM test",
		StartTime: time.Now().Add(-time.Second),
	}
	
	hook.AfterQuery(ctx, event)
	
	output := buf.String()
	require.Contains(t, output, "SELECT * FROM test", "Le log devrait contenir la requête")
	require.Contains(t, output, "bun", "Le log devrait contenir le composant")
}

func TestAfterQuery_WithError(t *testing.T) {
	hook := &QueryHook{Debug: true}
	
	var buf strings.Builder
	logger := logging.NewDefaultLogger(&buf, true, false, false)
	ctx := logging.ContextWithLogger(context.Background(), logger)
	
	event := &bun.QueryEvent{
		Query:     "SELECT * FROM test",
		StartTime: time.Now().Add(-time.Second),
		Err:       errors.New("test error"),
	}
	
	hook.AfterQuery(ctx, event)
	
	output := buf.String()
	require.Contains(t, output, "test error", "Le log devrait contenir l'erreur")
}

func TestAfterQuery_MultilineQuery(t *testing.T) {
	hook := &QueryHook{Debug: true}
	
	var buf strings.Builder
	logger := logging.NewDefaultLogger(&buf, true, false, false)
	ctx := logging.ContextWithLogger(context.Background(), logger)
	
	event := &bun.QueryEvent{
		Query:     "SELECT *\nFROM test\nWHERE id = 1",
		StartTime: time.Now().Add(-time.Second),
	}
	
	hook.AfterQuery(ctx, event)
	
	output := buf.String()
	require.Contains(t, output, "SELECT *...", "Le log devrait contenir la première ligne de la requête avec '...'")
}

func TestWithDebug(t *testing.T) {
	ctx := WithDebug(nil)
	require.NotNil(t, ctx, "Le contexte ne devrait pas être nil")
	require.True(t, isDebug(ctx), "Le mode debug devrait être activé")
	
	ctx2 := context.Background()
	ctx2 = WithDebug(ctx2)
	require.True(t, isDebug(ctx2), "Le mode debug devrait être activé")
}

func TestIsDebug(t *testing.T) {
	require.False(t, isDebug(nil), "Le mode debug devrait être désactivé pour un contexte nil")
	
	ctx := context.Background()
	require.False(t, isDebug(ctx), "Le mode debug devrait être désactivé pour un contexte sans valeur de debug")
	
	ctx = WithDebug(ctx)
	require.True(t, isDebug(ctx), "Le mode debug devrait être activé pour un contexte avec debug activé")
}
