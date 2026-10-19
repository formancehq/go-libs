//go:build !pyroscope

package pyroscope

import "github.com/spf13/pflag"

// Config is the build-disabled configuration. No native SDK is linked.
type Config struct {
	Enabled         bool
	ApplicationName string
	Tags            map[string]string
}

func DefaultConfig() Config { return Config{} }

// AddFlags advertises no unsupported native profiling options.
func AddFlags(*pflag.FlagSet)               {}
func ConfigFromFlags(*pflag.FlagSet) Config { return Config{} }
