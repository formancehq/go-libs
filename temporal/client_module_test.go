package temporal

import (
	"context"
	"testing"

	"github.com/formancehq/go-libs/v2/logging"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
)

func TestFXModuleFromFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String(TemporalAddressFlag, "localhost:7233", "")
	cmd.Flags().String(TemporalNamespaceFlag, "test-namespace", "")
	cmd.Flags().String(TemporalSSLClientCertFlag, "", "")
	cmd.Flags().String(TemporalSSLClientKeyFlag, "", "")
	cmd.Flags().Bool(TemporalInitSearchAttributesFlag, false, "")
	cmd.Flags().Bool(TemporalEncryptionEnabledFlag, false, "")
	cmd.Flags().String(TemporalEncryptionAESKeyFlag, "", "")

	tracer := trace.NewNoopTracerProvider().Tracer("test")
	searchAttributes := SearchAttributes{
		SearchAttributes: map[string]enums.IndexedValueType{
			"testAttribute": enums.INDEXED_VALUE_TYPE_TEXT,
		},
	}

	module := FXModuleFromFlags(cmd, tracer, searchAttributes)
	require.NotNil(t, module, "Le module fx ne devrait pas être nil")
}

func TestCreateSearchAttributes(t *testing.T) {
	t.Skip("Ce test nécessite un serveur Temporal en cours d'exécution")

	c, err := client.Dial(client.Options{
		HostPort: "localhost:7233",
	})
	require.NoError(t, err)
	defer c.Close()

	searchAttributes := map[string]enums.IndexedValueType{
		"testAttribute": enums.INDEXED_VALUE_TYPE_TEXT,
	}

	err = CreateSearchAttributes(context.Background(), c, "test-namespace", searchAttributes)
	require.NoError(t, err)
}

func TestClientOptionsCreation(t *testing.T) {
	logger := logging.Testing()
	meterProvider := noop.NewMeterProvider()

	options, err := func(logger logging.Logger, meterProvider noop.MeterProvider) (client.Options, error) {
		tracingInterceptor, err := opentelemetry.NewTracingInterceptor(opentelemetry.TracerOptions{
			Tracer: trace.NewNoopTracerProvider().Tracer("test"),
		})
		if err != nil {
			return client.Options{}, err
		}

		options := client.Options{
			Namespace:    "test-namespace",
			HostPort:     "localhost:7233",
			Interceptors: []interceptor.ClientInterceptor{tracingInterceptor},
			Logger:       newLogger(logger),
		}

		return options, nil
	}(logger, meterProvider)

	require.NoError(t, err)
	require.Equal(t, "test-namespace", options.Namespace)
	require.Equal(t, "localhost:7233", options.HostPort)
}
