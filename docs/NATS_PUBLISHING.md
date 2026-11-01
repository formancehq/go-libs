# Opt-in synchronous NATS recovery

The existing `NewNatsConn`, `NewNatsPublisherWithConn`, flags and default
publishers retain their behavior. `RecoveringNATSPublisher` is an additive opt-in
`message.Publisher`, using Watermill's marshaler and subject calculator and
nats.go's synchronous JetStream API. The implementation targets the actual
v5.6.0 checkout: Watermill NATS v2.1.3 and nats.go v1.49.0. No dependency change
or Watermill fork is required. The existing `messagingfx.NatsModule` still uses
the legacy publisher and shared publisher/subscriber connection; it is unchanged.
Use a dedicated connection when opting into this new owning publisher.

## Ownership

| Component | Responsibility |
| --- | --- |
| Caller | Foreground budget, attempt timeout, retry count/backoff, heartbeat, reconnect settings, connection callbacks, whether HTTP cancellation cancels publication, error logging and final delivery policy. |
| go-libs publisher | One wire snapshot per message, stable UUID as `Nats-Msg-Id`, sequential batches, shared per-message budget, bounded attempts, cancellation, narrow transient error classification, contextual stream management. |
| go-libs dialer | Total dial timeout across DNS/address attempts; bounded raw writes, close raw socket on write failure without taking NATS locks. |
| Watermill | Message representation, configured encoding and stream subject calculation. |
| Native NATS | TLS and certificate verification, protocol framing, acknowledgements, heartbeat, server discovery/selection, reconnect and reconnect buffer. |

A healthy NATS server already routes JetStream requests to the stream leader.
No leader lookup or forced reconnect on an acknowledgement timeout is needed.

## Gateway migration recipe

After maintainer review and a go-libs release, gateway can replace its local
retry publisher and raw dialer with the following wiring. Keep its existing
credentials, TLS, reconnect options, validation and lifecycle callbacks.

```go
dialer, err := publish.NewNATSBoundedDialer(
    net.Dialer{},
    2*time.Second,    // total dial, including address resolution
    4*time.Second,    // total raw write
    500*time.Millisecond, // each 16 KiB chunk
)
if err != nil {
    return err
}
config.NatsOptions = append(config.NatsOptions,
    nats.SetCustomDialer(dialer),
    nats.PingInterval(2*time.Second),
    nats.MaxPingsOutstanding(2),
)
config.JetStream.TrackMsgId = true
conn, err := publish.NewNatsConn(config)
if err != nil {
    return err
}
publisher, err := publish.NewRecoveringNATSPublisherWithConn(conn, config,
    publish.NATSRecoveryConfig{
        Budget: 16*time.Second,
        AttemptTimeout: 5*time.Second,
        RetryDelay: 250*time.Millisecond,
        MaxRetryDelay: time.Second,
        MaxAttempts: 3,
        MessageContext: context.WithoutCancel, // preserve gateway audit policy
    })
if err != nil {
    conn.Close() // construction failure retains caller ownership
    return err
}
// Transfer connection ownership to publisher. Close publisher at shutdown.
```

Gateway PR 141 remains independently releasable. Do not change its dependency
before this API is reviewed and released. Remove gateway's local facade, retry
loop and dialer in a later migration; keep configuration, callbacks and HTTP
policy in gateway. Do not layer both retry implementations.

`Publish` respects `message.Context()` by default. The current synchronous
`httpaudit` path calls `audit.PublishEvent` with the HTTP request context, and
`publish.NewMessage` attaches that context to the Watermill message. Gateway
must therefore set `MessageContext: context.WithoutCancel` in its publisher
configuration to preserve PR 141's detached foreground audit policy. This keeps
context policy in gateway wiring, with no middleware change. Other callers can
leave `MessageContext` nil to retain request cancellation. The transform runs
before the per-message budget; it cannot remove the recovery budget. It must be
concurrency-safe and return a non-nil context. No background publication goroutine
is started. The existing `httpaudit.AsyncPublisher` in this checkout is unchanged
and is not enabled by this change.

`MaxAttempts` counts the initial attempt. Three attempts preserve PR 141's
policy: three fast failures can exhaust it in about 750 ms. Applications that
need to recover through a longer transient failure can set `MaxAttempts: 0` to
retry until the same 16 s budget expires, with backoff capped by `MaxRetryDelay`.
This does not extend the total foreground budget. This is a caller decision;
the shared API does not impose audit-specific timing or a 100 ms heartbeat floor.
Budget and attempt timeouts apply per message, not per batch.

## Publication and provisioning semantics

The publisher requires synchronous JetStream and `TrackMsgId`. Empty UUIDs fail
before sending. Each message is marshaled once, then its bytes and headers are
snapshotted. Each retry gets its own copy, and `Nats-Msg-Id` uses the original
Watermill UUID, including for custom marshalers. Do not mutate messages while
publishing; marshalers must support concurrent calls. An earlier acknowledged
batch item is never replayed because a later item failed.

Native JetStream no-responder retries are disabled on each attempt, preventing
nested retry amplification. The attempt context overrides static publish
contexts; conflicting static `AckWait` options are rejected by nats.go rather
than silently defeating the context. Avoid static per-call timeout options.

Automatic provisioning preserves Watermill's stream name (topic) and calculated
subjects. `StreamInfo` and `AddStream` receive the same attempt context. Creation
runs only after `ErrStreamNotFound`, never after timeout or permission errors.
Provisioning is checked on every attempt, without an unbounded stream cache.
Concurrent stream creation is handled by the native server's idempotent creation
of the same configuration. Conflicting configurations remain errors.

Retries cover timeouts, disconnected transport, missing responders, reconnect
buffer saturation, raw write failures and exactly JetStream error code `10158`
(the same ID is awaiting commit). Generic API `503` and other `409` errors remain
permanent; broad HTTP-like status codes alone do not identify transient errors.
A closed native client and caller cancellation are permanent. Errors are returned
to the caller for logging; the shared mechanism installs no process-exit callback.

## Transport bounds and limitations

`NewNATSBoundedDialer` is independent from the publisher and must be installed
before connecting. It wraps the raw socket beneath native TLS. Writes run in
16 KiB chunks with a per-chunk stall deadline and a whole-write deadline.
A healthy large write can progress longer than the stall timeout; choose the
whole-write timeout for maximum frame size and minimum supported throughput.
Even progressing writes are closed when the whole-write bound expires. This is
an intentional transport policy, not a throughput guarantee. TLS can split a
frame into multiple raw writes; the foreground context is still required.
Shorter caller/TLS deadlines are preserved; concurrent deadline updates cannot
extend the current chunk or whole-write bound. Any failed raw write closes the
raw socket, waking the reader and enabling native reconnect without `nc.mu`.
Queued writes fail on the closed socket.

The foreground context is not a hard wall-clock HTTP ceiling. Native connection
locks, queued writers, dialing, TLS, scheduling and request cleanup can add time.
`Close` cancels active publication/backoff and closes the owned connection; native
locks can delay it until bounded writes finish. Do not share the owned connection
with a subscriber or another publisher. This API deliberately preserves the
existing Watermill constructor's connection ownership convention.

A lost acknowledgement is ambiguous. JetStream deduplicates stable IDs only
within its configured duplicate window. The reconnect buffer may deliver a
message after this wrapper returns, even beyond the usual 120 s window.
Consumers must deduplicate stable business IDs. There is no exactly-once claim,
durable spool or crash persistence. Async audit/HTTP decoupling, queue capacity
and loss policy remain deferred.

## Validation and release

Run the regular race suite with `just tests`, repository checks with
`just pre-commit`, and the real fault suite with:

```sh
go test -race -tags it ./pkg/messaging/publish -count=1
```

The fault suite embeds a real NATS server and uses a TCP proxy for silent loss,
lost acknowledgements and stopped reads. It covers 64 concurrent 128 KiB
publications with heartbeat disabled, production heartbeat/reconnect timing,
shared budgets, disabled native retries, unavailable management, stable encoding,
10158, batch progress, cancellation, shutdown and legacy constructor behavior.
Transport tests cover blocked writes, progressing large writes, whole-write bounds,
shorter/concurrent deadlines, shutdown and verified TLS over the wrapper.
These tests isolate client behavior. They do not prove Linux production traffic,
a three-node quorum/election, staging HTTP load or a strict HTTP wall-clock cap.

The API addition is compatible with module v5 and calls for a minor release after
the current v5.6.0, subject to intervening releases. No tag, push, PR, gateway
migration or deployment is part of this local change.

## Optional Watermill upstream seam

No upstream change is necessary for this implementation. If Watermill should own
this mechanism later, its minimal useful seam is a per-message context passed to
both `JetStream.PublishMsg` and `StreamInfo`/`AddStream`, with an explicit way to
disable native retries and choose connection ownership. Retain the configured
marshaler, subject calculator and existing defaults. Merely adding
`nats.Context(msg.Context())` to publish does not fix management timeouts, wire
snapshotting, connection ownership or blocked writes. Prepare any upstream patch
in an authorized Watermill checkout; do not edit the module cache, add a silent
replace/vendor fork, or publish a fork.
