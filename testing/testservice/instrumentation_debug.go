package testservice

import (
	"context"

	"github.com/formancehq/go-libs/v4/service"
)

func DebugInstrumentation(debug bool) Instrumentation {
	return InstrumentationFunc(func(ctx context.Context, cfg *RunConfiguration) error {
		if debug {
			cfg.AppendArgs("--" + service.DebugFlag)
		}
		return nil
	})
}
