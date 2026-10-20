// Package pyroscope integrates native Grafana SDK collection and push uploads.
// Compile with the pyroscope build tag to include the SDK; without it, native
// profiling is disabled. This package does not expose a pprof HTTP listener or
// export OpenTelemetry Profiles over OTLP.
//
// Package observe/profiling exposes pprof endpoints for local diagnostics or
// periodic retrieval by an external collector. Package observe/pyroscopetraces
// provides trace correlation without collection or uploads. See
// docs/PYROSCOPE.md for OpenTelemetry Profiles maturity and Go support, and for
// collection paths and composition limits.
package pyroscope
