# Metrics prefix

A service can give its own metrics a namespace. With the prefix
`formance.ledger`, an instrument created as `cache.size` is exported as
`formance.ledger.cache.size`.

| `--otel-metrics-prefix` | Exported name |
| --- | --- |
| empty or `none` (default) | `cache.size` |
| `formance.ledger` | `formance.ledger.cache.size` |

With no prefix, a service exports the same names as before.

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
	Prefix: "formance.ledger",
})
```

| Flag | Environment variable | Default | Values |
| --- | --- | --- | --- |
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
valid both as an OpenTelemetry name and as a Prometheus name once an exporter
replaces dots with underscores.

| Valid | Invalid |
| --- | --- |
| `formance.ledger`, `ledger`, `acme.payments_ledger2` | `formance-ledger`, `formance..ledger`, `formance.ledger.`, `formance_`, `1formance` |

An invalid prefix fails `fx.New` with an error such as:

```text
invalid metrics prefix "formance-ledger": expected "none" or dot-separated segments of letters, digits and underscores, each starting with a letter and ending with a letter or digit
```

## What is prefixed

Only instruments created through the `metric.MeterProvider` that the metrics
module injects are prefixed. The global provider, set with
`otel.SetMeterProvider`, stays the raw SDK provider. Instrumentation that reads
the global provider keeps its semantic-convention names: the Go runtime and host
metrics, and otelhttp and otelgrpc when no provider is passed to them.

A library that is explicitly handed the injected provider is prefixed like the
service's own instruments. In this module, that applies to the Temporal client
built by `workflowfx` and to `httpclient.RetryConfig.MeterProvider`.

When a prefix applies, the injected value wraps the SDK provider and is no
longer a `*sdkmetric.MeterProvider`. Inject `*sdkmetric.MeterProvider` for
`ForceFlush` or `Shutdown`.

## SDK views and tooling

SDK views see the exported name, not the name passed to the constructor. A view
that matches on an instrument name must use the exported name.
`metrics.PrefixedName` computes it, and dashboard generators can use it the
same way:

```go
name := metrics.PrefixedName("cache.size", "formance.ledger") // formance.ledger.cache.size
view := sdkmetric.NewView(sdkmetric.Instrument{Name: name}, sdkmetric.Stream{ /* ... */ })
```

To prefix a provider outside the fx module, wrap it with
`metrics.NewPrefixedMeterProvider(provider, prefix)`. It validates the prefix
and returns the provider unchanged when there is none.
