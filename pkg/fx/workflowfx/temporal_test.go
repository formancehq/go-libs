package workflowfx

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/fx"
)

func TestRegisterTemporalWorkerLifecycleIsRaceSafeWhenRunReturnsDuringStop(t *testing.T) {
	lc := &lifecycleRecorder{}
	w := &shutdownErrorWorker{
		runErr:      errors.New("worker stopped"),
		runReturned: make(chan struct{}),
	}

	registerTemporalWorkerLifecycle(lc, w)

	if len(lc.hooks) != 1 {
		t.Fatalf("expected 1 lifecycle hook, got %d", len(lc.hooks))
	}

	hook := lc.hooks[0]
	if hook.OnStart == nil {
		t.Fatal("expected OnStart hook")
	}
	if hook.OnStop == nil {
		t.Fatal("expected OnStop hook")
	}

	if err := hook.OnStart(context.Background()); err != nil {
		t.Fatalf("OnStart returned error: %v", err)
	}
	if err := hook.OnStop(context.Background()); err != nil {
		t.Fatalf("OnStop returned error: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
}

type lifecycleRecorder struct {
	hooks []fx.Hook
}

func (lc *lifecycleRecorder) Append(hook fx.Hook) {
	lc.hooks = append(lc.hooks, hook)
}

type shutdownErrorWorker struct {
	runErr      error
	runReturned chan struct{}
}

func (w *shutdownErrorWorker) Run(<-chan interface{}) error {
	time.Sleep(10 * time.Millisecond)
	close(w.runReturned)
	return w.runErr
}

func (w *shutdownErrorWorker) Stop() {
	select {
	case <-w.runReturned:
	case <-time.After(time.Second):
		panic("worker Run did not return")
	}
}
