package service

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.uber.org/dig"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v3/errorsutils"
	"github.com/formancehq/go-libs/v3/logging"
	"github.com/formancehq/go-libs/v3/otlp/otlptraces"
)

const (
	DebugFlag                   = "debug"
	GracePeriodBeforeOnStopFlag = "grace-period" // Keeping the same flag value for retro compatibility
	TotalStopTimeoutFlag        = "total-stop-timeout"
)

func AddFlags(flags *pflag.FlagSet) {
	flags.Bool(DebugFlag, false, "Debug mode")
	flags.Bool(logging.JsonFormattingLoggerFlag, false, "Format logs as json")
	flags.Duration(GracePeriodBeforeOnStopFlag, 0, "Grace period before triggering onStop hooks (e.g. to give time for"+
		" k8s to stop sending requests to the app before turning down the http server")
	flags.Duration(TotalStopTimeoutFlag, fx.DefaultTimeout, "Total time allowed for all OnStop hooks to complete (see https://pkg.go.dev/go.uber.org/fx#StopTimeout)")
}

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
		switch {
		case errorsutils.IsErrorWithExitCode(err):
			a.logger.Errorf("Error: %v", err)
			// We want to have a specific exit code for the error
			os.Exit(err.(*errorsutils.ErrorWithExitCode).ExitCode)
		default:
			// Return complete error if we are debugging
			// While polluting the output most of the time, it sometimes gives some precious information
			if IsDebug(cmd) {
				return err
			}
			return dig.RootCause(err)
		}
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

	if err := app.Stop(logging.ContextWithLogger(contextWithLifecycle(
		context.Background(), // Don't reuse original context as it can have been cancelled, and we really need to properly stop the app
		lifecycleFromContext(cmd.Context()),
	), a.logger)); err != nil {
		return err
	}

	if exitCode != 0 {
		os.Exit(exitCode)
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
