package service

import (
	"fmt"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestFlags(t *testing.T) {
	t.Parallel()

	err := os.Setenv("ROOT1", "changed")
	if err != nil {
		t.Fatalf("failed to set environment variable: %v", err)
	}

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

	err = command.Usage()
	if err != nil {
		t.Fatalf("failed to get usage: %v", err)
	}

	fmt.Println(command.Flags().GetString("root1"))
}

func TestBindEnvToFlagSetDoesNotEnableDebugFromBareDebugEnv(t *testing.T) {
	t.Setenv("DEBUG", "1")

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Bool(DebugFlag, false, "")

	BindEnvToFlagSet(flags)

	debug, err := flags.GetBool(DebugFlag)
	require.NoError(t, err)
	require.False(t, debug)
}

func TestBindEnvToFlagSetStillBindsNonDebugEnv(t *testing.T) {
	t.Setenv("ROOT1", "changed")

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("root1", "default", "")

	BindEnvToFlagSet(flags)

	root1, err := flags.GetString("root1")
	require.NoError(t, err)
	require.Equal(t, "changed", root1)
}

func TestBindEnvToFlagSetIgnoresInvalidEnvValue(t *testing.T) {
	t.Setenv("ENABLED", "not-a-bool")
	t.Setenv("ROOT1", "changed")

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Bool("enabled", false, "")
	flags.String("root1", "default", "")

	require.NotPanics(t, func() {
		BindEnvToFlagSet(flags)
	})

	enabled, err := flags.GetBool("enabled")
	require.NoError(t, err)
	require.False(t, enabled)
	root1, err := flags.GetString("root1")
	require.NoError(t, err)
	require.Equal(t, "changed", root1)
}

func TestBindEnvToFlagSetWithErrorRejectsMalformedTypedValues(t *testing.T) {
	tests := []struct {
		name       string
		envVar     string
		value      string
		flagName   string
		register   func(*pflag.FlagSet)
		errorMatch string
	}{
		{
			name:     "integer",
			envVar:   "GRPC_PORT",
			value:    "not-an-integer",
			flagName: "grpc-port",
			register: func(flags *pflag.FlagSet) {
				flags.Int("grpc-port", 8888, "")
			},
			errorMatch: "invalid syntax",
		},
		{
			name:     "boolean",
			envVar:   "ENABLED",
			value:    "not-a-boolean",
			flagName: "enabled",
			register: func(flags *pflag.FlagSet) {
				flags.Bool("enabled", false, "")
			},
			errorMatch: "invalid syntax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envVar, tt.value)

			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			tt.register(flags)

			err := BindEnvToFlagSetWithError(flags)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.envVar)
			require.ErrorContains(t, err, "--"+tt.flagName)
			require.ErrorContains(t, err, tt.value)
			require.ErrorContains(t, err, tt.errorMatch)
		})
	}
}

func TestBindEnvToFlagSetWithErrorAppliesValidValue(t *testing.T) {
	t.Setenv("GRPC_PORT", "9999")

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Int("grpc-port", 8888, "")

	require.NoError(t, BindEnvToFlagSetWithError(flags))
	value, err := flags.GetInt("grpc-port")
	require.NoError(t, err)
	require.Equal(t, 9999, value)
}

func TestBindEnvToFlagSetWithErrorRetainsDefaultWhenUnset(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Int("grpc-port", 8888, "")

	require.NoError(t, BindEnvToFlagSetWithError(flags))
	value, err := flags.GetInt("grpc-port")
	require.NoError(t, err)
	require.Equal(t, 8888, value)
}

func TestBindEnvToFlagSetWithErrorPrefersExplicitFlag(t *testing.T) {
	t.Setenv("GRPC_PORT", "not-an-integer")

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Int("grpc-port", 8888, "")
	require.NoError(t, flags.Parse([]string{"--grpc-port", "9999"}))

	require.NoError(t, BindEnvToFlagSetWithError(flags))
	value, err := flags.GetInt("grpc-port")
	require.NoError(t, err)
	require.Equal(t, 9999, value)
}
