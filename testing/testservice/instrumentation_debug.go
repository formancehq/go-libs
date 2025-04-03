package testservice

import "github.com/formancehq/go-libs/v2/service"

func DebugInstrumentation(debug bool) Instrumentation {
	return InstrumentationFunc(func(cfg *RunConfiguration) {
		if debug {
			cfg.AppendArgs("--" + service.DebugFlag)
		}
	})
}
