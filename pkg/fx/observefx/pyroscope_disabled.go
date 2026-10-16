//go:build !pyroscope

package observefx

import (
	"github.com/formancehq/go-libs/v5/pkg/observe/pyroscope"
	"go.uber.org/fx"
)

// PyroscopeModule is a no-op in builds without native profiling support.
func PyroscopeModule(pyroscope.Config) fx.Option { return fx.Options() }
