package iam

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestNewAuthTokenProvider(t *testing.T) {
	t.Skip("Ce test nécessite des dépendances AWS")
}

func TestAddFlags(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	
	AddFlags(flags)
	
	region, err := flags.GetString(AWSRegionFlag)
	require.NoError(t, err)
	require.Equal(t, "", region, "La région par défaut devrait être vide")
	
	accessKeyID, err := flags.GetString(AWSAccessKeyIDFlag)
	require.NoError(t, err)
	require.Equal(t, "", accessKeyID, "L'access key ID par défaut devrait être vide")
	
	secretAccessKey, err := flags.GetString(AWSSecretAccessKeyFlag)
	require.NoError(t, err)
	require.Equal(t, "", secretAccessKey, "La secret access key par défaut devrait être vide")
	
	sessionToken, err := flags.GetString(AWSSessionTokenFlag)
	require.NoError(t, err)
	require.Equal(t, "", sessionToken, "Le session token par défaut devrait être vide")
	
	profile, err := flags.GetString(AWSProfileFlag)
	require.NoError(t, err)
	require.Equal(t, "", profile, "Le profil par défaut devrait être vide")
	
	roleArn, err := flags.GetString(AWSRoleArnFlag)
	require.NoError(t, err)
	require.Equal(t, "", roleArn, "Le role ARN par défaut devrait être vide")
}

func TestLoadOptionFromCommand(t *testing.T) {
	t.Run("with credentials", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().String(AWSRegionFlag, "", "")
		cmd.Flags().String(AWSAccessKeyIDFlag, "", "")
		cmd.Flags().String(AWSSecretAccessKeyFlag, "", "")
		cmd.Flags().String(AWSSessionTokenFlag, "", "")
		cmd.Flags().String(AWSProfileFlag, "", "")
		
		cmd.Flags().Set(AWSRegionFlag, "eu-west-1")
		cmd.Flags().Set(AWSAccessKeyIDFlag, "test-access-key")
		cmd.Flags().Set(AWSSecretAccessKeyFlag, "test-secret-key")
		cmd.Flags().Set(AWSSessionTokenFlag, "test-session-token")
		cmd.Flags().Set(AWSProfileFlag, "test-profile")
		
		loadOption := LoadOptionFromCommand(cmd)
		
		opts := &config.LoadOptions{}
		err := loadOption(opts)
		require.NoError(t, err, "L'application des options ne devrait pas échouer")
		
		require.Equal(t, "eu-west-1", opts.Region, "La région devrait être correctement définie")
		require.Equal(t, "test-profile", opts.SharedConfigProfile, "Le profil devrait être correctement défini")
		
		require.NotNil(t, opts.Credentials, "Les credentials ne devraient pas être nil")
		
		creds, err := opts.Credentials.Retrieve(context.Background())
		require.NoError(t, err, "La récupération des credentials ne devrait pas échouer")
		require.Equal(t, "test-access-key", creds.AccessKeyID, "L'access key ID devrait être correctement défini")
		require.Equal(t, "test-secret-key", creds.SecretAccessKey, "La secret access key devrait être correctement définie")
		require.Equal(t, "test-session-token", creds.SessionToken, "Le session token devrait être correctement défini")
		require.Equal(t, "flags", creds.Source, "La source devrait être 'flags'")
	})
	
	t.Run("without credentials", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().String(AWSRegionFlag, "", "")
		cmd.Flags().String(AWSAccessKeyIDFlag, "", "")
		cmd.Flags().String(AWSSecretAccessKeyFlag, "", "")
		cmd.Flags().String(AWSSessionTokenFlag, "", "")
		cmd.Flags().String(AWSProfileFlag, "", "")
		
		cmd.Flags().Set(AWSRegionFlag, "eu-west-1")
		cmd.Flags().Set(AWSProfileFlag, "test-profile")
		
		loadOption := LoadOptionFromCommand(cmd)
		
		opts := &config.LoadOptions{}
		err := loadOption(opts)
		require.NoError(t, err, "L'application des options ne devrait pas échouer")
		
		require.Equal(t, "eu-west-1", opts.Region, "La région devrait être correctement définie")
		require.Equal(t, "test-profile", opts.SharedConfigProfile, "Le profil devrait être correctement défini")
		
		require.Nil(t, opts.Credentials, "Les credentials devraient être nil")
	})
}
