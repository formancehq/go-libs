//go:build !pyroscope

package pyroscope_test

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/observe/pyroscope"
)

func TestBuildDisabledDoesNotAdvertiseOrEnableNativeProfiling(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	pyroscope.AddFlags(flags)
	require.False(t, flags.HasFlags())
	require.False(t, pyroscope.DefaultConfig().Enabled)
	require.False(t, pyroscope.ConfigFromFlags(flags).Enabled)
}
