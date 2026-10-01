package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.uber.org/dig"
	"go.uber.org/fx"

	errorsutils "github.com/formancehq/go-libs/v5/pkg/errors"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	otlptraces "github.com/formancehq/go-libs/v5/pkg/observe/traces"
)

const (
	DebugFlag                   = "debug"
	GracePeriodBeforeOnStopFlag = "grace-period" // Keeping the same flag value for retro compatibility
	TotalStopTimeoutFlag        = "total-stop-timeout"
	// The default allows the standard five-second routing grace period, up to
	// 20 seconds for ordered cleanup, and five seconds for the process to exit
	// before Kubernetes' default 30-second termination window expires.
	defaultTotalStopTimeout = 25 * time.Second
)

func AddFlags(flags *pflag.FlagSet) {
	flags.Bool(DebugFlag, false, "Debug mode")
	flags.Bool(logging.JsonFormattingLoggerFlag, false, "Format logs as json")
	flags.Duration(GracePeriodBeforeOnStopFlag, 0, "Grace period before triggering onStop hooks (e.g. to give time for"+
		" k8s to stop sending requests to the app before turning down the http server")
	flags.Duration(TotalStopTimeoutFlag, defaultTotalStopTimeout, "Total time allowed for all OnStop hooks to complete (see https://pkg.go.dev/go.uber.org/fx#StopTimeout)")
}

// ErrShutdownExitCode is returned by Run, carrying the exit code, when the
// application was shut down through fx.Shutdowner with a non-zero fx.ExitCode.
var ErrShutdownExitCode = errors.New("application shut down with a non-zero exit code")

type App struct {
	options []fx.Option
	output  io.Writer
	logger  logging.Logger
}

func (a *App) Run(cmd *cobra.Command) error {
	if a.logger == nil {
		otelTraces, _ := cmd.Flags().GetString(otlptraces.OtelTracesExporterFlag)

		jsonFormatting, _ := cmd.Flags().GetBool(logging.JsonFormattingLoggerFlag)
		a.logger = logging.NewDefaultLogger(
			a.output,
			IsDebug(cmd),
			jsonFormatting,
			otelTraces != "",
		)
	}
	a.logger.Infof("Starting application")

	gracePeriod, _ := cmd.Flags().GetDuration(GracePeriodBeforeOnStopFlag)
	totalStopTimeout, _ := cmd.Flags().GetDuration(TotalStopTimeoutFlag)

	app := a.newFxApp(a.logger, gracePeriod, totalStopTimeout)
	if err := app.Start(logging.ContextWithLogger(cmd.Context(), a.logger)); err != nil {
		// Run never exits, so a caller with its own Execute can still render,
		// redact or classify the error first.
		//
		// The error is returned as is rather than wrapped in a new
		// ErrorWithExitCode, as the shutdown path below does: a start error
		// already carries its own exit code (a constructor or OnStart hook
		// returned errors.NewErrorWithExitCode), and both returns keep it in
		// the chain, where errors.ExitCodeFromError finds it. A shutdown has
		// no error of its own, only fx's exit code, hence its sentinel.
		//
		// Return complete error if we are debugging
		// While polluting the output most of the time, it sometimes gives some precious information.
		// Otherwise dig.RootCause drops dig's "could not build arguments for
		// function …" wrapping and returns the error the application raised.
		if IsDebug(cmd) {
			return err
		}
		return dig.RootCause(err)
	}

	var exitCode int
	select {
	case <-cmd.Context().Done():
	case shutdownSignal := <-app.Wait():
		// <-app.Done() is a signals channel, it means we have to call the
		// app.Stop in order to gracefully shutdown the app
		exitCode = shutdownSignal.ExitCode
	}

	a.logger.Infof("Stopping app...")
	defer func() {
		a.logger.Infof("App stopped!")
	}()

	// App.Stop does not apply StopTimeout itself. Use a fresh context so
	// cancellation of the command does not prevent graceful cleanup.
	stopCtx := context.Background()
	cancel := func() {}
	if app.StopTimeout() > 0 {
		stopCtx, cancel = context.WithTimeout(stopCtx, app.StopTimeout())
	}
	defer cancel()

	if err := app.Stop(logging.ContextWithLogger(contextWithLifecycle(
		stopCtx,
		lifecycleFromContext(cmd.Context()),
	), a.logger)); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("stopping application within %s: %w", app.StopTimeout(), err)
		}
		return err
	}

	if exitCode != 0 {
		return errorsutils.NewErrorWithExitCode(ErrShutdownExitCode, exitCode)
	}

	return nil
}

func (a *App) newFxApp(logger logging.Logger, gracePeriod time.Duration, totalStopTimeout time.Duration) *fx.App {
	options := append(
		a.options,
		fx.NopLogger,
		fx.Supply(fx.Annotate(logger, fx.As(new(logging.Logger)))),
		fx.Invoke(func(lc fx.Lifecycle) {
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					markAsAppReady(ctx)

					return nil
				},
			})
		}),
		fx.StopTimeout(totalStopTimeout),
	)
	options = append([]fx.Option{
		fx.Invoke(func(lc fx.Lifecycle) {
			lc.Append(fx.Hook{
				OnStop: func(ctx context.Context) error {
					markAsAppStopped(ctx)

					return nil
				},
			})
		}),
	}, options...)
	if gracePeriod != 0 {
		options = append(options, fx.Invoke(func(lc fx.Lifecycle) {
			lc.Append(fx.Hook{
				OnStop: func(ctx context.Context) error {
					logging.FromContext(ctx).Infof("Waiting for grace period (%s)...", gracePeriod)
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(gracePeriod):
						return nil
					}
				},
			})
		}))
	}
	return fx.New(options...)
}

func New(output io.Writer, options ...fx.Option) *App {
	return &App{
		options: options,
		output:  output,
	}
}

func NewWithLogger(l logging.Logger, options ...fx.Option) *App {
	return &App{
		options: options,
		logger:  l,
	}
}
