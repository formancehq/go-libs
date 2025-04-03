package testservice

import (
	"io"
)

func OutputInstrumentation(output io.Writer) Instrumentation {
	return InstrumentationFunc(func(cfg *RunConfiguration) {
		cfg.output = output
	})
}
