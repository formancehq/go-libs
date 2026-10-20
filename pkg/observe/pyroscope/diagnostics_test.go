//go:build pyroscope

package pyroscope

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	sdk "github.com/grafana/pyroscope-go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

func TestConfigDiagnosticsOmitArbitraryStrings(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.ApplicationName = "synthetic-application"
	cfg.ServerAddress = "https://synthetic-user:synthetic-password@synthetic-host/synthetic-path"
	cfg.AuthToken = "synthetic-token"
	cfg.BasicAuthUser = "synthetic-basic-user"
	cfg.BasicAuthPassword = "synthetic-basic-password"
	cfg.TenantID = "synthetic-tenant"
	cfg.Tags = map[string]string{"region": "synthetic-tag"}
	cfg.HTTPHeaders = map[string]string{"Authorization": "synthetic-header"}
	cfg.ProfileTypes = []sdk.ProfileType{"synthetic-profile-type"}
	for _, value := range []any{cfg, &cfg} {
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		for _, formatted := range []string{string(encoded), fmt.Sprint(value), fmt.Sprintf("%+v", value), fmt.Sprintf("%#v", value)} {
			require.NotContains(t, formatted, "synthetic-")
			var settings map[string]any
			require.NoError(t, json.Unmarshal([]byte(formatted), &settings))
			require.Len(t, settings, 7)
			require.Equal(t, true, settings["enabled"])
			require.Equal(t, float64(cfg.UploadRate), settings["upload_rate"])
			for _, setting := range settings {
				switch setting.(type) {
				case bool, float64:
				default:
					t.Fatalf("unexpected diagnostic type %T", setting)
				}
			}
		}
	}
}

func TestSDKLoggerDoesNotFormatArguments(t *testing.T) {
	for _, level := range []zapcore.Level{zapcore.InfoLevel, zapcore.DebugLevel} {
		t.Run(level.String(), func(t *testing.T) {
			var output bytes.Buffer
			logger := &sdkLogger{logger: logging.NewZapStack(&output, level, logging.ZapStackOptions{JSONFormatting: true})}
			argument := &observedArgument{}
			logger.Infof("synthetic-info %s", argument)
			logger.Errorf("synthetic-error %s", argument)
			logger.Debugf("synthetic-debug %s", argument)
			require.False(t, argument.formatted)
			require.NotContains(t, output.String(), "synthetic-")
			expected := 2
			if level == zapcore.DebugLevel {
				expected++
			}
			require.Equal(t, expected, bytes.Count(output.Bytes(), []byte("Pyroscope SDK event")))
		})
	}
}

type observedArgument struct{ formatted bool }

func (a *observedArgument) String() string { a.formatted = true; return "synthetic-argument" }
