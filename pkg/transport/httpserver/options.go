package httpserver

// defaultIgnoredPaths lists the request paths that the logging and tracing
// middlewares skip by default. These are infrastructure probe endpoints that
// would otherwise emit one log line and one span on every scrape, drowning out
// real traffic.
//
// Extend the set with AppendIgnoredPaths, or replace it entirely with
// WithIgnoredPaths.
var defaultIgnoredPaths = []string{"/_healthcheck", "/_info"}

// Option configures the logging and tracing middlewares (LoggerMiddleware and
// OTLPMiddleware). Options are applied in the order they are given.
type Option func(*middlewareConfig)

type middlewareConfig struct {
	ignoredPaths map[string]struct{}
}

func newMiddlewareConfig(opts ...Option) *middlewareConfig {
	cfg := &middlewareConfig{ignoredPaths: pathSet(defaultIgnoredPaths)}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

func pathSet(paths []string) map[string]struct{} {
	set := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		set[p] = struct{}{}
	}
	return set
}

func (c *middlewareConfig) isIgnored(path string) bool {
	_, ok := c.ignoredPaths[path]
	return ok
}

// AppendIgnoredPaths adds paths to the set the middlewares skip, on top of the
// defaults ("/_healthcheck", "/_info"). This is the option to reach for in
// almost all cases.
func AppendIgnoredPaths(paths ...string) Option {
	return func(c *middlewareConfig) {
		for _, p := range paths {
			c.ignoredPaths[p] = struct{}{}
		}
	}
}

// WithIgnoredPaths replaces the entire ignore set, discarding the defaults —
// including "/_healthcheck" and "/_info". Pass them explicitly if you still
// want them skipped. Calling WithIgnoredPaths() with no arguments makes the
// middlewares log and trace every request.
//
// Most callers want AppendIgnoredPaths instead.
func WithIgnoredPaths(paths ...string) Option {
	return func(c *middlewareConfig) {
		c.ignoredPaths = pathSet(paths)
	}
}
