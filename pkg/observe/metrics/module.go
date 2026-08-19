package metrics

import (
	"time"
)

const (
	StdoutExporter = "stdout"
	OTLPExporter   = "otlp"
)

type ModuleConfig struct {
	RuntimeMetrics              bool
	MinimumReadMemStatsInterval time.Duration

	Exporter           string
	OTLPConfig         *OTLPConfig
	PushInterval       time.Duration
	ResourceAttributes []string
	KeepInMemory       bool
}

type OTLPConfig struct {
	Mode     string
	Endpoint string
	Insecure bool
}
