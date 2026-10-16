# Process-local performance diagnostics

`pkg/observe/performance` records caller-supplied observations for local diagnostic views. It has no exporter, background worker, clock, logger or service lifecycle. Use the metrics package for exported telemetry. A missing observation stays unavailable instead of being estimated from other counters.

Create a recorder with `performance.New(performance.Config{})`, retain handles returned by `Stage(name, channel)`, and record `Sample` values at actual completion boundaries. Stage names and channels must be a bounded static vocabulary: stage registration is retained for the recorder lifetime. Each stage retains a fixed second-bucket ring and a bounded recent latency ring; `LatencySampleLimit` controls the latter and defaults to 1,024.

`Record` refuses zero timestamps and negative durations. An explicitly observed zero duration is a latency sample. Counts are independent measurements; transactions, calls and items are never inferred from each other. `AddInFlight` refuses underflow and overflow; a successful zero adjustment makes an observed zero available.

`Snapshot(now)` returns independent copies sorted by stage name and channel. Each stage is copied under its own lock, so concurrent cross-stage observations are not one atomic transaction. The requested clock value belongs to the caller. Rolling 10-second and 60-second windows include only complete one-second buckets in `[Start, End)`. Rates require at least two calls and positive observed elapsed time. Latency percentiles use nearest rank over bounded most recent samples; observed and retained counts are distinct.

Adapters can publish `QueueSnapshot` without a queue implementation or lifecycle dependency. Queue availability, capacity, byte depth, drops and oldest age must come from the queue owner. The recorder does not interpret delivery, recovery or accounting state.

Validation: `go test -race ./pkg/observe/performance` and `go test ./pkg/observe/performance -run '^$' -bench . -benchtime=50ms`.
