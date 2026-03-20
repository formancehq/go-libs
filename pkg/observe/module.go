package observe

type Config struct {
	ServiceName        string
	ResourceAttributes []string
	ServiceVersion     string
}

type Option func(*Config)

func WithServiceVersion(version string) Option {
	return func(cfg *Config) {
		cfg.ServiceVersion = version
	}
}

func NewConfig(opts ...Option) Config {
	cfg := Config{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
