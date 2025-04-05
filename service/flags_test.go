package service

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestFlags(t *testing.T) {
	t.Parallel()

	oldEnv := os.Environ()
	defer func() {
		os.Clearenv()
		for _, env := range oldEnv {
			key, value, _ := strings.Cut(env, "=")
			os.Setenv(key, value)
		}
	}()

	os.Setenv("ROOT1", "changed")

	command := &cobra.Command{
		Use: "root",
	}
	command.Flags().String("root1", "test", "")

	subCommand1 := &cobra.Command{
		Use: "subcommand1",
	}
	subCommand1.Flags().String("sub1", "test", "")

	subCommand2 := &cobra.Command{
		Use: "subcommand2",
	}
	subCommand2.Flags().String("sub2", "test", "")
	subCommand2.PersistentFlags().String("persub2", "test", "")

	command.AddCommand(subCommand1, subCommand2)

	BindEnvToCommand(command)

	value, err := command.Flags().GetString("root1")
	require.NoError(t, err)
	require.Equal(t, "changed", value, "La valeur du flag devrait être mise à jour depuis l'environnement")
}

func TestBindEnvToFlagSet(t *testing.T) {
	t.Parallel()

	oldEnv := os.Environ()
	defer func() {
		os.Clearenv()
		for _, env := range oldEnv {
			key, value, _ := strings.Cut(env, "=")
			os.Setenv(key, value)
		}
	}()

	t.Run("string slice flag with spaces", func(t *testing.T) {
		set := pflag.NewFlagSet("test", pflag.ContinueOnError)
		set.StringSlice("test-slice", []string{"default"}, "test slice flag")
		
		os.Setenv("TEST_SLICE", "value1 value2 value3")
		
		BindEnvToFlagSet(set)
		
		value, err := set.GetStringSlice("test-slice")
		require.NoError(t, err)
		require.Equal(t, []string{"value1", "value2", "value3"}, value, "La valeur du slice devrait être mise à jour avec les espaces convertis en virgules")
	})
	
	t.Run("empty environment variable", func(t *testing.T) {
		set := pflag.NewFlagSet("test", pflag.ContinueOnError)
		set.String("test-flag", "default", "test flag")
		
		os.Unsetenv("TEST_FLAG")
		
		BindEnvToFlagSet(set)
		
		value, err := set.GetString("test-flag")
		require.NoError(t, err)
		require.Equal(t, "default", value, "La valeur du flag ne devrait pas être modifiée")
	})
	
	t.Run("environment variable with spaces", func(t *testing.T) {
		set := pflag.NewFlagSet("test", pflag.ContinueOnError)
		set.String("test-flag", "default", "test flag")
		
		os.Setenv("TEST_FLAG", "  env-value  ")
		
		BindEnvToFlagSet(set)
		
		value, err := set.GetString("test-flag")
		require.NoError(t, err)
		require.Equal(t, "env-value", value, "La valeur du flag devrait être mise à jour avec les espaces supprimés")
	})
	
	t.Run("invalid flag value", func(t *testing.T) {
		set := pflag.NewFlagSet("test", pflag.ContinueOnError)
		set.Bool("test-bool", false, "test bool flag")
		
		os.Setenv("TEST_BOOL", "invalid")
		
		defer func() {
			r := recover()
			require.NotNil(t, r, "La fonction devrait paniquer avec une valeur invalide")
		}()
		
		BindEnvToFlagSet(set)
	})
}
