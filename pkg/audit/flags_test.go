package audit

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/service"
)

func TestConfigFromFlagsDisabledByDefault(t *testing.T) {
	t.Parallel()

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	AddFlags(flags)

	cfg, err := ConfigFromFlags(flags)

	require.NoError(t, err)
	assert.False(t, cfg.Enabled)
}

func TestConfigFromFlagsEnabledFromFlag(t *testing.T) {
	t.Parallel()

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	AddFlags(flags)
	require.NoError(t, flags.Set(AuditEnabledFlag, "true"))

	cfg, err := ConfigFromFlags(flags)

	require.NoError(t, err)
	assert.True(t, cfg.Enabled)
}

func TestConfigFromFlagsEnabledFromEnv(t *testing.T) {
	t.Setenv("AUDIT_ENABLED", "true")

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	AddFlags(flags)
	service.BindEnvToFlagSet(flags)

	cfg, err := ConfigFromFlags(flags)

	require.NoError(t, err)
	assert.True(t, cfg.Enabled)
}

func TestConfigFromFlagsReturnsErrorWhenFlagIsMissing(t *testing.T) {
	t.Parallel()

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)

	_, err := ConfigFromFlags(flags)

	require.Error(t, err)
	assert.Contains(t, err.Error(), AuditEnabledFlag)
}
