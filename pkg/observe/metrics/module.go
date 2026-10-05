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

	// Naming and Prefix rename the instruments created through the injected
	// metric.MeterProvider; see NewRenamingMeterProvider. The zero values
	// keep the upstream names.
	Naming Naming
	Prefix string
}

type OTLPConfig struct {
	Mode     string
	Endpoint string
	Insecure bool
}
