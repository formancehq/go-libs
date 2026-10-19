// Package profiling exposes Go profiles over HTTP through /debug/pprof.
// The endpoints support local diagnostics and periodic retrieval by an external
// collector. This package does not schedule collection or upload profiles.
//
// Package observe/pyroscope provides native SDK collection and push uploads.
// Package observe/pyroscopetraces provides trace correlation without collection
// or uploads. OpenTelemetry trace, metric and log exporters do not export these
// profiles. See docs/PYROSCOPE.md for OpenTelemetry Profiles maturity and Go
// support, and for collection paths and composition limits.
package profiling
