// Package job lets a component (typically a connectivity plugin) record
// free-form units of internal work -- an API page fetch, a store read, a
// mapping step, a background-loop iteration -- as a uniform signal: one span
// (nested under whatever caller span is already in ctx), one duration
// histogram, and one error counter.
//
// Run additionally installs pprof labels for the duration of the call, so a
// continuous profiler can attribute allocations/CPU back to the same job
// name. Those labels come from pprof.Do, which only labels the goroutine
// running fn (and any goroutine fn starts). See Run.
//
// Job names and component names are metric labels, so they must come from a
// small, fixed set declared ahead of time by the caller (see Desc) -- never
// built from runtime data (a cursor, a tx hash, a page number). That kind of
// detail belongs on the span as an attribute, which does not carry the same
// cardinality cost as a Prometheus label.
package job

import (
	"context"
	"fmt"
	"runtime/pprof"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
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
	// ComponentName identifies which component this job belongs to -- e.g.
	// "bankingbridge" for a connectivity plugin, but this package is not
	// plugin-specific, so keep it generic to whatever is calling Run.
	// It becomes the "component_name" span and metric attribute, letting
	// job.duration/job.errors/job.inflight be filtered or grouped by source
	// even though every connectivity plugin shares one OTel service.name
	// resource attribute -- component_name identifies the plugin
	// independently of that process-wide service.name. The collector, not
	// this package, is responsible for promoting service.name and other
	// resource attributes onto the same data points (e.g. via
	// resource_to_telemetry_conversion for a Prometheus remote-write target).
	ComponentName string
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

// baseAttrs returns the fixed attributes every span and metric for a job
// carries: the job name and the component name. Process-level identity
// (service.name, deployment.environment, ...) lives on the OTel Resource,
// not here -- a collector-side resource_to_telemetry_conversion setting is
// the right place to promote that onto Prometheus/VictoriaMetrics labels, so
// this package does not duplicate it onto every span and metric data point
// itself.
func baseAttrs(jobName, componentName string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("job_name", jobName),
		attribute.String("component_name", componentName),
	}
}

// Run wraps fn as a single job: a span (parented via ctx), duration/error
// metrics, and pprof labels ("job" = d.Name, "component_name" =
// d.ComponentName) scoped to fn's execution so a continuous profiler (e.g.
// pyroscope, which samples goroutine pprof labels) can filter CPU/allocation
// samples by job name or by component.
//
// The function's returned error is both what Run returns and what gets
// recorded on the job (span event + status + error counter). A panic from fn
// (including calling a nil fn) is recorded the same way and then rethrown,
// so the job is still closed out -- span ended, error counted, no longer
// in-flight -- before the panic continues to unwind through Run's caller.
func Run(ctx context.Context, d Desc, fn func(ctx context.Context) error, attrs ...attribute.KeyValue) error {
	jobAttrs := baseAttrs(d.Name, d.ComponentName)

	spanAttrs := append(append([]attribute.KeyValue{}, jobAttrs...), attrs...)
	ctx, span := tracer.Start(ctx, d.Name, trace.WithAttributes(spanAttrs...))

	start := time.Now()
	jobInflight.Add(ctx, 1, otelmetric.WithAttributes(jobAttrs...))

	var err error
	defer func() {
		endCtx := context.Background()

		jobDuration.Record(endCtx, time.Since(start).Seconds(), otelmetric.WithAttributes(jobAttrs...))
		jobInflight.Add(endCtx, -1, otelmetric.WithAttributes(jobAttrs...))

		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			jobErrors.Add(endCtx, 1, otelmetric.WithAttributes(append(jobAttrs, attribute.String("error_type", rootType(err)))...))
		}

		span.End()
	}()

	pprof.Do(ctx, pprof.Labels("job", d.Name, "component_name", d.ComponentName), func(ctx context.Context) {
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

// panicError converts a recovered panic value into an error for Run to
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
