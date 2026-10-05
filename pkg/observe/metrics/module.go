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

	// Prefix namespaces the instruments created through the injected
	// metric.MeterProvider; see NewPrefixedMeterProvider. The zero value
	// keeps the upstream names.
	Prefix string
}

type OTLPConfig struct {
	Mode     string
	Endpoint string
	Insecure bool
}
