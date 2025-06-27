package testservice

import (
	"context"

	"github.com/formancehq/go-libs/v3/grpcserver"
)

func GRPCServerInstrumentation() Instrumentation {
	return InstrumentationFunc(func(ctx context.Context, cfg *RunConfiguration) error {
		cfg.WrapContext(func(ctx context.Context) context.Context {
			return grpcserver.ContextWithServerInfo(ctx)
		})
		return nil
	})
}

func GetGRPCAddress(service *Service) string {
	return grpcserver.Address(service.GetContext())
}
