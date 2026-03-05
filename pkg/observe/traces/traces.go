package traces

const (
	StdoutExporter = "stdout"
	OTLPExporter   = "otlp"
)

type JaegerConfig struct {
	Endpoint string
	User     string
	Password string
}

type OTLPConfig struct {
	Mode     string
	Endpoint string
	Insecure bool
}

type ModuleConfig struct {
	Exporter           string
	Batch              bool
	JaegerConfig       *JaegerConfig
	OTLPConfig         *OTLPConfig
	ResourceAttributes []string
	ServiceName        string
}
