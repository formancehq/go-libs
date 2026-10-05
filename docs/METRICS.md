# Metrics naming

A service can give its own metrics a namespace and choose between OpenTelemetry
and Prometheus naming. With the prefix `formance.ledger`, an instrument created
as `cache.size` is exported as:

| `--otel-metrics-naming` | `--otel-metrics-prefix` | Exported name |
| --- | --- | --- |
| `otel` (default) | empty or `none` (default) | `cache.size` |
| `otel` | `formance.ledger` | `formance.ledger.cache.size` |
| `prom` | `formance.ledger` | `formance_ledger_cache_size` |
| `prom` | empty or `none` | `cache_size` |

Both settings default to the identity, so a service that sets nothing exports
the same names as before.

## Wiring it

Through flags, which `observefx.MetricsModuleFromFlags` reads:

```go
otlpmetrics.AddFlags(cmd.Flags(), otlpmetrics.WithDefaultPrefix("formance.ledger")) // the option is optional
// ...
observefx.MetricsModuleFromFlags(cmd)
```

Or directly through the module config:

```go
observefx.MetricsModule(metrics.ModuleConfig{
	// exporter settings ...
	Naming: metrics.NamingProm,
	Prefix: "formance.ledger",
})
```

| Flag | Environment variable | Default | Values |
| --- | --- | --- | --- |
| `--otel-metrics-naming` | `OTEL_METRICS_NAMING` | `otel` | `otel` keeps dots, `prom` replaces every `.` with `_` |
| `--otel-metrics-prefix` | `OTEL_METRICS_PREFIX` | empty, or the `WithDefaultPrefix` value | dot-separated namespace; empty or `none` disables it |

Precedence, highest first: the command line, the environment variable, the
`WithDefaultPrefix` default, no prefix.

`none` exists because the service environment binder ignores empty variables:
`OTEL_METRICS_PREFIX=""` cannot turn off a service default, but
`OTEL_METRICS_PREFIX=none` can.

## Prefix rules

A prefix is one or more segments joined by `.`. Each segment starts with a
letter, ends with a letter or a digit, and contains only letters, digits and
`_`. The whole prefix is at most 64 characters. These rules keep the result
valid both as an OpenTelemetry name and, after the `prom` rewrite, as a
Prometheus name.

| Valid | Invalid |
| --- | --- |
| `formance.ledger`, `ledger`, `acme.payments_ledger2` | `formance-ledger`, `formance..ledger`, `formance.ledger.`, `formance_`, `1formance` |

An invalid naming or prefix fails `fx.New` with an error such as:

```text
invalid metrics prefix "formance-ledger": expected "none" or dot-separated segments of letters, digits and underscores, each starting with a letter and ending with a letter or digit
```

## What is renamed

Only instruments created through the `metric.MeterProvider` that the metrics
module injects are renamed. The global provider, set with
`otel.SetMeterProvider`, stays the raw SDK provider. Instrumentation that reads
the global provider keeps its semantic-convention names: the Go runtime and host
metrics, and otelhttp and otelgrpc when no provider is passed to them.

A library that is explicitly handed the injected provider is renamed like the
service's own instruments. In this module, that applies to the Temporal client
built by `workflowfx` and to `httpclient.RetryConfig.MeterProvider`.

When renaming applies, the injected value wraps the SDK provider and is no
longer a `*sdkmetric.MeterProvider`. Inject `*sdkmetric.MeterProvider` for
`ForceFlush` or `Shutdown`.

## SDK views and tooling

SDK views see the exported name, not the name passed to the constructor. A view
that matches on an instrument name must use the exported name.
`metrics.TransformName` computes it, and dashboard generators can use it the
same way:

```go
name := metrics.TransformName("cache.size", metrics.NamingProm, "formance.ledger") // formance_ledger_cache_size
view := sdkmetric.NewView(sdkmetric.Instrument{Name: name}, sdkmetric.Stream{ /* ... */ })
```

To rename a provider outside the fx module, wrap it with
`metrics.NewRenamingMeterProvider(provider, naming, prefix)`. It validates both
values and returns the provider unchanged when the policy is the identity.
