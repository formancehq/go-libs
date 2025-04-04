package testservice

import (
	"context"
	"io"
)

func OutputInstrumentation(output io.Writer) Instrumentation {
	return InstrumentationFunc(func(ctx context.Context, cfg *RunConfiguration) error {
		cfg.output = output
		return nil
	})
}
