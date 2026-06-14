package publish_test

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/messaging/publish"
)

func TestInitNatsCLIFlagsUsesAutoProvisionOverride(t *testing.T) {
	t.Parallel()

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)

	publish.InitNatsCLIFlags(flags, "service", func(values *publish.ConfigDefault) {
		values.PublisherNatsAutoProvision = false
	})

	autoProvision, err := flags.GetBool(publish.PublisherNatsAutoProvisionFlag)
	require.NoError(t, err)
	require.False(t, autoProvision)
}
