package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	errorsutils "github.com/formancehq/go-libs/v5/pkg/errors"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/service"
)

func TestRunBoundsStop(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	service.AddFlags(cmd.Flags())
	require.NoError(t, cmd.Flags().Set(service.TotalStopTimeoutFlag, "100ms"))
	release := make(chan struct{})
	defer close(release)
	stopped := make(chan context.Context, 1)
	app := service.NewWithLogger(logging.Testing(), fx.Invoke(func(lc fx.Lifecycle, shutdowner fx.Shutdowner) {
		lc.Append(fx.Hook{
			OnStart: func(context.Context) error { return shutdowner.Shutdown() },
			OnStop: func(ctx context.Context) error {
				cancel()
				stopped <- ctx
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-release:
					return nil
				}
			},
		})
	}))
	result := make(chan error, 1)
	go func() { result <- app.Run(cmd) }()
	select {
	case stopCtx := <-stopped:
		_, bounded := stopCtx.Deadline()
		require.True(t, bounded, "shutdown must receive the configured deadline")
	case <-time.After(5 * time.Second):
		t.Fatal("stop hook was not reached")
	}
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.ErrorContains(t, err, "stopping application within 100ms")
	case <-time.After(5 * time.Second):
		t.Fatal("Run exceeded its shutdown allowance")
	}
}

func TestTotalStopTimeoutDefaultsToTwentyFiveSeconds(t *testing.T) {
	t.Parallel()
	cmd := &cobra.Command{}
	service.AddFlags(cmd.Flags())

	totalStopTimeout, err := cmd.Flags().GetDuration(service.TotalStopTimeoutFlag)
	require.NoError(t, err)
	require.Equal(t, 25*time.Second, totalStopTimeout)
}

func TestRunCompletesCooperativeStop(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(service.ContextWithLifecycle(context.Background()))
	defer cancel()
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	service.AddFlags(cmd.Flags())
	require.NoError(t, cmd.Flags().Set(service.TotalStopTimeoutFlag, "5s"))
	stopContext := make(chan context.Context, 1)
	var logs bytes.Buffer
	logger := logging.NewDefaultLogger(&logs, false, false, false)
	app := service.NewWithLogger(logger, fx.Invoke(func(lc fx.Lifecycle, shutdowner fx.Shutdowner) {
		lc.Append(fx.Hook{
			OnStart: func(context.Context) error { return shutdowner.Shutdown() },
			OnStop: func(stopCtx context.Context) error {
				cancel()
				logging.FromContext(stopCtx).Info("cooperative stop hook")
				stopContext <- stopCtx
				return stopCtx.Err()
			},
		})
	}))
	require.NoError(t, app.Run(cmd))
	select {
	case stopCtx := <-stopContext:
		require.Contains(t, logs.String(), "cooperative stop hook")
		require.Equal(t, service.Stopped(ctx), service.Stopped(stopCtx))
		require.ErrorIs(t, stopCtx.Err(), context.Canceled, "stop context must be released after Run")
	default:
		t.Fatal("stop hook did not run")
	}
	select {
	case <-service.Stopped(ctx):
	default:
		t.Fatal("lifecycle did not report stopped")
	}
}

func TestRunWithZeroTimeoutDoesNotBoundStop(t *testing.T) {
	t.Parallel()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	service.AddFlags(cmd.Flags())
	require.NoError(t, cmd.Flags().Set(service.TotalStopTimeoutFlag, "0"))
	stopContext := make(chan context.Context, 1)
	app := service.NewWithLogger(logging.Testing(), fx.Invoke(func(lc fx.Lifecycle, shutdowner fx.Shutdowner) {
		lc.Append(fx.Hook{
			OnStart: func(context.Context) error { return shutdowner.Shutdown() },
			OnStop: func(ctx context.Context) error {
				stopContext <- ctx
				return nil
			},
		})
	}))

	require.NoError(t, app.Run(cmd))
	select {
	case ctx := <-stopContext:
		_, bounded := ctx.Deadline()
		require.False(t, bounded)
	default:
		t.Fatal("stop hook did not run")
	}
}

func TestRunPreservesStopHookError(t *testing.T) {
	t.Parallel()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	service.AddFlags(cmd.Flags())
	stopErr := errors.New("connection refused")
	app := service.NewWithLogger(logging.Testing(), fx.Invoke(func(lc fx.Lifecycle, shutdowner fx.Shutdowner) {
		lc.Append(fx.Hook{
			OnStart: func(context.Context) error { return shutdowner.Shutdown() },
			OnStop:  func(context.Context) error { return stopErr },
		})
	}))

	err := app.Run(cmd)
	require.ErrorIs(t, err, stopErr)
	require.Equal(t, stopErr.Error(), err.Error())
}

func TestRunGracePeriodConsumesStopBudget(t *testing.T) {
	t.Parallel()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	service.AddFlags(cmd.Flags())
	require.NoError(t, cmd.Flags().Set(service.TotalStopTimeoutFlag, "100ms"))
	require.NoError(t, cmd.Flags().Set(service.GracePeriodBeforeOnStopFlag, "1h"))
	app := service.NewWithLogger(logging.Testing(), fx.Invoke(func(lc fx.Lifecycle, shutdowner fx.Shutdowner) {
		lc.Append(fx.Hook{OnStart: func(context.Context) error { return shutdowner.Shutdown() }})
	}))
	result := make(chan error, 1)
	go func() { result <- app.Run(cmd) }()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(5 * time.Second):
		t.Fatal("grace period exceeded the total shutdown allowance")
	}
}

// TestRunReturnsExitCodedStartError: Run used to os.Exit on an exit-coded start
// error, so a caller could not render or redact it. A regression kills this test
// binary with status 78 instead of failing an assertion.
func TestRunReturnsExitCodedStartError(t *testing.T) {
	t.Parallel()
	for _, debug := range []bool{false, true} {
		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		service.AddFlags(cmd.Flags())
		if debug {
			require.NoError(t, cmd.Flags().Set(service.DebugFlag, "true"))
		}
		startErr := errorsutils.NewErrorWithExitCode(errors.New("invalid configuration"), 78)
		app := service.NewWithLogger(logging.Testing(), fx.Invoke(func(lc fx.Lifecycle) {
			lc.Append(fx.Hook{OnStart: func(context.Context) error { return startErr }})
		}))

		err := app.Run(cmd)
		code, ok := errorsutils.ExitCodeFromError(err)
		require.True(t, ok, "debug=%v: exit code lost from %v", debug, err)
		require.Equal(t, 78, code)
		require.ErrorContains(t, err, "invalid configuration")
	}
}

func TestRunReturnsShutdownExitCode(t *testing.T) {
	t.Parallel()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	service.AddFlags(cmd.Flags())
	app := service.NewWithLogger(logging.Testing(), fx.Invoke(func(lc fx.Lifecycle, shutdowner fx.Shutdowner) {
		lc.Append(fx.Hook{OnStart: func(context.Context) error { return shutdowner.Shutdown(fx.ExitCode(3)) }})
	}))

	err := app.Run(cmd)
	require.ErrorIs(t, err, service.ErrShutdownExitCode)
	code, ok := errorsutils.ExitCodeFromError(err)
	require.True(t, ok)
	require.Equal(t, 3, code)
}
