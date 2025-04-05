package service

import (
	"bytes"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestExecute(t *testing.T) {
	originalOsExit := osExit
	defer func() { osExit = originalOsExit }()

	exitCalled := false
	exitCode := 0
	osExit = func(code int) {
		exitCalled = true
		exitCode = code
	}

	t.Run("successful execution", func(t *testing.T) {
		exitCalled = false
		exitCode = 0

		cmd := &cobra.Command{
			Use: "test",
			Run: func(cmd *cobra.Command, args []string) {
			},
		}

		oldStdout := os.Stdout
		oldStderr := os.Stderr
		defer func() {
			os.Stdout = oldStdout
			os.Stderr = oldStderr
		}()
		
		r, w, _ := os.Pipe()
		os.Stdout = w
		os.Stderr = w

		Execute(cmd)

		w.Close()
		var buf bytes.Buffer
		buf.ReadFrom(r)

		require.False(t, exitCalled, "os.Exit ne devrait pas être appelé")
		require.Equal(t, 0, exitCode, "Le code de sortie devrait être 0")
	})

	t.Run("failed execution", func(t *testing.T) {
		exitCalled = false
		exitCode = 0

		cmd := &cobra.Command{
			Use: "test",
			RunE: func(cmd *cobra.Command, args []string) error {
				return os.ErrInvalid
			},
		}

		oldStdout := os.Stdout
		oldStderr := os.Stderr
		defer func() {
			os.Stdout = oldStdout
			os.Stderr = oldStderr
		}()
		
		r, w, _ := os.Pipe()
		os.Stdout = w
		os.Stderr = w

		Execute(cmd)

		w.Close()
		var buf bytes.Buffer
		buf.ReadFrom(r)

		require.True(t, exitCalled, "os.Exit devrait être appelé")
		require.Equal(t, 1, exitCode, "Le code de sortie devrait être 1")
	})
}
