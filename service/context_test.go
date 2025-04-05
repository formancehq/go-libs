package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContextWithDebug(t *testing.T) {
	ctx := context.Background()
	
	val := ctx.Value(debugKey)
	require.Nil(t, val, "Le contexte ne devrait pas avoir le flag debug par défaut")
	
	debugCtx := ContextWithDebug(ctx)
	
	val = debugCtx.Value(debugKey)
	require.NotNil(t, val, "Le contexte devrait avoir le flag debug")
	require.True(t, val.(bool), "Le flag debug devrait être true")
}

func TestContextWithLifecycleFunction(t *testing.T) {
	ctx := context.Background()
	
	lc := lifecycleFromContext(ctx)
	require.Nil(t, lc, "Le contexte ne devrait pas avoir de lifecycle par défaut")
	
	lifecycleCtx := ContextWithLifecycle(ctx)
	
	lc = lifecycleFromContext(lifecycleCtx)
	require.NotNil(t, lc, "Le contexte devrait avoir un lifecycle")
	require.NotNil(t, lc.ready, "Le canal ready devrait être initialisé")
	require.NotNil(t, lc.stopped, "Le canal stopped devrait être initialisé")
}

func TestReady(t *testing.T) {
	ctx := context.Background()
	
	readyChan := Ready(ctx)
	select {
	case <-readyChan:
	default:
		t.Error("Le canal devrait être fermé quand il n'y a pas de lifecycle")
	}
	
	lc := newLifecycle()
	ctx = contextWithLifecycle(ctx, lc)
	
	readyChan = Ready(ctx)
	select {
	case <-readyChan:
		t.Error("Le canal ne devrait pas être fermé")
	default:
	}
	
	markAsAppReady(ctx)
	
	select {
	case <-readyChan:
	default:
		t.Error("Le canal devrait être fermé après markAsAppReady")
	}
}

func TestStopped(t *testing.T) {
	ctx := context.Background()
	
	stoppedChan := Stopped(ctx)
	select {
	case <-stoppedChan:
	default:
		t.Error("Le canal devrait être fermé quand il n'y a pas de lifecycle")
	}
	
	lc := newLifecycle()
	ctx = contextWithLifecycle(ctx, lc)
	
	stoppedChan = Stopped(ctx)
	select {
	case <-stoppedChan:
		t.Error("Le canal ne devrait pas être fermé")
	default:
	}
	
	markAsAppStopped(ctx)
	
	select {
	case <-stoppedChan:
	default:
		t.Error("Le canal devrait être fermé après markAsAppStopped")
	}
}

func TestClosedChan(t *testing.T) {
	select {
	case <-closedChan:
	default:
		t.Error("Le canal closedChan devrait être fermé")
	}
}
