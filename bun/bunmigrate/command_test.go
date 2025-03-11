package bunmigrate_test

import (
	"testing"

	"github.com/formancehq/go-libs/v2/bun/bunmigrate"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestNewDefaultCommand(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name           string
		options        []func(*cobra.Command)
		args           []string
		executorError  error
		expectedOutput string
		expectedError  bool
	}{
		{
			name:           "basic command",
			options:        nil,
			args:           []string{},
			executorError:  nil,
			expectedOutput: "",
			expectedError:  false,
		},
		{
			name: "with custom option",
			options: []func(*cobra.Command){
				func(cmd *cobra.Command) {
					cmd.Short = "Custom short description"
				},
			},
			args:           []string{},
			executorError:  nil,
			expectedOutput: "",
			expectedError:  false,
		},
		{
			name: "with multiple custom options",
			options: []func(*cobra.Command){
				func(cmd *cobra.Command) {
					cmd.Short = "Custom short description"
				},
				func(cmd *cobra.Command) {
					cmd.Long = "Custom long description"
				},
			},
			args:           []string{},
			executorError:  nil,
			expectedOutput: "",
			expectedError:  false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Create a mock executor
			executor := func(cmd *cobra.Command, args []string, db *bun.DB) error {
				return tc.executorError
			}

			// Create the command
			cmd := bunmigrate.NewDefaultCommand(executor, tc.options...)

			// Verify command properties
			require.Equal(t, "migrate", cmd.Use)
			if len(tc.options) > 0 && tc.options[0] != nil {
				require.Equal(t, "Custom short description", cmd.Short)
			} else {
				require.Equal(t, "Run migrations", cmd.Short)
			}

			if len(tc.options) > 1 && tc.options[1] != nil {
				require.Equal(t, "Custom long description", cmd.Long)
			}

			// Verify flags
			require.NotNil(t, cmd.Flags().Lookup("postgres-uri"))
			require.NotNil(t, cmd.Flags().Lookup("postgres-max-open-conns"))
			require.NotNil(t, cmd.Flags().Lookup("postgres-max-idle-conns"))
			require.NotNil(t, cmd.Flags().Lookup("postgres-conn-max-idle-time"))
			require.NotNil(t, cmd.Flags().Lookup("postgres-aws-enable-iam"))

			// Note: We can't fully test the RunE function because it requires a database connection
			// Instead, we'll verify that the command is properly constructed
			require.NotNil(t, cmd.RunE)
		})
	}
}

// Mock error type for testing
type mockError struct {
	message string
}

func (e *mockError) Error() string {
	return e.message
}
