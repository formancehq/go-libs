// Package job lets a component (typically a connectivity plugin) record
// free-form units of internal work -- an API page fetch, a store read, a
// mapping step, a background-loop iteration -- as a uniform signal: one span
// (nested under whatever caller span is already in ctx), one duration
// histogram, and one error counter.
//
// Run additionally installs pprof labels for the duration of the call, so a
// continuous profiler can attribute allocations/CPU back to the same job
// name. Those labels come from pprof.Do, which only labels the goroutine
// running fn (and any goroutine fn starts) -- the Start/End pattern gets no
// labels, since there is no call for them to be scoped to. See Run.
//
// Job names and service names are metric labels, so they must come from a
// small, fixed set declared ahead of time by the caller (see Desc) -- never
// built from runtime data (a cursor, a tx hash, a page number). That kind of
// detail belongs on the span as an attribute, which does not carry the same
// cardinality cost as a Prometheus label.
package job

import (
	"context"
	"fmt"
	"runtime/pprof"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/formancehq/go-libs/v5/pkg/observe"
)

const instrumentationName = "github.com/formancehq/go-libs/v5/pkg/observe/job"

// Desc declares one job a component can run.
type Desc struct {
	// Name identifies the job. It becomes the span name and the "job_name"
	// metric attribute -- keep it a fixed, caller-declared constant.
	//
	// The attribute is "job_name", not "job": the OTel-to-Prometheus
	// translation the collector applies before remote-writing already
	// synthesizes a reserved "job" label from the resource's service.name
	// (identical across every connectivity plugin, since they all report the
	// same service.name), and silently overwrites any data-point attribute of
	// the same name rather than renaming it -- so a literal "job" attribute
	// here would never survive to VictoriaMetrics.
	Name string
	// ServiceName identifies which component this job belongs to -- e.g.
	// "bankingbridge" for a connectivity plugin, but this package is not
	// plugin-specific, so keep it generic to whatever is calling Run/Start.
	// It becomes the "service_name" span and metric attribute, letting
	// job.duration/job.errors/job.inflight be filtered or grouped by source
	// even though every connectivity plugin shares one OTel service.name
	// resource attribute.
	ServiceName string
	// Description documents what the job does; used only for humans
	// reading the declared job list, never emitted as telemetry.
	Description string
}

var (
	tracer = otel.Tracer(instrumentationName)
	meter  = otel.Meter(instrumentationName)

	jobDuration, _ = meter.Float64Histogram("job.duration",
		otelmetric.WithDescription("Duration of a job in seconds"),
		otelmetric.WithUnit("s"))
	jobErrors, _ = meter.Int64Counter("job.errors",
		otelmetric.WithDescription("Number of jobs that ended in error"))
	jobInflight, _ = meter.Int64UpDownCounter("job.inflight",
		otelmetric.WithDescription("Number of jobs currently running"))
)

// Job is a started, not-yet-ended unit of work returned by Start.
type Job struct {
	span        trace.Span
	start       time.Time
	name        string
	serviceName string
	endOnce     sync.Once
}

// baseAttrs returns the fixed attributes every span and metric for a job
// carries: the job name, the service name, and whatever
// observe.ResourceAttributes reports (OTEL_RESOURCE_ATTRIBUTES plus any
// programmatically configured resource attributes).
func baseAttrs(jobName, serviceName string) []attribute.KeyValue {
	return append([]attribute.KeyValue{
		attribute.String("job_name", jobName),
		attribute.String("service_name", serviceName),
	}, observe.ResourceAttributes()...)
}

// Start begins a job: it opens a span as a child of whatever span is already
// in ctx (so it nests correctly under an inbound gRPC call, for instance),
// and marks the job in-flight. The returned ctx carries the new span and
// must be threaded into any work done as part of the job so further child
// spans nest correctly.
//
// Prefer Run when the job is a single function call; use Start/End directly
// only when one unit of work genuinely spans more than one call -- it begins
// in one place and finishes in another, e.g. started on dispatch and ended by
// a later acknowledgement callback. Work that fits inside one callback body
// is a Run, not a Start/End: hoisting a single Job outside a recurring
// callback records only the first invocation, since End is idempotent.
//
// Unlike Run, Start installs no pprof labels -- the work it covers has no
// call boundary to scope them to. A caller that wants its job's work
// attributed in a continuous profiler has to wrap the goroutine body itself,
// e.g. pprof.Do(ctx, pprof.Labels("job", d.Name, "service_name",
// d.ServiceName), ...).
func Start(ctx context.Context, d Desc, attrs ...attribute.KeyValue) (context.Context, *Job) {
	spanAttrs := append(baseAttrs(d.Name, d.ServiceName), attrs...)
	ctx, span := tracer.Start(ctx, d.Name, trace.WithAttributes(spanAttrs...))

	jobInflight.Add(ctx, 1, otelmetric.WithAttributes(baseAttrs(d.Name, d.ServiceName)...))

	return ctx, &Job{span: span, start: time.Now(), name: d.Name, serviceName: d.ServiceName}
}

// End completes the job: it records the job's duration, marks it no longer
// in-flight, and -- if err is non-nil -- records the error on the span and
// increments the error counter, tagged with a best-effort error type.
//
// End is idempotent and safe to call concurrently: only the first call's err
// is recorded, and later (or concurrent) calls are no-ops once that first
// call's teardown has run.
func (j *Job) End(err error) {
	if j == nil {
		return
	}

	j.endOnce.Do(func() {
		ctx := context.Background()
		jobAttrs := baseAttrs(j.name, j.serviceName)

		jobDuration.Record(ctx, time.Since(j.start).Seconds(), otelmetric.WithAttributes(jobAttrs...))
		jobInflight.Add(ctx, -1, otelmetric.WithAttributes(jobAttrs...))

		if err != nil {
			j.span.RecordError(err)
			j.span.SetStatus(codes.Error, err.Error())
			jobErrors.Add(ctx, 1, otelmetric.WithAttributes(append(jobAttrs, attribute.String("error_type", rootType(err)))...))
		}

		j.span.End()
	})
}

// Run wraps fn as a single job: a span (parented via ctx), duration/error
// metrics, and pprof labels ("job" = d.Name, "service_name" = d.ServiceName)
// scoped to fn's execution so a continuous profiler (e.g. pyroscope, which
// samples goroutine pprof labels) can filter CPU/allocation samples by job
// name or by service.
//
// The function's returned error is both what Run returns and what gets
// recorded on the job (span event + status + error counter). A panic from fn
// (including calling a nil fn) is recorded the same way and then rethrown,
// so the job is still closed out -- span ended, error counted, no longer
// in-flight -- before the panic continues to unwind through Run's caller.
func Run(ctx context.Context, d Desc, fn func(ctx context.Context) error, attrs ...attribute.KeyValue) error {
	ctx, j := Start(ctx, d, attrs...)

	var err error
	defer func() {
		j.End(err)
	}()

	pprof.Do(ctx, pprof.Labels("job", d.Name, "service_name", d.ServiceName), func(ctx context.Context) {
		defer func() {
			if r := recover(); r != nil {
				err = panicError(r)
				panic(r)
			}
		}()

		err = fn(ctx)
	})

	return err
}

// panicError converts a recovered panic value into an error for End to
// record, preserving it as-is when it already is one (e.g. the
// runtime.Error from calling a nil fn) so rootType still reports its real
// underlying type rather than a generic wrapper.
func panicError(r any) error {
	if e, ok := r.(error); ok {
		return e
	}
	return fmt.Errorf("panic: %v", r)
}

// rootType returns a best-effort type name for the deepest error in err's
// Unwrap chain, so "top exceptions" dashboards group by underlying cause
// rather than by whatever wrapping happened to be applied at the call site.
func rootType(err error) string {
	for {
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			break
		}
		next := unwrapped.Unwrap()
		if next == nil {
			break
		}
		err = next
	}

	return fmt.Sprintf("%T", err)
}
