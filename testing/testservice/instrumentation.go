package testservice

import (
	"context"
	"io"
)

type RunConfiguration struct {
	serviceID string
	ctx       context.Context
	args      []string
	output    io.Writer
}

func (cfg *RunConfiguration) GetID() string {
	return cfg.serviceID
}

func (cfg *RunConfiguration) GetArgs() []string {
	return cfg.args
}

func (cfg *RunConfiguration) GetContext() context.Context {
	return cfg.ctx
}

func (cfg *RunConfiguration) AppendArgs(args ...string) {
	cfg.args = append(cfg.args, args...)
}

func (cfg *RunConfiguration) WrapContext(fn func(context.Context) context.Context) {
	cfg.ctx = fn(cfg.ctx)
}

type Instrumentation interface {
	Instrument(ctx context.Context, cfg *RunConfiguration) error
}
type InstrumentationFunc func(ctx context.Context, cfg *RunConfiguration) error

func (f InstrumentationFunc) Instrument(ctx context.Context, cfg *RunConfiguration) error {
	return f(ctx, cfg)
}

func AppendArgsInstrumentation(args ...string) Instrumentation {
	return InstrumentationFunc(func(ctx context.Context, cfg *RunConfiguration) error {
		cfg.AppendArgs(args...)
		return nil
	})
}
