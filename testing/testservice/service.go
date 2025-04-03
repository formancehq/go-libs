package testservice

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/formancehq/go-libs/v2/logging"
	"github.com/formancehq/go-libs/v2/otlp"
	"github.com/formancehq/go-libs/v2/otlp/otlpmetrics"
	"github.com/formancehq/go-libs/v2/service"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type OTLPConfig struct {
	BaseConfig otlp.Config
	Metrics    *otlpmetrics.ModuleConfig
}

type CommonConfiguration struct {
	Output     io.Writer
	Debug      bool
	OTLPConfig *OTLPConfig
}

func (cfg CommonConfiguration) getArgs() []string {
	args := []string{}
	if cfg.OTLPConfig != nil {
		if cfg.OTLPConfig.Metrics != nil {
			args = append(
				args,
				"--"+otlpmetrics.OtelMetricsExporterFlag, cfg.OTLPConfig.Metrics.Exporter,
			)
			if cfg.OTLPConfig.Metrics.KeepInMemory {
				args = append(
					args,
					"--"+otlpmetrics.OtelMetricsKeepInMemoryFlag,
				)
			}
			if cfg.OTLPConfig.Metrics.OTLPConfig != nil {
				args = append(
					args,
					"--"+otlpmetrics.OtelMetricsExporterOTLPEndpointFlag, cfg.OTLPConfig.Metrics.OTLPConfig.Endpoint,
					"--"+otlpmetrics.OtelMetricsExporterOTLPModeFlag, cfg.OTLPConfig.Metrics.OTLPConfig.Mode,
				)
				if cfg.OTLPConfig.Metrics.OTLPConfig.Insecure {
					args = append(args, "--"+otlpmetrics.OtelMetricsExporterOTLPInsecureFlag)
				}
			}
			if cfg.OTLPConfig.Metrics.RuntimeMetrics {
				args = append(args, "--"+otlpmetrics.OtelMetricsRuntimeFlag)
			}
			if cfg.OTLPConfig.Metrics.MinimumReadMemStatsInterval != 0 {
				args = append(
					args,
					"--"+otlpmetrics.OtelMetricsRuntimeMinimumReadMemStatsIntervalFlag,
					cfg.OTLPConfig.Metrics.MinimumReadMemStatsInterval.String(),
				)
			}
			if cfg.OTLPConfig.Metrics.PushInterval != 0 {
				args = append(
					args,
					"--"+otlpmetrics.OtelMetricsExporterPushIntervalFlag,
					cfg.OTLPConfig.Metrics.PushInterval.String(),
				)
			}
			if len(cfg.OTLPConfig.Metrics.ResourceAttributes) > 0 {
				args = append(
					args,
					"--"+otlp.OtelResourceAttributesFlag,
					strings.Join(cfg.OTLPConfig.Metrics.ResourceAttributes, ","),
				)
			}
		}
		if cfg.OTLPConfig.BaseConfig.ServiceName != "" {
			args = append(args, "--"+otlp.OtelServiceNameFlag, cfg.OTLPConfig.BaseConfig.ServiceName)
		}
	}
	if cfg.Debug {
		args = append(args, "--"+service.DebugFlag)
	}

	return args
}

type SpecializedConfiguration interface {
	GetArgs(serverID string) []string
}

type Configuration[Cfg SpecializedConfiguration] struct {
	CommonConfiguration
	Configuration Cfg
}

func (cfg Configuration[Cfg]) getArgs(serverID string) []string {
	return append(cfg.Configuration.GetArgs(serverID), cfg.CommonConfiguration.getArgs()...)
}

type Service[cfg SpecializedConfiguration] struct {
	BaseConfiguration
	commandFactory func() *cobra.Command
	configuration  Configuration[cfg]
	cancel         func()
	ctx            context.Context
	errorChan      chan error
	id             string
}

func (s *Service[Cfg]) GetID() string {
	return s.id
}

func (s *Service[Cfg]) Start(ctx context.Context) error {
	args := s.configuration.getArgs(s.id)

	s.Logger.Logf("Starting application with flags: %s", strings.Join(args, " "))
	cmd := s.commandFactory()
	cmd.SetArgs(args)
	cmd.SilenceErrors = true
	output := s.configuration.Output
	if output == nil {
		output = io.Discard
	}
	cmd.SetOut(output)
	cmd.SetErr(output)

	ctx = logging.ContextWithLogger(ctx, logging.Testing())
	ctx = service.ContextWithLifecycle(ctx)
	ctx, cancel := context.WithCancel(ctx)

	for _, instrument := range s.Instruments {
		ctx = instrument.Instrument(ctx)
	}

	go func() {
		s.errorChan <- cmd.ExecuteContext(ctx)
	}()

	select {
	case <-service.Ready(ctx):
	case err := <-s.errorChan:
		cancel()
		if err != nil {
			return err
		}

		return errors.New("unexpected service stop")
	}

	s.ctx, s.cancel = ctx, cancel

	return nil
}

func (s *Service[Cfg]) Stop(ctx context.Context) error {
	if s.cancel == nil {
		return nil
	}
	s.cancel()
	s.cancel = nil

	// Wait app to be marked as stopped
	select {
	case <-service.Stopped(s.ctx):
	case <-ctx.Done():
		return errors.New("service should have been stopped")
	}

	// Ensure the app has been properly shutdown
	select {
	case err := <-s.errorChan:
		return err
	case <-ctx.Done():
		return errors.New("service should have been stopped without error")
	}
}

func (s *Service[Cfg]) Restart(ctx context.Context) error {
	if err := s.Stop(ctx); err != nil {
		return err
	}
	if err := s.Start(ctx); err != nil {
		return err
	}

	return nil
}

func (s *Service[Cfg]) GetConfiguration() Configuration[Cfg] {
	return s.configuration
}

func (s *Service[cfg]) GetContext() context.Context {
	return s.ctx
}

func New[Cfg SpecializedConfiguration](commandFactory func() *cobra.Command, configuration Configuration[Cfg], opts ...Option) *Service[Cfg] {
	baseConfiguration := &BaseConfiguration{}
	for _, opt := range append(defaultOptions, opts...) {
		opt(baseConfiguration)
	}
	return &Service[Cfg]{
		BaseConfiguration: *baseConfiguration,
		commandFactory:    commandFactory,
		configuration:     configuration,
		id:                uuid.NewString()[:8],
		errorChan:         make(chan error, 1),
	}
}

type BaseConfiguration struct {
	Logger      Logger
	Instruments []Instrumentation
}

type Option func(s *BaseConfiguration)

func WithLogger(logger Logger) Option {
	return func(s *BaseConfiguration) {
		s.Logger = logger
	}
}

func WithInstruments(instruments ...Instrumentation) Option {
	return func(s *BaseConfiguration) {
		s.Instruments = append(s.Instruments, instruments...)
	}
}

var defaultOptions = []Option{
	WithLogger(NoOpLogger),
}
