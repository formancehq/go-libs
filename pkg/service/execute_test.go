package service

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestBindEnvBeforeRunRejectsMalformedSelectedEnvironment(t *testing.T) {
	t.Setenv("GRPC_PORT", "not-an-integer")

	runCalled := false
	root := &cobra.Command{Use: "service"}
	run := &cobra.Command{
		Use: "run",
		Run: func(*cobra.Command, []string) {
			runCalled = true
		},
	}
	run.Flags().Int("grpc-port", 8888, "")
	root.AddCommand(run)
	root.SetArgs([]string{"run"})
	root.SilenceUsage = true
	root.SilenceErrors = true

	bindEnvForExecute(root)
	err := root.Execute()
	require.Error(t, err)
	require.ErrorContains(t, err, "GRPC_PORT")
	require.ErrorContains(t, err, "--grpc-port")
	require.False(t, runCalled)
}

func TestBindEnvBeforeRunPreservesExplicitFlagPrecedence(t *testing.T) {
	t.Setenv("GRPC_PORT", "not-an-integer")

	var effectivePort int
	root := &cobra.Command{Use: "service"}
	run := &cobra.Command{
		Use: "run",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var err error
			effectivePort, err = cmd.Flags().GetInt("grpc-port")

			return err
		},
	}
	run.Flags().Int("grpc-port", 8888, "")
	root.AddCommand(run)
	root.SetArgs([]string{"run", "--grpc-port", "9999"})

	bindEnvForExecute(root)
	require.NoError(t, root.Execute())
	require.Equal(t, 9999, effectivePort)
}

func TestBindEnvForExecuteBindsValidEnvironmentBeforeExecution(t *testing.T) {
	t.Setenv("GRPC_PORT", "9999")

	root := &cobra.Command{Use: "service"}
	run := &cobra.Command{Use: "run", Run: func(*cobra.Command, []string) {}}
	run.Flags().Int("grpc-port", 8888, "")
	root.AddCommand(run)

	bindEnvForExecute(root)
	port, err := run.Flags().GetInt("grpc-port")
	require.NoError(t, err)
	require.Equal(t, 9999, port)
}

func TestBindEnvBeforeRunIgnoresMalformedEnvironmentForUnselectedCommand(t *testing.T) {
	t.Setenv("GRPC_PORT", "not-an-integer")

	root := &cobra.Command{Use: "service"}
	run := &cobra.Command{Use: "run", Run: func(*cobra.Command, []string) {}}
	run.Flags().Int("grpc-port", 8888, "")
	versionCalled := false
	version := &cobra.Command{
		Use: "version",
		Run: func(*cobra.Command, []string) {
			versionCalled = true
		},
	}
	root.AddCommand(run, version)
	root.SetArgs([]string{"version"})

	bindEnvForExecute(root)
	require.NoError(t, root.Execute())
	require.True(t, versionCalled)
}
