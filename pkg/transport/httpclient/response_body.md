# Bounded response reads

Use `httpclient.ReadBounded(response.Body, limit)` to admit a response only when
its complete body fits within the limit. It reads at most one extra byte to
separate an exact-limit body from an oversized body. Oversized input returns
`ErrResponseTooLarge` and no bytes. Read failures return no bytes and preserve
the underlying cause for `errors.Is` and `errors.As`.

Use `ReadBoundedWindow` when diagnostics need the actual observed window,
including the extra detection byte. Check both its error and exceeded result
before interpreting the bytes. On a read failure it retains partial bytes for
diagnostics, returns the wrapped read error, and reports exceeded as false.

Non-positive limits select `DefaultMaxResponseBody` (32 MiB). Supply explicit
limits appropriate to the endpoint. Neither helper closes the reader, drains
an oversized response, decides whether to retry, interprets status codes,
decodes JSON, nor redacts bytes. Those responsibilities remain with the caller.
