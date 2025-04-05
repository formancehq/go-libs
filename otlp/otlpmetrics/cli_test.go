package otlpmetrics

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestHeadersParsing(t *testing.T) {
	cmd := &cobra.Command{}
	AddFlags(cmd.Flags())

	cmd.SetArgs([]string{
		"--otel-metrics-exporter-otlp-headers=Authorization=Bearer token,Content-Type=application/json",
	})
	err := cmd.Execute()
	assert.NoError(t, err)

	headers, _ := cmd.Flags().GetStringSlice(OtelMetricsExporterOTLPHeadersFlag)
	assert.Len(t, headers, 2)

	cmd2 := &cobra.Command{}
	AddFlags(cmd2.Flags())
	cmd2.SetArgs([]string{
		"--otel-metrics-exporter-otlp-headers=Authorization=Bearer token,BadHeader",
	})
	err = cmd2.Execute()
	assert.NoError(t, err)

	headers, _ = cmd2.Flags().GetStringSlice(OtelMetricsExporterOTLPHeadersFlag)

	cmd3 := &cobra.Command{}
	AddFlags(cmd3.Flags())
	cmd3.SetArgs([]string{
		"--otel-metrics-exporter-otlp-headers=Authorization=Bearer token,BadHeader",
		"--otel-metrics-exporter-otlp-mode=grpc",
	})
	err = cmd3.Execute()
	assert.NoError(t, err)

	_ = FXModuleFromFlags(cmd3)
}
