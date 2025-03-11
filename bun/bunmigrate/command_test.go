package bunmigrate_test

import (
	"bytes"
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
		executorCalled bool
		executorError  error
		expectedOutput string
		expectedError  bool
	}{
		{
			name:           "basic command",
			options:        nil,
			args:           []string{},
			executorCalled: true,
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
			executorCalled: true,
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
			executorCalled: true,
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
			executorCalled := false
			executor := func(cmd *cobra.Command, args []string, db *bun.DB) error {
				executorCalled = true
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
			require.NotNil(t, cmd.Flags().Lookup("database-source-name"))
			require.NotNil(t, cmd.Flags().Lookup("database-max-open-connections"))
			require.NotNil(t, cmd.Flags().Lookup("database-max-idle-connections"))
			require.NotNil(t, cmd.Flags().Lookup("database-connection-max-lifetime"))
			require.NotNil(t, cmd.Flags().Lookup("database-connection-max-idle-time"))

			// Note: We can't fully test the RunE function because it requires a database connection
			// Instead, we'll verify that the command is properly constructed
			require.NotNil(t, cmd.RunE)
		})
	}
}

func TestNewDefaultCommandWithMockRun(t *testing.T) {
	t.Parallel()

	// Create a mock Run function that we can use to test the command without a real database
	originalRun := bunmigrate.Run
	defer func() {
		bunmigrate.Run = originalRun
	}()

	testCases := []struct {
		name          string
		mockRunError  error
		expectedError bool
	}{
		{
			name:          "successful run",
			mockRunError:  nil,
			expectedError: false,
		},
		{
			name:          "run with error",
			mockRunError:  &mockError{message: "mock error"},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Mock the Run function
			bunmigrate.Run = func(cmd *cobra.Command, args []string, executor bunmigrate.Executor) error {
				return tc.mockRunError
			}

			// Create a mock executor
			executor := func(cmd *cobra.Command, args []string, db *bun.DB) error {
				return nil
			}

			// Create the command
			cmd := bunmigrate.NewDefaultCommand(executor)
			
			// Set up command for execution
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs([]string{})

			// Execute the command
			err := cmd.Execute()

			// Verify the result
			if tc.expectedError {
				require.Error(t, err)
				require.Equal(t, tc.mockRunError.Error(), err.Error())
			} else {
				require.NoError(t, err)
			}
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
