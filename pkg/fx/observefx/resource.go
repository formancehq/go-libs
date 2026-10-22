package observefx

import (
	"github.com/spf13/cobra"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v5/pkg/fx/otelfx"
	"github.com/formancehq/go-libs/v5/pkg/observe"
)

// ResourceModule delegates to the SDK-independent otelfx package.
func ResourceModule(cfg observe.Config) fx.Option {
	return otelfx.ResourceModule(cfg)
}

// ResourceModuleFromFlags delegates to the SDK-independent otelfx package.
func ResourceModuleFromFlags(cmd *cobra.Command, opts ...observe.Option) fx.Option {
	return otelfx.ResourceModuleFromFlags(cmd, opts...)
}
