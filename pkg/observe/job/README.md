# job

`job` lets a component (typically a connectivity plugin) record free-form units
of internal work -- an API page fetch, a store read, a mapping step, a
background-loop iteration -- as one uniform signal:

- a span, nested under whatever caller span is already in `ctx`
- a `job.duration` histogram (seconds)
- a `job.errors` counter, tagged with a best-effort root error type
- a `job.inflight` up-down counter

`Run` additionally installs `job` and `component_name` pprof labels for the
duration of the call, so a continuous profiler (e.g. Pyroscope) can attribute
CPU/allocations back to the job by name. These come from `pprof.Do`, so they
cover the goroutine running the wrapped function and any goroutine it starts.

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

const componentName = "myplugin"

var (
	HTTPGet = job.Desc{
		Name:          "myplugin.http_get",
		ComponentName: componentName,
		Description:   "A single authenticated GET against the upstream API",
	}
	ListPage = job.Desc{
		Name:          "myplugin.list_page",
		ComponentName: componentName,
		Description:   "List one page of an upstream resource stream",
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
		require.NotEmpty(t, d.ComponentName, "job %q missing a ComponentName", d.Name)
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
