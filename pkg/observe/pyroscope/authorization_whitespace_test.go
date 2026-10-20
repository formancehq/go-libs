//go:build pyroscope

package pyroscope

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/grafana/pyroscope-go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

func TestRealSDKOmitsWireNormalizedAuthorizationDiagnostic(t *testing.T) {
	const token = "synthetic-wire-normalization-token"
	const header = " \tBearer " + token + " \t"
	var received atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := r.Header.Get("Authorization")
		if value == "Bearer "+token {
			received.Store(true)
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, "rejected "+strings.TrimPrefix(value, "Bearer "))
	}))
	defer server.Close()
	t.Setenv("PYROSCOPE_ADHOC_SERVER_ADDRESS", server.URL)
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.ApplicationName = "test-service"
	cfg.ServerAddress = server.URL
	cfg.HTTPHeaders = map[string]string{"Authorization": header}
	cfg.DisableGCRuns = true
	cfg.ProfileTypes = []sdk.ProfileType{sdk.ProfileGoroutines}
	cfg.UploadRate = 100 * time.Millisecond
	var output bytes.Buffer
	hook := NewHook(cfg, logging.NewZapStack(&output, zapcore.DebugLevel, logging.ZapStackOptions{JSONFormatting: true}))
	require.NoError(t, hook.OnStart(t.Context()))
	defer func() { require.NoError(t, hook.OnStop(t.Context())) }()
	require.Eventually(t, received.Load, 3*time.Second, 10*time.Millisecond, "SDK must preserve credentials while HTTP normalizes whitespace")
	require.NoError(t, hook.OnStop(t.Context()))
	require.Contains(t, output.String(), "Pyroscope SDK event")
	require.NotContains(t, output.String(), "rejected")
	if strings.Contains(output.String(), token) {
		t.Error("SDK callback disclosed a configured authentication component")
	}
	require.Equal(t, header, cfg.HTTPHeaders["Authorization"])
}
