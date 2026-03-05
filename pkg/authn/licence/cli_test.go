package licence_test

import (
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/authn/licence"
)

func TestAddFlags(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	licence.AddFlags(flags)

	// Verify that all flags are added
	token, err := flags.GetString(licence.LicenceTokenFlag)
	require.NoError(t, err)
	require.Empty(t, token)

	tick, err := flags.GetDuration(licence.LicenceValidateTickFlag)
	require.NoError(t, err)
	require.Equal(t, 2*time.Minute, tick)

	clusterID, err := flags.GetString(licence.LicenceClusterIDFlag)
	require.NoError(t, err)
	require.Empty(t, clusterID)

	issuer, err := flags.GetString(licence.LicenceExpectedIssuerFlag)
	require.NoError(t, err)
	require.Empty(t, issuer)
}
