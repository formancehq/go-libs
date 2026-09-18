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

## Wiring it

Everything that shapes the output — where records go, at which level, in which
format — is decided once, on the `*zap.Logger`. The adapters choose the
interface a dependency expects, and whether records are correlated. Nothing
else.

```go
level := logging.ZapLevelFromFlags(logLevel, debug)          // --log-level, --debug
json  := jsonFormatting                                      // JSON_FORMATTING_LOGGER
otel  := otelTracesExporter != ""                            // a traces exporter is configured

z := logging.NewZapLogger(os.Stderr, level, json)            // the one logger

appLogger  := logging.NewZapWithTraces(z.Sugar())            // Logger, correlated
ctrlLogger := logging.NewLogr(z)                             // logr.Logger
```

| Knob | Where | Value |
| --- | --- | --- |
| destination | `NewZapLogger(w, …)` | usually `os.Stderr` |
| level | `NewZapLogger(…, level, …)` | `ParseZapLevel("info")`, or `ZapLevelFromFlags(logLevel, debug)` which clamps to Debug when `--debug` is set |
| JSON or text | `NewZapLogger(…, jsonFormatting)` | `JSON_FORMATTING_LOGGER` / `--json-formatting-logger` |
| correlation | the adapter | see [Trace correlation](#trace-correlation) |

Levels accept `trace`, `debug`, `info`, `warn` and `error`. `warn` is a real
level here, which `Level` cannot express — a caller going through `Level` still
has to round it.

Changing the level or the format on the `*zap.Logger` changes it for every
adapter over it, which is the point: a service has one knob per property, not
one per interface.

## The two stacks

| Constructor | Backend | Use |
| --- | --- | --- |
| `NewZapLogger(w, level, json)` | zap | the current stack — expose it with the adapters below |
| `NewDefaultLogger(w, debug, json, otel)` | logrus | what `pkg/service` gives a service that has not migrated |

The zap stack emits the record above and is the direction: it is what this
package builds on, and only it offers the logr interface. The logrus
stack keeps its own shape unless a caller opts in — see Compatibility below.

### Adapters over one `*zap.Logger`

| Interface | Constructor | Consumers |
| --- | --- | --- |
| `Logger` | `NewZap(z.Sugar())` | lifecycle, existing consumers |
| `Logger` | `NewZapWithTraces(z.Sugar())` | same, but trace-correlated |
| `logr.Logger` | `NewLogr(z)` | controller-runtime, klog |

`Logger` is the interface to write application code against. It carries the
custom `Trace` level, which nothing above `Debug` in the standard hierarchy can
express.

## Trace correlation

Correlation is **attached deliberately**, not implied by holding a context.
That is how the logrus stack has always worked: `WithContext` stores the
context, and `SetHooks` attaches the hook that reads it — only when a traces
exporter is configured, since stamping ids for a trace no backend will receive
correlates a record with nothing.

| Constructor | Correlates |
| --- | --- |
| `NewDefaultLogger(w, debug, json, otelTraces)` | when `otelTraces` is set |
| `NewZap(sugar)` | no — `WithContext` returns the receiver |
| `NewZapWithTraces(sugar)` | yes |

`NewZapWithTraces` belongs at the same place in a service's wiring as the
logrus hook: alongside a configured traces exporter, on the same condition.

It is a constructor rather than a core because a `zapcore.Core` sees no
context — which is what the original "attach an otelzap core" note meant,
`otelzap` being the bridge that carries one. Taking the context through
`Logger.WithContext` reaches the same result without the dependency.

All three stacks take the condition from the same place — whether the service
has a traces exporter configured, which `pkg/service` derives from
`otlptraces.OtelTracesExporterFlag`:

```go
otelTraces, _ := cmd.Flags().GetString(otlptraces.OtelTracesExporterFlag)

sugar := z.Sugar()
logger := logging.NewZap(sugar)
if otelTraces != "" {
	logger = logging.NewZapWithTraces(sugar)
}
```

`NewZap` keeps its signature — it predates this — so `NewZapWithTraces` is the
opt-in form rather than a parameter on `NewZap`, which would have been a
breaking change for every existing caller. `NewZapCorrelatedIf(sugar, correlate)`
selects between the two, so a service with several entrypoints decides the
condition once rather than repeating the branch:

```go
logger := logging.NewZapCorrelatedIf(z.Sugar(), traces.Enabled(cmd.Flags()))
```

Both stamp on a **valid span context**, which includes one propagated from
another service. The logrus hook stamps only for a **recording** span, so a
context carrying a remote or sampled-out span is correlated on the new stack
and not on the logrus one. Worth knowing when comparing records emitted by two
services on different stacks.

## Compatibility

**No record changes shape.** `NewDefaultLogger` and
`NewDefaultLoggerWithLevel` keep logrus's own formatters in **both** formats —
JSON with lowercase levels, second-precision timestamps and alphabetically
ordered keys, and text as `key="value"` pairs — because changing either would
rewrite the output of every service that has not migrated. A test pins both
against bare `logrus.JSONFormatter` and `logrus.TextFormatter`.

**One source-level change, for implementors only.** `Logger` gains `Warn` and
`Warnf`, so an external type implementing the interface stops satisfying it
until it grows them; callers are unaffected. The implementations are not new --
`ZapLogger` and the logrus adapter already had both methods -- this exposes
them on the interface. [#608](https://github.com/formancehq/go-libs/pull/608)
did the same for `Trace`, in a minor release.

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
logger — `NewLogr`, `NewZap` and any direct zap use — rather than to whichever
adapter implemented it.

`trace_id` and `span_id` are escaped the same way, on **every** zap-stack path
and on the shared logrus formatters — whether the field is scoped with `With`
or written on the record (`Infow`, zapr's `WithValues`), and whether or not the
logger correlates. Escaping only where correlation is on would make a field's
path depend on whether the service configured a traces exporter, and on whether
the request happened to be sampled.

The one exception is the pair a correlating logger stamps itself, which reaches
the record root. It is marked as it is stamped, so the core can tell it from an
application field using the same key — without that marker the core would have
to escape both, losing the correlation, or neither, letting an application
field shadow the span.

**A namespace swallows the correlation.** If a caller opens a `zap.Namespace`
on the underlying logger, everything written afterwards nests under it — the
stamped pair included, so the record carries `request.trace_id` rather than a
root `trace_id`, and a query keying on the root misses it. The namespace is
opened by the encoder while it writes the scoped fields, and a core layered
above cannot reach back out to the record root, so this is a limitation rather
than an oversight. `Logger.WithField`/`WithFields` never open one; only direct
`zap.Namespace` use does.

**One ordering emits a duplicate.** A literal `fields.msg` scoped with `With`,
followed by a record-level `msg`, produces `fields.msg` twice: the scoped field
is already encoded by the inner core by the time the record is renamed, and
nothing can retract it. The order is deterministic rather than arbitrary — the
escaped reserved value is always written last, so a decoder keeping the last
value (the JSON norm, and what `encoding/json` does) reads the intended one.
The reverse order, and both keys in the same call, resolve to a single field.

A field carrying both a reserved key and its escaped form (`msg` and
`fields.msg`) resolves in favour of the reserved one, deterministically, on
every path. Attributes under a `zap.Namespace` are nested and keep their own
names — they cannot collide with a key at the record root, so they are not
escaped.

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

**Field order is not guaranteed between the two.** It follows the source. zap
carries a call order and keeps it, so `WithField("b", 2).WithField("a", 1)`
renders `b` before `a`. logrus holds its fields in a map and has no order to preserve,
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

**An unrecognised or empty value falls back to Info, silently.** A mistyped
`--log-level=erro` therefore changes verbosity without saying so. That is the
deliberate trade — a typo in a log knob should not crash-loop a pod — but it is
worth knowing when a level does not take effect. `ParseLevel` returns an error
instead, for a caller that would rather refuse to start.
