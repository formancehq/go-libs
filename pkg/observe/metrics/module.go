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

	// ClassicHistograms forces explicit-bucket ("classic") histogram
	// aggregation instead of the default Base2ExponentialHistogram.
	//
	// Exponential histograms are higher resolution and cheaper to export, and
	// remain the default here for that reason -- but prometheusremotewriteexporter
	// (a common collector destination) only sends exponential-histogram data
	// points as Prometheus native histograms, and the receiving TSDB has to
	// actually understand that wire format to store them. The exporter itself
	// has supported the conversion since
	// open-telemetry/opentelemetry-collector-contrib#17370; the constraint
	// that matters today is downstream of it -- e.g. the shared VictoriaMetrics
	// instance connectivity-plugins-poc remote-writes through was confirmed
	// running v1.96.0 (2023-12-12) as of 2026-08-19, and VictoriaMetrics didn't
	// add native-histogram ingestion until v1.143.0, so it silently drops
	// every such data point instead of converting it. A service exporting
	// through that path and only that path should set this true; one
	// exporting straight OTLP to a backend with native-histogram support (or
	// scraped via the plain prometheusexporter, which does convert them)
	// should leave it false.
	ClassicHistograms bool
}

type OTLPConfig struct {
	Mode     string
	Endpoint string
	Insecure bool
}
