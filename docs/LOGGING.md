# Logging

A service builds one logger and exposes it through whichever interface each
dependency expects. Every interface over one logger renders the same record, so
a service emits one shape rather than one per dependency:

```json
{"level":"INFO","time":"2026-09-15T12:40:36.917268302Z","msg":"batch applied","source":"bitcoin"}
```

Field order is fixed: `level`, `time`, the logger name when there is one, `msg`,
then the record's own fields.

A deployment reaches **one shape across services** as each service moves onto
this stack. Nothing changes for one that has not — see [Compatibility](#compatibility).

## The two stacks

| Constructor | Backend | Use |
| --- | --- | --- |
| `NewZapLogger(w, level, json)` | zap | the current stack — expose it with the adapters below |
| `NewDefaultLogger(w, debug, json, otel)` | logrus | what `pkg/service` gives a service that has not migrated |

The zap stack emits the record above and is the direction: it is what this
package builds on, and only it offers the slog and logr interfaces. The logrus
stack keeps its own shape unless a caller opts in — see Compatibility below.

### Adapters over one `*zap.Logger`

| Interface | Constructor | Consumers |
| --- | --- | --- |
| `Logger` | `NewZap(z.Sugar())` | lifecycle, existing consumers |
| `Logger` | `NewSlogLogger(NewSlog(z))` | same, but trace-correlated |
| `*slog.Logger` | `NewSlog(z)` | standard-library call sites |
| `logr.Logger` | `NewLogr(z)` | controller-runtime, klog |

`NewSlogLogger` carries a context, which is what `ZapLogger` gives up — its
`WithContext` returns itself. That is what makes `ContextWithLogger`, and the
HTTP middleware built on it, produce records stamped with the active span. The
cost is that `zapslog` clamps every slog level below Info to Debug, so `Trace`
arrives at `Debug`; use `NewZap` where the custom trace level matters more than
correlation.

## Compatibility

Nothing an existing consumer sees changes. `NewDefaultLogger` and
`NewDefaultLoggerWithLevel` keep logrus's own JSON shape — lowercase levels,
second-precision timestamps, alphabetically ordered keys — because changing it
would rewrite records a deployment's queries and dashboards are already built
on. A test pins that output against a bare `logrus.JSONFormatter`.

The shared shape is therefore reached by **moving to the zap stack**, one
service at a time. A service that has to stay on logrus for now can opt into
the shape alone:

```go
l := logrus.New()
l.SetFormatter(logging.NewSharedJSONFormatter())
logger := logging.NewLogrus(l)
```

Either move changes that service's records as follows, which is what to check
before making it:

| | logrus default | shared shape |
| --- | --- | --- |
| level | `"info"` | `"INFO"` |
| warning level | `"warning"` | `"WARN"` |
| timestamp | second precision | RFC 3339 with nanoseconds |
| key order | `level`, `msg`, `time` | `level`, `time`, `msg` |

**What to update when you migrate a service**: any query, dashboard, alert or
log-based metric filtering on a lowercase level (`level="info"`) or on
`level="warning"`. Parsers reading fields by name and ignoring order need no
change, and the text format is untouched either way.

`NewSharedJSONFormatter` carries over two behaviours from
`logrus.JSONFormatter` deliberately: a field holding an `error` renders as its
message rather than as `{}`, and a field whose key collides with `level`,
`time` or `msg` is emitted as `fields.<key>` so the record's own value is not
shadowed.

## Levels

`Level` is `trace|debug|info|error` — it has no warn. `ParseZapLevel` does
accept `warn`, since zap has a real warn level; a caller going through `Level`
still has to round it.

`ZapLevelFromFlags(logLevel, debug)` clamps the level to at least Debug when
`--debug` is set, matching what the default logger does with that flag.
