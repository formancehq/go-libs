package licence

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v3/logging"
)

type cliMockLogger struct {
	logging.Logger
}

func (m *cliMockLogger) Info(args ...any)                                {}
func (m *cliMockLogger) Infof(format string, args ...any)                {}
func (m *cliMockLogger) Error(args ...any)                               {}
func (m *cliMockLogger) Errorf(format string, args ...any)               {}
func (m *cliMockLogger) Warn(args ...any)                                {}
func (m *cliMockLogger) Warnf(format string, args ...any)                {}
func (m *cliMockLogger) Debug(args ...any)                               {}
func (m *cliMockLogger) Debugf(format string, args ...any)               {}
func (m *cliMockLogger) WithFields(fields map[string]any) logging.Logger { return m }
func (m *cliMockLogger) WithField(key string, value any) logging.Logger  { return m }
func (m *cliMockLogger) WithContext(ctx context.Context) logging.Logger  { return m }
func (m *cliMockLogger) Writer() io.Writer                               { return io.Discard }

func TestAddFlags(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	AddFlags(flags)

	// Verify that all flags are added
	token, err := flags.GetString(LicenceTokenFlag)
	require.NoError(t, err)
	require.Empty(t, token)

	tick, err := flags.GetDuration(LicenceValidateTickFlag)
	require.NoError(t, err)
	require.Equal(t, 2*time.Minute, tick)

	clusterID, err := flags.GetString(LicenceClusterIDFlag)
	require.NoError(t, err)
	require.Empty(t, clusterID)

	issuer, err := flags.GetString(LicenceExpectedIssuerFlag)
	require.NoError(t, err)
	require.Empty(t, issuer)
}

func TestFXModuleFromFlags(t *testing.T) {
	t.Run("licence not enabled", func(t *testing.T) {
		licenceEnabled = false
		cmd := &cobra.Command{}
		flags := cmd.Flags()
		AddFlags(flags)

		option := FXModuleFromFlags(cmd, "test-service")
		require.NotNil(t, option)

		app := fx.New(
			option,
			fx.NopLogger,
		)
		require.NoError(t, app.Err())
	})

	t.Run("licence enabled", func(t *testing.T) {
		licenceEnabled = true
		cmd := &cobra.Command{}
		flags := cmd.Flags()
		AddFlags(flags)

		// Set flag values
		require.NoError(t, flags.Set(LicenceTokenFlag, "test-token"))
		require.NoError(t, flags.Set(LicenceValidateTickFlag, "1m"))
		require.NoError(t, flags.Set(LicenceClusterIDFlag, "test-cluster"))
		require.NoError(t, flags.Set(LicenceExpectedIssuerFlag, "test-issuer"))

		option := FXModuleFromFlags(cmd, "test-service")
		require.NotNil(t, option)

		app := fx.New(
			option,
			fx.Provide(func() logging.Logger {
				return &cliMockLogger{}
			}),
			fx.NopLogger,
		)
		require.NoError(t, app.Err())
	})
}

type mockShutdowner struct {
	shutdownCalled bool
}

func (m *mockShutdowner) Shutdown(opts ...fx.ShutdownOption) error {
	m.shutdownCalled = true
	return nil
}

func TestWaitLicenceError(t *testing.T) {
	t.Run("error received", func(t *testing.T) {
		licenceErrorChan := make(chan error, 1)
		shutdowner := &mockShutdowner{}

		licenceErrorChan <- fmt.Errorf("test error")
		waitLicenceError(licenceErrorChan, shutdowner)
		require.True(t, shutdowner.shutdownCalled)
	})

	t.Run("channel closed", func(t *testing.T) {
		licenceErrorChan := make(chan error, 1)
		shutdowner := &mockShutdowner{}

		close(licenceErrorChan)
		waitLicenceError(licenceErrorChan, shutdowner)
		require.False(t, shutdowner.shutdownCalled)
	})
}
