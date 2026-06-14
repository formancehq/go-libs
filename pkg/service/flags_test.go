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

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Bool("enabled", false, "")

	require.NotPanics(t, func() {
		BindEnvToFlagSet(flags)
	})

	enabled, err := flags.GetBool("enabled")
	require.NoError(t, err)
	require.False(t, enabled)
}
