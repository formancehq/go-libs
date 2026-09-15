# Logging

Every service builds one logger and exposes it through whichever interface each
dependency expects. All of them render the same record, so a deployment running
several services has one shape to parse rather than one per binary.

```json
{"level":"INFO","time":"2026-09-15T12:40:36.917268302Z","msg":"batch applied","source":"bitcoin"}
```

Field order is fixed: `level`, `time`, the logger name when there is one, `msg`,
then the record's own fields.

## The two stacks

| Constructor | Backend | Use |
| --- | --- | --- |
| `NewZapLogger(w, level, json)` | zap | the current stack — expose it with the adapters below |
| `NewDefaultLogger(w, debug, json, otel)` | logrus | what `pkg/service` gives a service that has not migrated |

Both emit the record above. The zap stack is the direction: it is what
`pkg/observe/log` builds on, and only it offers the slog and logr interfaces.

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

## Compatibility — the JSON output of `NewDefaultLogger` changed

The logrus stack used to emit logrus's own shape. It now emits the shared one.
A deployment querying those logs has to migrate:

| | before | after |
| --- | --- | --- |
| level | `"info"` | `"INFO"` |
| warning level | `"warning"` | `"WARN"` |
| timestamp | second precision | RFC 3339 with nanoseconds |
| key order | `level`, `msg`, `time` (alphabetical) | `level`, `time`, `msg` |

**What to update**: any query, dashboard, alert or log-based metric filtering on
a lowercase level (`level="info"`, `level="error"`) or on `level="warning"`.
Parsers reading the fields by name and ignoring order need no change, and so do
consumers of the text format, which is untouched.

Two behaviours are carried over from `logrus.JSONFormatter` deliberately: a
field holding an `error` renders as its message rather than as `{}`, and a
field whose key collides with `level`, `time` or `msg` is emitted as
`fields.<key>` so the record's own value is not shadowed.

## Levels

`Level` is `trace|debug|info|error` — it has no warn. `ParseZapLevel` does
accept `warn`, since zap has a real warn level; a caller going through `Level`
still has to round it.

`ZapLevelFromFlags(logLevel, debug)` clamps the level to at least Debug when
`--debug` is set, matching what the default logger does with that flag.
