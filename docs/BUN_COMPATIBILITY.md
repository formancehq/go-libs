# Bun compatibility

The module requires Bun, `pgdialect`, `bundebug` and `bunotel` v1.3.0 together.
Bun v1.3.0 uses `int64` for query limits, offsets and count results. Go module
version selection also applies this API to consumers that previously selected
Bun v1.2.x. Convert consumer calls that pass typed `int` arguments to `int64`,
and update interfaces or mocks that specify Bun count results.

The go-libs pagination query fields remain `uint64`. `Cursor.PageSize` and the
circuit breaker storage constructor's limit remain `int`.

Pagination rejects page sizes above `min(MaxInt-1, MaxInt64-1)` with
`ErrInvalidPageSize` before SQL execution. This reserves one row for lookahead
and keeps the response size representable on the target architecture. Offset
pagination rejects offsets above `MaxInt64` and returns an error if the next
cursor's offset would exceed that bound. Previous offsets use unsigned
subtraction with a floor of zero, without narrowing to the platform's `int`.
Zero page sizes retain their existing behavior: offset pagination has no limit;
column pagination fetches one lookahead row.

## Delivery

Consumers need a tagged go-libs version containing this change. Review and merge
the PR, then publish an explicitly authorized version. Updating this source branch
alone does not repair consumers pinned to a published version. Consumer source
calls to Bun must also use the v1.3.0 API; upgrading go-libs cannot convert those
calls automatically.
