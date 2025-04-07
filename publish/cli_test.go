package publish

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestAddFlags(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)

	// Appeler AddFlags qui va définir les drapeaux
	AddFlags("test-service", flags)

	// Vérifier que les drapeaux sont définis avec les valeurs par défaut
	topicMapping, err := flags.GetStringSlice(PublisherTopicMappingFlag)
	require.NoError(t, err)
	require.Empty(t, topicMapping, "Le mapping de topics par défaut devrait être vide")

	circuitBreakerEnabled, err := flags.GetBool(PublisherCircuitBreakerEnabledFlag)
	require.NoError(t, err)
	require.False(t, circuitBreakerEnabled, "Le circuit breaker devrait être désactivé par défaut")

	httpEnabled, err := flags.GetBool(PublisherHttpEnabledFlag)
	require.NoError(t, err)
	require.False(t, httpEnabled, "HTTP devrait être désactivé par défaut")

	kafkaEnabled, err := flags.GetBool(PublisherKafkaEnabledFlag)
	require.NoError(t, err)
	require.False(t, kafkaEnabled, "Kafka devrait être désactivé par défaut")

	natsEnabled, err := flags.GetBool(PublisherNatsEnabledFlag)
	require.NoError(t, err)
	require.False(t, natsEnabled, "NATS devrait être désactivé par défaut")
}

func TestInitNatsCLIFlags(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)

	// Appeler InitNatsCLIFlags qui va définir les drapeaux
	InitNatsCLIFlags(flags, "test-service")

	// Vérifier que les drapeaux sont définis avec les valeurs par défaut
	natsURL, err := flags.GetString(PublisherNatsURLFlag)
	require.NoError(t, err)
	require.Equal(t, "", natsURL, "L'URL NATS par défaut devrait être une chaîne vide")

	natsGroup, err := flags.GetString(PublisherQueueGroupFlag)
	require.NoError(t, err)
	require.Equal(t, "test-service", natsGroup, "Le groupe NATS par défaut devrait être le nom du service")

	// Vérifier que les autres drapeaux sont définis
	natsEnabled, err := flags.GetBool(PublisherNatsEnabledFlag)
	require.NoError(t, err)
	require.False(t, natsEnabled, "NATS devrait être désactivé par défaut")

	natsAutoProvision, err := flags.GetBool(PublisherNatsAutoProvisionFlag)
	require.NoError(t, err)
	require.True(t, natsAutoProvision, "L'auto-provisionnement NATS devrait être activé par défaut")
}

func TestFXModuleFromFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String(PublisherQueueGroupFlag, "test-service", "")
	cmd.Flags().StringSlice(PublisherTopicMappingFlag, []string{}, "")
	cmd.Flags().Bool(PublisherCircuitBreakerEnabledFlag, false, "")
	cmd.Flags().Bool(PublisherHttpEnabledFlag, false, "")
	cmd.Flags().Bool(PublisherNatsEnabledFlag, false, "")
	cmd.Flags().Bool(PublisherKafkaEnabledFlag, false, "")
	cmd.Flags().String(PublisherNatsURLFlag, "", "")
	cmd.Flags().Bool(PublisherNatsAutoProvisionFlag, true, "")
	cmd.Flags().Int(PublisherNatsMaxReconnectFlag, -1, "")
	cmd.Flags().Duration(PublisherNatsReconnectWaitFlag, 0, "")

	module := FXModuleFromFlags(cmd, false)
	require.NotNil(t, module, "Le module ne devrait pas être nil")

	// Tester avec NATS activé
	cmd.Flags().Set(PublisherNatsEnabledFlag, "true")
	module = FXModuleFromFlags(cmd, false)
	require.NotNil(t, module, "Le module ne devrait pas être nil")

	// Tester avec Kafka activé
	cmd.Flags().Set(PublisherNatsEnabledFlag, "false")
	cmd.Flags().Set(PublisherKafkaEnabledFlag, "true")
	module = FXModuleFromFlags(cmd, false)
	require.NotNil(t, module, "Le module ne devrait pas être nil")

	// Tester avec HTTP activé
	cmd.Flags().Set(PublisherKafkaEnabledFlag, "false")
	cmd.Flags().Set(PublisherHttpEnabledFlag, "true")
	module = FXModuleFromFlags(cmd, false)
	require.NotNil(t, module, "Le module ne devrait pas être nil")
}
