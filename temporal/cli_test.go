package temporal

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestAddFlags(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	AddFlags(flags)
	
	address, err := flags.GetString(TemporalAddressFlag)
	require.NoError(t, err)
	require.Equal(t, "", address, "L'adresse par défaut devrait être vide")
	
	namespace, err := flags.GetString(TemporalNamespaceFlag)
	require.NoError(t, err)
	require.Equal(t, "default", namespace, "Le namespace par défaut devrait être 'default'")
	
	clientCert, err := flags.GetString(TemporalSSLClientCertFlag)
	require.NoError(t, err)
	require.Empty(t, clientCert, "Le certificat client devrait être vide par défaut")
	
	clientKey, err := flags.GetString(TemporalSSLClientKeyFlag)
	require.NoError(t, err)
	require.Empty(t, clientKey, "La clé client devrait être vide par défaut")
	
	taskQueue, err := flags.GetString(TemporalTaskQueueFlag)
	require.NoError(t, err)
	require.Equal(t, "default", taskQueue, "La file d'attente par défaut devrait être 'default'")
	
	initSearchAttributes, err := flags.GetBool(TemporalInitSearchAttributesFlag)
	require.NoError(t, err)
	require.False(t, initSearchAttributes, "L'initialisation des attributs de recherche devrait être désactivée par défaut")
	
	encryptionEnabled, err := flags.GetBool(TemporalEncryptionEnabledFlag)
	require.NoError(t, err)
	require.False(t, encryptionEnabled, "Le chiffrement devrait être désactivé par défaut")
	
	encryptionKey, err := flags.GetString(TemporalEncryptionAESKeyFlag)
	require.NoError(t, err)
	require.Empty(t, encryptionKey, "La clé de chiffrement devrait être vide par défaut")
}
