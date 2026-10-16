//go:build !pyroscope

package observefx_test

import (
	"github.com/formancehq/go-libs/v5/pkg/fx/observefx"
	"github.com/formancehq/go-libs/v5/pkg/observe/pyroscope"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"testing"
)

func TestNativeProfilingBuildDisabledModuleIsNoop(t *testing.T) {
	app := fx.New(fx.NopLogger, observefx.PyroscopeModule(pyroscope.Config{Enabled: true}))
	require.NoError(t, app.Err())
	require.NoError(t, app.Start(t.Context()))
	require.NoError(t, app.Stop(t.Context()))
}
