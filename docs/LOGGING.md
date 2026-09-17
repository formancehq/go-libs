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
`NewDefaultLoggerWithLevel` keep logrus's own formatters in **both** formats —
JSON with lowercase levels, second-precision timestamps and alphabetically
ordered keys, and text as `key="value"` pairs — because changing either would
rewrite the output of every service that has not migrated. A test pins both
against bare `logrus.JSONFormatter` and `logrus.TextFormatter`.

The shared shape is therefore reached by **moving to the zap stack**, one
service at a time. A service that has to stay on logrus for now can opt into
the shape alone, in either format:

```go
l := logrus.New()
l.SetFormatter(logging.NewSharedJSONFormatter()) // or NewSharedTextFormatter
logger := logging.NewLogrus(l)
```

The two moves are not equivalent, and the difference matters for text logs.

### JSON — both moves change it the same way

| | logrus default | shared shape |
| --- | --- | --- |
| level | `"info"` | `"INFO"` |
| warning level | `"warning"` | `"WARN"` |
| timestamp | second precision | RFC 3339 with nanoseconds |
| key order | `level`, `msg`, `time` | `level`, `time`, `msg` |

**What to update**: any query, dashboard, alert or log-based metric filtering on
a lowercase level (`level="info"`) or on `level="warning"`. Field *names* are
unchanged, so a parser reading by name and ignoring order keeps working — but
see the two subsections below before assuming a typed parser does.

### Values — some types render differently

The shared shape encodes values through zap, whose `Any` prefers `fmt.Stringer`
over reflection. A type implementing both `Stringer` and JSON marshalling is
rendered by its `String()`:

| value | logrus default | shared shape |
| --- | --- | --- |
| `json.Number("42")` | `42` | `"42"` |
| any `Stringer` that also marshals | its JSON form | its `String()` |
| `int`, `float64`, `bool` | unchanged | unchanged |
| `time.Duration` | nanoseconds | nanoseconds |
| `error` | its message | its message |

A **typed** parser or a numeric query over such a field has to be checked. Both
shared paths and the zap stack agree on this, by construction — it is the
encoder's behaviour, not an adaptation — and a test pins it.

### Fields colliding with a reserved key

A field named `level`, `time`, `msg` or `logger` is emitted as `fields.<key>`,
so the record's own value is not shadowed. `logrus`'s default did it for
`level`, `time` and `msg` only, so a record that used to read `fields.msg` still
does, and a field named `logger` gains the same protection.

The escaping lives in a `zapcore.Core`, so it applies to **every** façade over a
logger — `NewSlog`, `NewLogr`, `NewZap` and any direct zap use — rather than to
whichever adapter implemented it. `NewSlog` additionally escapes `trace_id` and
`span_id`, since it is what injects them; it does so whether or not a span is
active, so a field's path does not depend on whether the request was traced.

A field carrying both a reserved key and its escaped form (`msg` and
`fields.msg`) resolves in favour of the reserved one, deterministically, on
every path. Attributes inside a `WithGroup` are namespaced and keep their own
names.

### Text — unified too

`NewSharedTextFormatter` is the console counterpart: a service opting into it
renders text exactly as the zap stack does.

```
2026-09-16T17:16:01.187482+02:00	INFO	listening	{"addr": ":8080"}
```

Tab-separated positional fields and a JSON object for the attributes, instead of
logrus's `key="value"` pairs. Anything parsing the text output — a local `grep`,
a log shipper reading the non-JSON format, a test asserting on a line — has to
be revisited when a service adopts it. In practice text is the development
default and JSON is what a deployment runs, so this usually costs a few test
assertions rather than a dashboard.

A service migrating to the zap stack gets both changes at once; one opting into
a single shared formatter changes only that format.

### What is guaranteed, and what is not

Both shared formatters delegate to zap's own encoders rather than reproducing
their layout, so the two stacks *encode* through one encoder: level spelling,
timestamp precision, field escaping and value conversion cannot drift apart.
The tests assert on the rendered bytes of both stacks, in both formats.

**Field order is not guaranteed between the two.** It follows the source. slog
and zap carry a call order and keep it, so `Info("x", "b", 2, "a", 1)` renders
`b` before `a`. logrus holds its fields in a map and has no order to preserve,
so the shared formatter sorts them to stay deterministic across runs. The two
therefore agree only when the call order happens to be the sorted one.

Nothing should depend on field order in JSON, and nothing in this package emits
records where it carries meaning — but the guarantee is "the same encoding",
not "the same bytes for any input".

## Levels

`Level` is `trace|debug|info|error` — it has no warn. `ParseZapLevel` does
accept `warn`, since zap has a real warn level; a caller going through `Level`
still has to round it.

`ZapLevelFromFlags(logLevel, debug)` clamps the level to at least Debug when
`--debug` is set, matching what the default logger does with that flag.
