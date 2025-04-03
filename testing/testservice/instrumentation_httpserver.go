package testservice

import (
	"context"

	"github.com/formancehq/go-libs/v2/httpserver"
)

func HTTPServerInstrumentation() Instrumentation {
	return InstrumentationFunc(func(cfg *RunConfiguration) {
		cfg.WrapContext(func(ctx context.Context) context.Context {
			return httpserver.ContextWithServerInfo(ctx)
		})
	})
}

func GetServerURL(srv interface {
	GetContext() context.Context
}) string {
	return httpserver.URL(srv.GetContext())
}
