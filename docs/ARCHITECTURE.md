# Architecture

## Overview

`go-libs` is a collection of shared Go libraries used across Formance services.
The project is organized by **functional domain** (bounded contexts), with a strict
separation between pure logic and dependency injection wiring.

## Directory Structure

```
go-libs/
├── docs/                            # Project documentation
│
└── pkg/                             # All Go packages
    ├── types/                       # Leaf packages (zero internal dependency)
    │   ├── pointer/                 #   Pointer creation/dereference helpers
    │   ├── time/                    #   Time wrapper with JSON serialization
    │   ├── currency/                #   Currency types and formatting
    │   ├── metadata/                #   Generic metadata map
    │   └── collections/             #   Generic slice, map, linked list utilities
    │
    ├── errors/                      # Error helpers (exit codes, wrapping)
    ├── query/                       # Query expression builder
    │
    ├── observe/                     # Observability
    │   ├── log/                     #   Logger interface + adapters (zap, logr, logrus) -- see docs/LOGGING.md
    │   ├── traces/                  #   Tracer provider (OTLP gRPC/HTTP, stdout)
    │   ├── metrics/                 #   Meter provider (OTLP, in-memory), namespace prefix -- see docs/METRICS.md
    │   ├── payloadlog/              #   Opt-in bounded reassemblable payload capture -- see docs/PAYLOAD-CAPTURE.md
    │   ├── resource.go              #   Shared OTLP resource builder
    │   └── profiling/               #   pprof debug server
    │
    ├── transport/                   # Network I/O
    │   ├── serverport/              #   Server address discovery and context binding
    │   ├── httpserver/              #   HTTP server (chi, middlewares, OTEL)
    │   ├── grpcserver/              #   gRPC server
    │   ├── httpclient/              #   HTTP client debug/tracing, retry, rate limiting
    │   ├── webhook/                 #   Bounded inbound webhook receiver (verify, then deliver)
    │   └── api/                     #   Response formatting, pagination, idempotency
    │
    ├── authn/                       # Authentication & authorization
    │   ├── jwt/                     #   JWT validation, keyset, middleware
    │   ├── oidc/                    #   OpenID Connect provider/client
    │   └── licence/                 #   Licence JWT validation
    │
    ├── storage/                     # Persistence
    │   ├── postgres/                #   PostgreSQL error mapping
    │   ├── bun/                     #   Bun ORM helpers
    │   │   ├── connect/             #     Connection setup
    │   │   ├── paginate/            #     Cursor/offset pagination
    │   │   ├── migrate/             #     Bun migrations
    │   │   ├── debug/               #     Query debug hook
    │   │   └── explain/             #     EXPLAIN hook
    │   ├── migrations/              #   Generic migration framework
    │   └── s3/                      #   S3 bucket helpers
    │
    ├── messaging/                   # Async messaging
    │   ├── publish/                 #   Multi-backend publisher (kafka, nats, sns, http)
    │   │   ├── circuit/             #     Circuit breaker
    │   │   └── topicmap/            #     Topic mapping decorator
    │   └── queue/                   #   Listener/consumer
    │
    ├── workflow/                    # Orchestration
    │   └── temporal/                #   Temporal client, worker, encryption
    │
    ├── cloud/                       # Cloud provider integrations
    │   └── aws/                     #   IAM credential loading
    │
    ├── service/                     # Service bootstrap
    │   ├── app.go                   #   Config, run loop, graceful shutdown
    │   └── health/                  #   Health check controller
    │
    ├── fx/                          # FX wiring (imports pure packages, wraps in fx.Module)
    │   ├── observefx/               #   Modules for observe/*
    │   ├── transportfx/             #   Modules for transport/*
    │   ├── authnfx/                 #   Modules for authn/*
    │   ├── storagefx/               #   Modules for storage/*
    │   ├── messagingfx/             #   Modules for messaging/*
    │   ├── workflowfx/              #   Modules for workflow/*
    │   ├── cloudfx/                 #   Modules for cloud/*
    │   └── servicefx/               #   Top-level service assembly
    │
    └── testing/                     # Test infrastructure
        ├── testservice/             #   Service scaffold for integration tests
        ├── docker/                  #   Container pool management
        ├── platform/                #   Database/broker containers (pg, nats, clickhouse...)
        └── api/                     #   HTTP assertion helpers
```

## Rules

### 1. Dependency layers

Packages are organized in strict layers. Dependencies flow **upward only** (a lower
layer never imports a higher layer).

```
Layer 0 (Leaf)    : pkg/types/  pkg/query/  pkg/errors/
Layer 1 (Core)    : pkg/observe/  pkg/storage/  pkg/authn/
Layer 2 (Infra)   : pkg/transport/  pkg/messaging/  pkg/workflow/  pkg/cloud/
Layer 3 (Bootstrap): pkg/service/
Layer 4 (Wiring)  : pkg/fx/
Layer 5 (Test)    : pkg/testing/
```

**A package may only import from its own layer or a lower layer.**

### 2. FX isolation

FX is **plumbing only**. The `fx/` directory is the sole consumer of `go.uber.org/fx`.

- Pure packages (`observe/`, `transport/`, etc.) **must not** import `go.uber.org/fx`.
- All `fx.Module`, `fx.Provide`, `fx.Invoke` calls live under `fx/`.
- Every feature must be usable with plain constructor calls, without FX.

Example:

```go
// pkg/observe/traces/provider.go — PURE
package traces

func NewTracerProvider(cfg Config, res *resource.Resource) (*sdktrace.TracerProvider, error) {
    // ...
}
```

```go
// pkg/fx/observefx/traces.go — WIRING
package observefx

func TracesModule() fx.Option {
    return fx.Module("traces",
        fx.Provide(traces.NewTracerProvider),
        fx.Invoke(func(lc fx.Lifecycle, tp *sdktrace.TracerProvider) {
            lc.Append(fx.Hook{
                OnStop: func(ctx context.Context) error {
                    return tp.Shutdown(ctx)
                },
            })
        }),
    )
}
```

### 3. Leaf packages have zero internal dependencies

Everything under `types/`, `query/`, and `errors/` must not import any other
`go-libs` package. They may only depend on the standard library or minimal
external dependencies.

### 4. No `*utils` naming

Package names should be descriptive nouns, not suffixed with `utils`.
Use `collections`, not `collectionutils`. Use `errors`, not `errorsutils`.

### 5. CLI flags

Packages that need CLI configuration expose an `AddFlags(*pflag.FlagSet)` function.
The `fx/` layer or `service/` calls these during setup. The pure package itself
never imports cobra or interacts with CLI frameworks directly beyond pflag.

`*cobra.Command` is only accepted in `service/`, `fx/`, and CLI builder packages
(e.g. `storage/bun/migrate/`). Pure packages accept `*pflag.FlagSet` and
`context.Context` instead.

### 6. Testing packages

`testing/` is a top-level directory for test infrastructure. Test files (`*_test.go`)
within each package are fine and encouraged. The `testing/` directory is for shared
test helpers and integration test scaffolding.

## Adding a new package

1. Determine which **layer** your package belongs to.
2. Place it under the correct domain directory.
3. Write pure constructors and functions — no FX.
4. If FX wiring is needed, add a module under `pkg/fx/<domain>fx/`.
5. Update this document if adding a new domain directory.

## FX isolation status

All packages comply with the FX isolation rule. The only exception is
`service/app.go` which is the bootstrap layer and uses FX by design.

## Module path

The module path is `github.com/formancehq/go-libs/v5`.
This is a breaking change from v4 to enforce the new structure.

## Exit codes

`service.App.Run` never exits the process. A start failure (a constructor or an
`OnStart` hook returned an error) comes back as `service.ErrStartFailed`
wrapping the cause with `%w`, so its message and any exit code it carries
(`errors.NewErrorWithExitCode`) stay in the chain. An exit code requested
through `fx.Shutdowner` with `fx.ExitCode` comes back as
`service.ErrShutdownExitCode` carrying that code.
`service.Execute` exits with that code, and with 1 for any other error.
A service calling `cmd.Execute` itself must do the same, for example with
`errors.ExitCodeFromError`, to keep its exit codes.

## Service shutdown budget

`service.App.Run` applies `--total-stop-timeout` to the complete Fx stop
operation. Every `OnStop` hook receives the same deadline, including the
`--grace-period` hook. Shutdown uses a fresh context so cancellation of the
command context does not cancel cleanup immediately; logger and lifecycle
context values are preserved.

The default total budget is 25 seconds. This accommodates the standard
five-second routing grace period while leaving up to 20 seconds for ordered
cleanup across all hooks and five seconds for the process to exit before
Kubernetes' default 30-second termination window expires. Deployments with a
different supervisor window must keep it longer than the application budget.
In particular, the Formance operator gives liveness-probe failures a 10-second
termination grace period, so the process may be killed before the default
25-second application budget expires on that path.

Set `--total-stop-timeout=0` to disable the deadline and allow all stop hooks to
run without a total time limit.

If the budget expires, `Run` returns a shutdown error. Cleanup may be incomplete:
Fx can return before a hook that ignores context cancellation has finished.
Callers must handle the error and separately bound any cleanup they perform
after `Run` returns. Consumers upgrading from an unbounded runner should size
their total timeout to include the grace period and all stop hooks.

The `grpcserver` stop hook drains in-flight RPCs until the deadline, then
closes every transport, and returns only once `Serve` and every handler have
returned, even when its context has already expired. A handler that ignores
cancellation therefore holds the hook past the deadline; Fx still returns at
the budget.


A non-nil `grpcserver.WithServeErrorHandler` callback opts into withdrawing the
configured health server before reporting an unexpected gRPC serving failure.
Withdrawal affects every named service on that probe, including services sharing
it with another hook. The callback can request application shutdown; it must
return promptly and must not wait for `OnStop`, because shutdown joins the serving
goroutine. Without a handler, including an explicitly nil handler, serving
failures are logged and health remains under the existing lifecycle and caller
control. `OnStop` still withdraws the configured probe before draining. Normal
shutdown does not invoke the callback. Listener admission failures still return
from `OnStart`.

## Native continuous profiling

[PYROSCOPE.md](./PYROSCOPE.md) describes the optional native SDK configuration and
plain lifecycle hook. `pkg/fx/observefx.PyroscopeModule` composes it with existing
trace correlation; pprof HTTP profiling remains separate.

## Bounded read-only object access

`pkg/storage/object` owns bounded object listing, metadata and conditional reads
for composed S3/Azure clients and rooted local directories. Credential and CLI
configuration stays at composition. See [object storage](OBJECT_STORAGE.md) for
limits, generation and enumeration semantics. AWS IAM also exposes a flag-free
`Options`/`LoadConfig` API; its existing flag API remains compatible.

## WebSocket client transport

`pkg/transport/websocketclient` supplies a minimal `Conn`, `Dialer` and
`NewDialer(scope)` backed by `github.com/coder/websocket v1.8.15`. It injects
OpenTelemetry propagation headers into the handshake, bounds inbound messages
(including fragmented messages) to `ReadLimit` (32 MiB), writes text messages,
and exposes context-aware read, write and ping with normal close semantics.
It does not log URLs, headers or frames.

The constructor creates `wsx.dial.attempts` and `wsx.dial.duration` instruments
once, recording only the closed `outcome=ok|error` dimension. Explicit scopes
are preserved; an empty scope uses the package's public `DefaultMetricsScope`.
Instrumentation failure never prevents a connection attempt. These metric
names remain stable independently of the package name.

Protocol authentication frames, subscriptions, reconnect policy, watchdogs,
queues, message interpretation and payload capture are caller responsibilities.
There is no FX, CLI, provider or storage dependency in this package.

### WebSocket session supervision

`pkg/transport/websocketclient.Supervisor` owns one reconnecting transport
session: bounded raw-frame admission, ping, confirmation timeout, activity
watchdog, stable-connection retry reset and cancellation. Callers supply Open
and HandleFrame plus lifecycle callbacks. Protocol setup, recovery and decoded
event queues remain caller-owned. HandleFrame returns FrameResult to report
confirmation, activity or requested reconnect; it does not transfer decoding or
provider authority into the transport.

SupervisorMonitor retains coherent physical frame accounting and instruments
its owned measurements through the application OTel meter provider. FrameJob consumes an existing observe/job descriptor; opening a socket
never runs under a frame job. Byte admission uses the existing weighted semaphore
with 64 KiB charges and a separate item bound. No generic worker/event-queue
framework or second connection interface is introduced.

### Bounded decoded WebSocket work

`websocketclient.EventQueue` owns non-blocking bounded FIFO admission, close/drain,
reset discard and coherent queue accounting. Callers select drop or coalesce for
item saturation; encoded-byte rejection is always a drop. `KeyedBuffer` owns
insertion ordering, replacement-in-place, bounded oldest eviction and drains.
Both retain encoded-payload bounds without claiming total heap measurement.
`Event.Origin` and keyed origins use immutable OpenTelemetry span contexts.

Generic correlation uses `observe/traces.AddLinks` for valid deduplicated origins
and `RecordSpanError` for no-stacktrace error/status recording. Application span
names/attributes, root selection, capture policy, provider recovery and generation
fences remain with callers. Broker listeners and a linked list do not supply these
bounded in-process semantics; no second generic framework is introduced.
### WebSocket connection ownership

Keep one reader active to service control frames and run Ping concurrently.
An expired or cancelled active Read closes the underlying connection; reconnect
instead of treating it as a recoverable per-read timeout. A reader paused for
local admission cannot service pongs, so callers must coordinate heartbeat
failure policy with backpressure. Close ends the connection and releases the
read loop; the owner cancels and joins its work before starting another session.

### Supervisor failure and memory contracts

Only a confirmed established session contributes to the stable-session retry reset;
failed setup and reconnect callbacks do not. Retry logs include a fixed message,
attempt and error type, never arbitrary setup/URL/error text. Callers that need
additional diagnostics retain their own credential-safe logging boundary.

Frame callbacks remain serial but must return when their context is cancelled.
Read/ping failures and confirmation/watchdog expiry can cancel a cooperative
callback independently; callback, reader and heartbeat work is joined before retry.
A non-cooperative callback can still block shutdown and is outside this contract.
Local reader admission waits suspend heartbeat deadlines and provider-activity
elapsed time; resuming admission gives the heartbeat its timeout again. This does
not cancel an in-progress control write or classify local overload as peer failure.

FrameQueueBytes bounds the admitted queue only. One complete frame returned by
Read may wait for admission outside that budget, and one admitted frame can be in
the active handler. For the bundled connection, additional payload allowance is
at most ReadLimit plus min(FrameQueueBytes, ReadLimit), beyond the admitted queue.
Custom connections must enforce their own read limit. This is not a total heap
bound. Item/byte admission and chunk charges themselves are unchanged.

### OTel session measurements

NewSupervisorMonitor accepts WithSupervisorMeterProvider and WithSupervisorName.
Use the application's injected provider for prefix/view policy; nil uses the global
provider. No SDK provider or recorder is constructed. Stage/channel dimensions are
fixed; names must be bounded static roles, never tenant/account/URL/error values.
Counters aggregate monitors sharing dimensions; last-value gauges describe the
last observed session for those dimensions, so concurrent sessions need distinct
bounded names when their individual values must be displayed.

Frames read/handled/abandoned and bytes are counters, handling durations are seconds
histograms, and owned depth/in-flight/high-water values are explicit gauges. Raw
Snapshot is operational state for lifecycle/conservation checks, not a metric
collection or rolling/percentile interface. Local views collect from the same SDK
provider using ForceFlush then the existing in-memory exporter, or an application
ManualReader. Exporter ForceFlush alone does not collect. Collection failure,
freshness and unavailable-versus-observed-zero handling belong to the local view.
Histogram estimates and collected intervals must not be labelled as exact retained
sample percentiles or the withdrawn recorder's exact 10/60-second windows.

### Recorder-free collection measurements

EventQueueSnapshot.Queue is native QueueStats, owned by the bounded collection:
availability, physical depth/capacity/bytes, operation counts and oldest age. It
has no stage schema, recorder or performance-package dependency. Its semantics
remain queue state, not metric collection or a replacement diagnostic API.

Collection constructors accept WithCollectionMeterProvider and WithCollectionName.
They create no SDK provider or reader. Fixed stage/channel dimensions identify
WebSocket events/buffers; names are bounded declared roles. Counters use item units
and monotonic native-operation deltas, gauges expose depth/capacity, retained bytes,
byte capacity, high water and the oldest enqueue's Unix nanosecond timestamp (zero
when empty). The timestamp is an owned observation from which a local display can
compute current residence age, not an unrefreshed age gauge or inferred provider
latency. Constructed empty collections explicitly record zero. Shared dimensions
aggregate counters; gauges are last-value observations, so use distinct bounded
names for individually displayed concurrent collections.

All metrics use the same application provider and its injected prefix/views. A
local exporter snapshot requires SDK-provider ForceFlush, not exporter ForceFlush.
The collecting application owns deadlines, freshness/error/unavailable state and
display models. No exact 10/60-second windows or nearest-rank samples are restored.
Admission, byte accounting, coalesce/drop outcomes, replacement age/order, close/
drain and reset behavior remain independent of telemetry; instrumentation failure
disables measurements rather than changing transport or collection outcomes.

## Payload capture policy

[PAYLOAD-CAPTURE.md](./PAYLOAD-CAPTURE.md) separates capture mechanics from caller-owned sanitization.

### Webhook receiver consumer policies

`pkg/transport/webhook.Config` optionally accepts additional method handlers,
response rendering, delivery-error status classification, bounded-body observation
and request-context preparation. The existing HEAD/POST gate, verification order,
503 delivery failures and response bytes remain the defaults. Method extensions
cannot override HEAD or POST, and delivery-error classification cannot accept a
failed delivery with a successful status. The observer sees at most MaxBody+1
bytes and never bypasses body admission or verification. Capture and redaction
remain the observer's responsibility. Middleware can retain span lifecycle
ownership with `UseRequestSpan`, preventing duplicate delivery spans.

## Webhook hook admission

The optional body observer receives the bounded window, including one overrun
byte, while `http.MaxBytesReader` retains HTTP/1 oversize/close signaling. An
overrun wins over a simultaneous read error: verification and delivery do not
run, and the response is 413 without waiting for a missing body remainder.
Default 405 responses include configured additional methods after HEAD and POST
in deterministic order. With no hooks, the original method gate is unchanged.

## HTTP hook shutdown and serving failures

`pkg/transport/httpserver.NewHook` accepts `WithCloseOnShutdownError()` to close
active connections when graceful shutdown fails. `OnStop` returns the original
shutdown error. Without this option, the existing graceful-only behavior stays
unchanged. Hijacked connections remain the caller's responsibility, as with
`net/http.Server.Close`.

`WithServeErrorHandler(func(error))` observes unexpected Serve failures after
logging. Normal shutdown and listen failures do not invoke it. The callback
runs on the serving goroutine and must return promptly without waiting for
`OnStop`; an application can request its own lifecycle shutdown there.

The shared `serverport.Server.Listen` also honors startup cancellation. It uses
`net.ListenConfig.Listen`, refuses an already-cancelled context before opening
or advertising a socket, and releases a newly-created listener when cancellation
is observed before publication. A supplied listener remains caller-owned when
startup is refused. Both HTTP and gRPC hooks consume this same boundary.

### Startup cancellation compatibility

The listener cancellation correction is unconditional, including when both new
HTTP options are omitted. Previously an already-cancelled startup context could
still admit a supplied listener. Startup now returns the context error before
admission, does not publish readiness, and does not transfer listener ownership.
`OnStop` after that failed HTTP or gRPC start leaves the supplied listener open;
the caller must close it. After successful admission, normal lifecycle shutdown
continues to close the listener. Newly created sockets are closed if cancellation
is observed before publication.

This differs from the old cancelled-start behavior. The default graceful HTTP
shutdown policy is unchanged; that does not imply all startup behavior is
unchanged. Consumers must qualify failed-start cleanup and ownership before
adoption. The correction's compatibility classification requires maintainer
review; the default-off HTTP options alone do not establish same-major
compatibility for this shared listener change.

## Token refresh diagnostics

`transport/httpclient.BearerRefreshFailureFields(err)` produces the bounded
`error_kind` vocabulary used by bearer recovery logs. Context deadline and
cancellation take precedence. Token endpoint errors may expose
`HTTPStatusCode() int`; `oauth2.RetrieveError` remains supported. The result
contains only the error kind and optional integer `token_endpoint_status`.
The classifier never renders error messages, response bodies, URLs or tokens.
It reports diagnostics only: it does not authorize replay, select retry policy,
or turn a status into a permanent credential verdict. Transport timeout-capable
errors and invalid-token responses retain their existing categories.


## Completed HTTP attempt diagnostics

`httpclient.RetryConfig.OnAttempt` receives each completed wire attempt before
retry-policy evaluation and limiter feedback. It receives the actual attempt
request context and 1-based wire count, including bearer recovery. It does not
grant retries, change signing order, or render errors. Observers own admission
and must preserve request/response bytes and errors and respect cancellation.
HTTP 101 response bodies remain untouched by snapshotting, preserving their
bidirectional stream and close ownership.

`SnapshotResponseBody` provides a bounded window plus an over-limit sentinel
byte while restoring every byte and pending read error for the response owner.
The owner still closes the body; the observer owns its time budget.

`RetryConfig.Debug` composes the shared diagnostic transport. `DebugConfig.Logger`
overrides the context logger, and `MetadataOnly` emits structured request/status
and header fields without reading application bodies. Error diagnostics use the
closed bearer diagnostic categories and never arbitrary error text. Body preview
behavior remains unchanged unless the new mode is selected.

Debug-level `PathIsCredential` protection is combined with retry-level protection
using OR; either setting keeps credential paths hidden.

`PathIsCredential` renders only the origin in debug diagnostics and default-base
client spans. Retry/recovery logs remain host-only by default; `LogPath` adds a
shape-redacted path only when the path is not declared a credential.
Caller-supplied base transports continue
to own their own tracing. A pending bearer replay that expires while pacing
records budget exhaustion once while preserving the wait's original error.
Pacing retains whether a failure came from the API limiter or token source.
Anticipatory limiter refusal counts toward MaxElapsed exhaustion; an independent
source-owned timeout does not while the transport budget remains live. Caller
cancellation takes precedence for budget attribution, including during token
revalidation after pacing; this does not replace a source-owned returned error.

## Context-aware client credentials

[CLIENT_CREDENTIALS.md](./CLIENT_CREDENTIALS.md) describes the bounded token
exchange and per-call-context cache composed directly with `httpclient` bearer
recovery. Authentication policy stays with callers.
