package bunexplain

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestNewExplainHook(t *testing.T) {
	hook := NewExplainHook()
	require.NotNil(t, hook, "Le hook ne devrait pas être nil")
	require.NotNil(t, hook.listener, "Le listener par défaut ne devrait pas être nil")
	require.False(t, hook.json, "Le format JSON devrait être désactivé par défaut")
	
	hook = NewExplainHook(WithJSONFormat())
	require.True(t, hook.json, "Le format JSON devrait être activé")
	
	var output string
	hook = NewExplainHook(WithListener(func(data string) {
		output = data
	}))
	require.NotNil(t, hook.listener, "Le listener personnalisé ne devrait pas être nil")
	
	hook.listener("test")
	require.Equal(t, "test", output, "Le listener devrait stocker la sortie")
}

func TestBeforeQuery_NonSelectQuery(t *testing.T) {
	hook := NewExplainHook()
	ctx := context.Background()
	
	event := &bun.QueryEvent{
		Query: "INSERT INTO test (id) VALUES (1)",
	}
	
	resultCtx := hook.BeforeQuery(ctx, event)
	
	require.Equal(t, ctx, resultCtx, "Le contexte devrait être inchangé pour une requête non-SELECT")
}

func TestBeforeQuery_SelectQuery(t *testing.T) {
	t.Skip("Ce test nécessite une vraie base de données")
	
	hook := NewExplainHook(WithListener(func(data string) {
	}))
	
	ctx := context.Background()
	
	event := &bun.QueryEvent{
		Query: "SELECT * FROM test",
		DB:    nil,
	}
	
	resultCtx := hook.BeforeQuery(ctx, event)
	require.Equal(t, ctx, resultCtx, "Le contexte devrait être retourné")
}

func TestBeforeQuery_WithQuery(t *testing.T) {
	t.Skip("Ce test nécessite une vraie base de données")
	
	hook := NewExplainHook(WithListener(func(data string) {
	}))
	
	ctx := context.Background()
	
	event := &bun.QueryEvent{
		Query: "WITH t AS (SELECT 1) SELECT * FROM t",
		DB:    nil,
	}
	
	resultCtx := hook.BeforeQuery(ctx, event)
	require.Equal(t, ctx, resultCtx, "Le contexte devrait être retourné")
}

func TestBeforeQuery_WithJSONFormat(t *testing.T) {
	t.Skip("Ce test nécessite une vraie base de données")
	
	hook := NewExplainHook(
		WithListener(func(data string) {
		}),
		WithJSONFormat(),
	)
	
	ctx := context.Background()
	
	event := &bun.QueryEvent{
		Query: "SELECT * FROM test",
		DB:    nil,
	}
	
	resultCtx := hook.BeforeQuery(ctx, event)
	require.Equal(t, ctx, resultCtx, "Le contexte devrait être retourné")
}

func TestAfterQuery(t *testing.T) {
	hook := NewExplainHook()
	ctx := context.Background()
	event := &bun.QueryEvent{}
	
	hook.AfterQuery(ctx, event)
}

func TestWithListener(t *testing.T) {
	var called bool
	listener := func(data string) {
		called = true
	}
	
	opt := WithListener(listener)
	require.NotNil(t, opt, "L'option ne devrait pas être nil")
	
	hook := &explainHook{}
	opt(hook)
	
	require.NotNil(t, hook.listener, "Le listener devrait être défini")
	
	hook.listener("test")
	require.True(t, called, "Le listener devrait être appelé")
}

func TestWithJSONFormat(t *testing.T) {
	opt := WithJSONFormat()
	require.NotNil(t, opt, "L'option ne devrait pas être nil")
	
	hook := &explainHook{}
	opt(hook)
	
	require.True(t, hook.json, "Le format JSON devrait être activé")
}
