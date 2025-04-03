package testservice

import (
	"context"
)

type Instrumentation interface {
	Instrument(ctx context.Context) context.Context
}
type InstrumentationFunc func(ctx context.Context) context.Context

func (f InstrumentationFunc) Instrument(ctx context.Context) context.Context {
	return f(ctx)
}
