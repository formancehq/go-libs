# Audit event contract

`pkg/audit` defines the audit event that services and the Gateway publish.
`pkg/audit/httpaudit` captures HTTP requests. Other transports fill the same
`audit.Payload` and publish it with `audit.PublishEvent` or
`audit.NewEventMessageWithError`.

## Envelope

| Field | Value |
|---|---|
| `date` | Publication time, UTC |
| `app` | Name of the publishing component |
| `version` | `audit.EventVersion` (`v2`) |
| `type` | `audit.EventTypeAudit` (`AUDIT`) |
| `payload` | `audit.Payload` |

## Payload

`payload` always contains `id`, `trace_id`, `actor` and `http`.

`payload.grpc` (`audit.GRPC`) is present only for native gRPC calls. Plain HTTP
events do not contain this field. A gRPC event still fills `payload.http` with
the HTTP/2 method, path, host, filtered headers, observed status and response
headers, including trailers. Its text body fields stay empty.

| Field | Meaning |
|---|---|
| `service`, `method` | Parsed from the `/<service>/<method>` path |
| `status_code` | Observed `grpc-status`, 0 through 16. Absent when no valid status was observed |
| `status_message` | Percent-decoded `grpc-message` |
| `termination` | How the call ended (see below) |
| `gateway_status_code` | HTTP status proposed by an intermediate component for its own error |
| `request`, `response` | Bounded capture of each stream direction (`audit.GRPCBody`) |

`audit.GRPCBody.data` holds the raw length-prefixed gRPC frames, encoded as
base64 (`encoding: "base64"`). It is not decoded protobuf. `bytes_total` counts
all bytes observed, `truncated` reports that `data` holds only a prefix, and
`complete` reports that the direction reached its normal end.

`termination` values (`audit.GRPCTermination`):

| Value | Meaning |
|---|---|
| `completed` | The server sent a valid `grpc-status` |
| `canceled` | The client canceled the call |
| `deadline_exceeded` | The call deadline expired |
| `aborted` | The handler aborted the stream |
| `gateway_error` | An intermediate component returned an error |
| `incomplete` | The call returned without a valid `grpc-status` |

New fields are additive. Consumers must ignore unknown fields.
