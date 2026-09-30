// Package webhook provides a bounded HTTP receiver for inbound webhook
// deliveries.
//
// The receiver owns the provider-neutral part of accepting a delivery:
//
//   - the method gate: HEAD answers 200 with no body (senders probe endpoints
//     with it before delivering), POST is a delivery, anything else is 405;
//   - a hard cap on the request body: a body larger than [Config.MaxBody] is
//     rejected with 413 and is never truncated, and at most MaxBody+1 bytes
//     are read from the connection;
//   - the ordering: the exact raw bytes are authenticated by the [Verifier]
//     before anything decodes them, and a delivery that fails verification
//     (401) never reaches [Config.Deliver];
//   - the acknowledgement: 200 with an empty body once Deliver returns nil,
//     503 when it returns an error so the sender redelivers;
//   - one span per delivery on the global OpenTelemetry tracer provider. The
//     span never carries the body, the headers or the query string, which can
//     hold signatures and secrets.
//
// What stays with the consumer:
//
//   - the signature scheme: HMAC header formats, timestamp tolerance, replay
//     windows, certificate fetching and key rotation all live in the Verifier.
//     The request body has already been consumed when Verify runs, so a
//     Verifier reads the body argument, never r.Body;
//   - decoding, deduplication and idempotency of the delivered events;
//   - queueing or spooling: Deliver decides whether a delivery is durably
//     accepted before it returns, and returning an error is the only way to
//     ask the sender to redeliver;
//   - routing: the receiver is a plain [net/http.Handler], mounted on whatever
//     mux and path the consumer chooses, behind whatever middleware it uses.
package webhook
