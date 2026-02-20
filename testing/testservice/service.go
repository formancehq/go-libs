package testservice

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/formancehq/go-libs/v4/logging"
	"github.com/formancehq/go-libs/v4/service"
)

type Service struct {
	BaseConfiguration
	commandFactory func() *cobra.Command
	cancel         func()
	ctx            context.Context
	errorChan      chan error
	id             string
}

func (s *Service) GetID() string {
	return s.id
}

func (s *Service) Start(ctx context.Context) error {

	ctx = logging.ContextWithLogger(ctx, logging.Testing())
	ctx = service.ContextWithLifecycle(ctx)
	ctx, cancel := context.WithCancel(ctx)

	runConfiguration := &RunConfiguration{
		ctx:       ctx,
		serviceID: s.id,
	}
	for _, instrument := range s.Instruments {
		err := instrument.Instrument(ctx, runConfiguration)
		if err != nil {
			cancel()
			return err
		}
	}

	s.Logger.Logf("Starting application with flags: %s", strings.Join(runConfiguration.args, " "))
	cmd := s.commandFactory()
	cmd.SetArgs(runConfiguration.args)
	cmd.SilenceErrors = true
	output := runConfiguration.output
	if output == nil {
		output = io.Discard
	}
	cmd.SetOut(output)
	cmd.SetErr(output)

	go func() {
		s.errorChan <- cmd.ExecuteContext(runConfiguration.ctx)
	}()

	select {
	case <-service.Ready(runConfiguration.ctx):
	case err := <-s.errorChan:
		cancel()
		if err != nil {
			return err
		}

		return errors.New("unexpected service stop")
	}

	s.ctx, s.cancel = runConfiguration.ctx, cancel

	return nil
}

func (s *Service) Stop(ctx context.Context) error {
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

func (s *Service) Restart(ctx context.Context) error {
	if err := s.Stop(ctx); err != nil {
		return err
	}
	if err := s.Start(ctx); err != nil {
		return err
	}

	return nil
}

func (s *Service) GetContext() context.Context {
	return s.ctx
}

func New(commandFactory func() *cobra.Command, opts ...Option) *Service {
	baseConfiguration := &BaseConfiguration{}
	for _, opt := range append(defaultOptions, opts...) {
		opt(baseConfiguration)
	}
	return &Service{
		BaseConfiguration: *baseConfiguration,
		commandFactory:    commandFactory,
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
