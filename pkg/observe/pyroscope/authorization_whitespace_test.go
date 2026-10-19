//go:build pyroscope

package pyroscope

import (
	"bytes"
	"encoding/base64"
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

func TestScrubberNormalizesWholeAuthorizationValue(t *testing.T) {
	const token = "synthetic-header-normalization-token"
	const user, password = "synthetic-auth-user", "synthetic-auth-password"
	basic := base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
	for _, header := range []string{"Authorization", "Proxy-Authorization"} {
		for _, tc := range []struct {
			name, value, diagnostic string
		}{
			{"bearer", " \tBearer " + token + " \t", "rejected " + token},
			{"basic", " \tBasic " + basic + " \t", "rejected " + basic + " " + user + " " + password},
		} {
			t.Run(header+"/"+tc.name, func(t *testing.T) {
				cfg := DefaultConfig()
				cfg.HTTPHeaders = map[string]string{header: tc.value}
				redacted := cfg.scrubber()(tc.diagnostic)
				for _, secret := range []string{token, basic, user, password} {
					if strings.Contains(redacted, secret) {
						t.Error("diagnostic contains a configured authentication component")
					}
				}
				require.Equal(t, tc.value, cfg.HTTPHeaders[header], "scrubbing must not modify request headers")
			})
		}
	}
}

func TestRealSDKMasksWireNormalizedAuthorizationCredential(t *testing.T) {
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
	if !strings.Contains(output.String(), "rejected") {
		t.Error("SDK callback did not exercise the echoed-credential diagnostic")
	}
	if strings.Contains(output.String(), token) {
		t.Error("SDK callback disclosed a configured authentication component")
	}
	require.Equal(t, header, cfg.HTTPHeaders["Authorization"])
}
