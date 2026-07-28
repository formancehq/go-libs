# job

`job` lets a component (typically a connectivity plugin) record free-form units
of internal work -- an API page fetch, a store read, a mapping step, a
background-loop iteration -- as one uniform signal:

- a span, nested under whatever caller span is already in `ctx`
- a `job.duration` histogram (seconds)
- a `job.errors` counter, tagged with a best-effort root error type
- a `job.inflight` up-down counter

`Run` additionally installs `job` and `service_name` pprof labels for the
duration of the call, so a continuous profiler (e.g. Pyroscope) can attribute
CPU/allocations back to the job by name. These come from `pprof.Do`, so they
cover the goroutine running the wrapped function and any goroutine it starts
-- but *not* the `Start`/`End` pattern below, which has no call boundary to
scope them to.

Job names are a metric label, so they must come from a small, fixed set
declared ahead of time -- never built from runtime data (a cursor, a tx hash,
a page number). That kind of detail belongs on the span as an attribute
instead, via the `attrs` passed to `Run`/`Start`.

## 1. Declare your jobs

Each plugin declares its own closed set of `job.Desc` values, usually in a
small `instrumentation` package, plus an `All` slice a test can iterate over
to keep the declared contract from drifting out of sync with the code:

```go
// package instrumentation declares the fixed set of jobs this plugin
// records via go-libs' job package.
package instrumentation

import "github.com/formancehq/go-libs/v5/pkg/observe/job"

const serviceName = "myplugin"

var (
	HTTPGet = job.Desc{
		Name:        "myplugin.http_get",
		ServiceName: serviceName,
		Description: "A single authenticated GET against the upstream API",
	}
	ListPage = job.Desc{
		Name:        "myplugin.list_page",
		ServiceName: serviceName,
		Description: "List one page of an upstream resource stream",
	}
)

// All lists every job this plugin declares.
var All = []job.Desc{HTTPGet, ListPage}
```

```go
func TestAllJobsAreDeclaredAndUnique(t *testing.T) {
	require.NotEmpty(t, instrumentation.All)
	seen := make(map[string]bool, len(instrumentation.All))
	for _, d := range instrumentation.All {
		require.NotEmpty(t, d.Name, "job descriptor missing a Name")
		require.NotEmpty(t, d.ServiceName, "job %q missing a ServiceName", d.Name)
		require.NotEmpty(t, d.Description, "job %q missing a Description", d.Name)
		require.False(t, seen[d.Name], "duplicate job name %q", d.Name)
		seen[d.Name] = true
	}
}
```

## 2. Run the job

Wrap the unit of work with `job.Run`. It starts the span/metrics/pprof label,
runs `fn`, and ends the job with whatever error `fn` returns:

```go
func (c *Client) doGet(ctx context.Context, path string, query url.Values) (map[string]any, int, error) {
	var (
		out    map[string]any
		status int
	)

	err := job.Run(ctx, instrumentation.HTTPGet, func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.BaseURL+path, nil)
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		// ...
		return nil
	}, attribute.String("resource", path))

	return out, status, err
}
```

Pass per-call detail (a resource name, an item count) as span attributes via
the trailing `attrs`, not by baking it into the job name:

```go
err = job.Run(ctx, instrumentation.ListPage, func(ctx context.Context) error {
	// ...
	return nil
}, attribute.String("resource", resource), attribute.String("kind", kind))
```

`Run` works just as well around a job that fans out internally (e.g. a
bounded worker pool) -- it's still a single job/span, with the fan-out as
child spans/work underneath:

```go
return job.Run(ctx, instrumentation.HydrateDeposits, func(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// ... fan out across a worker pool using ctx, cancel on first error ...

	return firstErr
}, attribute.String("resource", resource), attribute.Int("items", len(items)))
```

## 3. Start/End directly (advanced)

Use `Start`/`End` instead of `Run` only when one unit of work genuinely spans
more than one function call -- it begins in one place and finishes in another,
so there is no single call for `Run` to wrap. Dispatching to a transport and
completing on a later acknowledgement is the usual shape:

```go
// One job per delivery, started on dispatch and ended by whichever
// acknowledgement comes back for it.
var inflight sync.Map // delivery ID -> *job.Job

transport.OnAck(func(ack Ack) {
	if j, ok := inflight.LoadAndDelete(ack.DeliveryID); ok {
		j.(*job.Job).End(ack.Err)
	}
})

// ... and on each dispatch:
ctx, j := job.Start(ctx, instrumentation.WebhookDelivery)
inflight.Store(d.ID, j)
transport.Send(ctx, d)
```

Each delivery gets its own `Job`, because each delivery is its own unit of
work. Do **not** hoist a single `Job` outside a recurring callback:

```go
// WRONG -- one job shared across every event.
ctx, j := job.Start(ctx, instrumentation.WebhookDelivery)

subscribe(ctx, func(event Event) {
	j.End(handle(ctx, event))
})
```

`End` is idempotent, so the first event closes the job and every later one is
silently dropped: the span, the duration sample, and the error count describe
one delivery no matter how many actually ran.

When the work does fit inside the callback body, that's not a `Start`/`End`
case at all -- reach for `Run`, which scopes the job to the call and installs
pprof labels for you:

```go
subscribe(ctx, func(event Event) {
	_ = job.Run(ctx, instrumentation.WebhookDelivery, func(ctx context.Context) error {
		return handle(ctx, event)
	})
})
```

Jobs built with `Start`/`End` carry the span and all three metrics but no
pprof labels, since there is no call boundary for `pprof.Do` to wrap. To get
profiler attribution for work shaped like the dispatch/ack example above,
wrap whichever goroutine actually burns the CPU:

```go
pprof.Do(ctx, pprof.Labels(
	"job", instrumentation.WebhookDelivery.Name,
	"service_name", instrumentation.WebhookDelivery.ServiceName,
), func(ctx context.Context) {
	transport.Send(ctx, d)
})
```
