//go:build pyroscope

package pyroscope

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/grafana/pyroscope-go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

func TestHookLifecycleAndSDKConfiguration(t *testing.T) {
	var output bytes.Buffer
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.ApplicationName = "test-service"
	cfg.AuthToken = "synthetic-token"
	cfg.BasicAuthPassword = "synthetic-password"
	cfg.Tags = map[string]string{"region": "test"}
	hook := NewHook(cfg, logging.NewZapStack(&output, zapcore.DebugLevel, logging.ZapStackOptions{JSONFormatting: true}))
	cfg.Tags["region"] = "mutated"
	starts, stops := 0, 0
	hook.start = func(got sdk.Config) (func() error, error) {
		starts++
		require.Equal(t, "test", got.Tags["region"])
		require.Equal(t, "synthetic-token", got.AuthToken)
		require.Equal(t, 15*time.Second, got.UploadRate)
		require.Len(t, got.ProfileTypes, 10)
		got.Logger.Infof("token synthetic-token")
		got.Logger.Debugf("password synthetic-password")
		got.Logger.Errorf("token synthetic-token")
		return func() error { stops++; return errors.New("stop synthetic-token") }, nil
	}
	require.NoError(t, hook.OnStop(t.Context()))
	require.NoError(t, hook.OnStart(t.Context()))
	require.NoError(t, hook.OnStart(t.Context()))
	require.Equal(t, 1, starts)
	require.NoError(t, hook.OnStop(t.Context()))
	require.NoError(t, hook.OnStop(t.Context()))
	require.Equal(t, 1, stops)
	require.Contains(t, output.String(), "Starting Pyroscope profiler")
	require.Contains(t, output.String(), "Stopping Pyroscope profiler")
	require.Contains(t, output.String(), "Error stopping Pyroscope profiler")
	require.NotContains(t, output.String(), "synthetic-token")
	require.NotContains(t, output.String(), "synthetic-password")
}

func TestStartFailureSafeAndRetryable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.AuthToken = "synthetic-secret"
	hook := NewHook(cfg, nil)
	hook.start = func(sdk.Config) (func() error, error) { return nil, errors.New("synthetic-secret rejected") }
	err := hook.OnStart(t.Context())
	require.Error(t, err)
	require.NotContains(t, fmt.Sprintf("%+v %#v", err, err), "synthetic-secret")
	require.Nil(t, errors.Unwrap(err))
	require.NoError(t, hook.OnStop(t.Context()))
	called := false
	hook.start = func(sdk.Config) (func() error, error) { return func() error { called = true; return nil }, nil }
	require.NoError(t, hook.OnStart(t.Context()))
	require.NoError(t, hook.OnStop(t.Context()))
	require.True(t, called)
}

func TestDisabledHookAndSampling(t *testing.T) {
	old := runtime.SetMutexProfileFraction(-1)
	t.Cleanup(func() { runtime.SetMutexProfileFraction(old) })
	cfg := DefaultConfig()
	cfg.MutexProfileFraction = 13
	cfg.SetupRuntimeProfiling()
	require.Equal(t, old, runtime.SetMutexProfileFraction(-1))
	hook := NewHook(cfg, nil)
	hook.start = func(sdk.Config) (func() error, error) { t.Fatal("disabled profiler started"); return nil, nil }
	require.NoError(t, hook.OnStart(t.Context()))
	require.NoError(t, hook.OnStop(t.Context()))
	cfg.Enabled = true
	cfg.BlockProfileRate = 0
	cfg.MutexProfileFraction = 0
	cfg.SetupRuntimeProfiling()
	require.Equal(t, old, runtime.SetMutexProfileFraction(-1))
	cfg.MutexProfileFraction = 13
	cfg.SetupRuntimeProfiling()
	require.Equal(t, 13, runtime.SetMutexProfileFraction(-1))
}

func TestRealSDKLifecycleUsesConfiguredCredentials(t *testing.T) {
	var authenticated atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if ok && user == "synthetic-user" && password == "synthetic-password" {
			authenticated.Store(true)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	t.Setenv("PYROSCOPE_ADHOC_SERVER_ADDRESS", server.URL)
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.ApplicationName = "test-service"
	cfg.ServerAddress = server.URL
	cfg.BasicAuthUser = "synthetic-user"
	cfg.BasicAuthPassword = "synthetic-password"
	cfg.DisableGCRuns = true
	cfg.ProfileTypes = []sdk.ProfileType{sdk.ProfileGoroutines}
	cfg.UploadRate = 100 * time.Millisecond
	hook := NewHook(cfg, nil)
	require.NoError(t, hook.OnStart(t.Context()))
	defer func() { require.NoError(t, hook.OnStop(t.Context())) }()
	require.Eventually(t, authenticated.Load, 3*time.Second, 10*time.Millisecond, "native SDK must upload using the unmodified credentials")
	require.NoError(t, hook.OnStop(t.Context()))
}

func TestRealSDKInvalidAddressDoesNotDiscloseInput(t *testing.T) {
	// An invalid address fails synchronously, without opening a network client.
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.ServerAddress = "https://synthetic-user:synthetic-password%xx@example.test"
	cfg.ApplicationName = "test-service"
	hook := NewHook(cfg, nil)
	err := hook.OnStart(t.Context())
	require.Error(t, err)
	require.False(t, strings.Contains(err.Error(), "synthetic-password"))
	require.NoError(t, hook.OnStop(t.Context()))
}
