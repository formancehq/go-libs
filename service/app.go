package service

import (
	"context"
	"io"
	"os"
	"time"

	"go.uber.org/dig"

	"github.com/spf13/pflag"

	"github.com/formancehq/go-libs/v2/otlp/otlptraces"

	"github.com/formancehq/go-libs/v2/errorsutils"
	"github.com/formancehq/go-libs/v2/logging"
	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

const (
	DebugFlag       = "debug"
	GracePeriodFlag = "grace-period"
)

func AddFlags(flags *pflag.FlagSet) {
	flags.Bool(DebugFlag, false, "Debug mode")
	flags.Bool(logging.JsonFormattingLoggerFlag, false, "Format logs as json")
	flags.Duration(GracePeriodFlag, 0, "Grace period for shutdown")
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

	gracePeriod, _ := cmd.Flags().GetDuration(GracePeriodFlag)

	app := a.newFxApp(a.logger, gracePeriod)
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

func (a *App) newFxApp(logger logging.Logger, gracePeriod time.Duration) *fx.App {
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
