package logging

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFromContext(t *testing.T) {
	logger := FromContext(context.Background())
	require.NotNil(t, logger, "Le logger par défaut ne devrait pas être nil")

	customLogger := Testing()
	ctx := ContextWithLogger(context.Background(), customLogger)
	loggerFromCtx := FromContext(ctx)
	require.Same(t, customLogger, loggerFromCtx, "Le logger récupéré devrait être le même que celui ajouté")
}

func TestContextWithLogger(t *testing.T) {
	logger := Testing()
	
	ctx := context.Background()
	ctxWithLogger := ContextWithLogger(ctx, logger)
	
	loggerFromCtx := FromContext(ctxWithLogger)
	require.Same(t, logger, loggerFromCtx, "Le logger récupéré devrait être le même que celui ajouté")
}

func TestContextWithFields(t *testing.T) {
	logger := Testing()
	ctx := ContextWithLogger(context.Background(), logger)
	
	fields := map[string]any{"key": "value"}
	ctxWithFields := ContextWithFields(ctx, fields)
	
	loggerWithFields := FromContext(ctxWithFields)
	require.NotSame(t, logger, loggerWithFields, "Un nouveau logger devrait être créé")
}

func TestContextWithField(t *testing.T) {
	logger := Testing()
	ctx := ContextWithLogger(context.Background(), logger)
	
	ctxWithField := ContextWithField(ctx, "key", "value")
	
	loggerWithField := FromContext(ctxWithField)
	require.NotSame(t, logger, loggerWithField, "Un nouveau logger devrait être créé")
}

func TestTestingContext(t *testing.T) {
	ctx := TestingContext()
	
	logger := FromContext(ctx)
	require.NotNil(t, logger, "Le contexte de test devrait avoir un logger")
}
