package job_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/formancehq/go-libs/v5/pkg/observe/job"
)

// The OTel global tracer/meter vars that job.go captures at package-init time
// only re-delegate correctly to the *first* SetTracerProvider/SetMeterProvider
// call made afterwards (matching real usage: a service installs its SDK once
// at startup, before any job.Run call). So the whole suite installs one
// recording provider of each kind here, and individual tests isolate
// themselves by using a unique job name and filtering the shared recorder/
// reader down to just that name, rather than each swapping providers.
var (
	spanRecorder = tracetest.NewSpanRecorder()
	metricReader = sdkmetric.NewManualReader()
)

func TestMain(m *testing.M) {
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder)))
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader)))

	os.Exit(m.Run())
}

func spansNamed(name string) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, s := range spanRecorder.Ended() {
		if s.Name() == name {
			out = append(out, s)
		}
	}
	return out
}

func collectMetrics(t *testing.T) map[string]metricdata.Metrics {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, metricReader.Collect(context.Background(), &rm))

	byName := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			byName[m.Name] = m
		}
	}

	return byName
}

func dataPointForJob(t *testing.T, sum metricdata.Sum[int64], jobName string) (metricdata.DataPoint[int64], bool) {
	t.Helper()

	for _, dp := range sum.DataPoints {
		if v, ok := dp.Attributes.Value(attribute.Key("job_name")); ok && v.AsString() == jobName {
			return dp, true
		}
	}

	return metricdata.DataPoint[int64]{}, false
}

func spanAttr(span sdktrace.ReadOnlySpan, key string) (attribute.Value, bool) {
	for _, a := range span.Attributes() {
		if a.Key == attribute.Key(key) {
			return a.Value, true
		}
	}

	return attribute.Value{}, false
}

func TestRunNestsSpanUnderCallerSpan(t *testing.T) {
	tracer := otel.Tracer("test")
	ctx, parentSpan := tracer.Start(context.Background(), "TestRunNestsSpanUnderCallerSpan.parent")

	err := job.Run(ctx, job.Desc{Name: "test.job.nesting", ComponentName: "test-service"}, func(ctx context.Context) error {
		return nil
	})
	require.NoError(t, err)
	parentSpan.End()

	jobSpans := spansNamed("test.job.nesting")
	require.Len(t, jobSpans, 1)
	jobSpan := jobSpans[0]

	require.Equal(t, parentSpan.SpanContext().TraceID(), jobSpan.SpanContext().TraceID(),
		"job span must belong to the same trace as its caller, even though it is a distinct span")
	require.Equal(t, parentSpan.SpanContext().SpanID(), jobSpan.Parent().SpanID(),
		"job span must be parented to the caller's span")

	componentNameAttr, ok := spanAttr(jobSpan, "component_name")
	require.True(t, ok, "job span must carry a component_name attribute")
	require.Equal(t, "test-service", componentNameAttr.AsString())
}

func TestRunRecordsErrorOnSpan(t *testing.T) {
	wantErr := errors.New("boom")
	err := job.Run(context.Background(), job.Desc{Name: "test.job.err", ComponentName: "test-service"}, func(ctx context.Context) error {
		return wantErr
	})
	require.ErrorIs(t, err, wantErr)

	jobSpans := spansNamed("test.job.err")
	require.Len(t, jobSpans, 1)
	require.Equal(t, codes.Error, jobSpans[0].Status().Code)
	require.NotEmpty(t, jobSpans[0].Events(), "an error must be recorded as a span event")
}

func TestRunRecordsMetrics(t *testing.T) {
	const jobName = "test.job.metrics"
	const componentName = "test-service"

	require.NoError(t, job.Run(context.Background(), job.Desc{Name: jobName, ComponentName: componentName}, func(ctx context.Context) error {
		return nil
	}))

	failing := fmt.Errorf("wrapped: %w", errors.New("root cause"))
	err := job.Run(context.Background(), job.Desc{Name: jobName, ComponentName: componentName}, func(ctx context.Context) error {
		return failing
	})
	require.ErrorIs(t, err, failing)

	byName := collectMetrics(t)
	require.Contains(t, byName, "job.duration")
	require.Contains(t, byName, "job.errors")
	require.Contains(t, byName, "job.inflight")

	errorSum, ok := byName["job.errors"].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	errDP, found := dataPointForJob(t, errorSum, jobName)
	require.True(t, found, "expected a job.errors data point for %q", jobName)
	require.Equal(t, int64(1), errDP.Value)
	componentNameAttr, ok := errDP.Attributes.Value(attribute.Key("component_name"))
	require.True(t, ok, "job.errors data point must carry a component_name attribute")
	require.Equal(t, componentName, componentNameAttr.AsString())

	inflightSum, ok := byName["job.inflight"].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	inflightDP, found := dataPointForJob(t, inflightSum, jobName)
	require.True(t, found, "expected a job.inflight data point for %q", jobName)
	require.Equal(t, int64(0), inflightDP.Value, "inflight must return to zero once both runs end")
	componentNameAttr, ok = inflightDP.Attributes.Value(attribute.Key("component_name"))
	require.True(t, ok, "job.inflight data point must carry a component_name attribute")
	require.Equal(t, componentName, componentNameAttr.AsString())
}

func TestEndIsSafeToCallOnceAndIgnoresNilJob(t *testing.T) {
	var nilJob *job.Job
	require.NotPanics(t, func() { nilJob.End(nil) })
}

func TestEndIsIdempotentUnderConcurrency(t *testing.T) {
	const jobName = "test.job.concurrent-end"

	_, j := job.Start(context.Background(), job.Desc{Name: jobName, ComponentName: "test-service"})

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			j.End(nil)
		}()
	}
	wg.Wait()

	jobSpans := spansNamed(jobName)
	require.Len(t, jobSpans, 1, "concurrent End calls must only end the span once")

	byName := collectMetrics(t)
	inflightSum, ok := byName["job.inflight"].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	inflightDP, found := dataPointForJob(t, inflightSum, jobName)
	require.True(t, found, "expected a job.inflight data point for %q", jobName)
	require.Equal(t, int64(0), inflightDP.Value, "inflight must be decremented exactly once despite concurrent End calls")
}

// baseAttrs must be a pure function of (jobName, componentName), with no
// dependency on mutable process state -- otherwise Start's +1 and End's -1
// could be computed with different attribute sets (e.g. if some external
// input changed between the two calls) and land on two distinct series
// instead of netting to zero on one.
func TestInflightIncrementAndDecrementNetToASingleZeroSeries(t *testing.T) {
	const jobName = "test.job.inflight-single-series"

	_, j := job.Start(context.Background(), job.Desc{Name: jobName, ComponentName: "test-service"})
	j.End(nil)

	byName := collectMetrics(t)
	inflightSum, ok := byName["job.inflight"].Data.(metricdata.Sum[int64])
	require.True(t, ok)

	var matches int
	for _, dp := range inflightSum.DataPoints {
		if v, ok := dp.Attributes.Value(attribute.Key("job_name")); ok && v.AsString() == jobName {
			matches++
			require.Equal(t, int64(0), dp.Value, "the +1 from Start and the -1 from End must land on the same series and net to zero")
		}
	}
	require.Equal(t, 1, matches, "Start and End must produce identical attribute sets for the same job, collapsing into exactly one series")
}

func TestRunEndsJobEvenIfFnPanics(t *testing.T) {
	const jobName = "test.job.panic"
	const componentName = "test-service"

	var recovered any
	func() {
		defer func() { recovered = recover() }()

		_ = job.Run(context.Background(), job.Desc{Name: jobName, ComponentName: componentName}, func(ctx context.Context) error {
			panic("boom")
		})
	}()
	require.Equal(t, "boom", recovered, "Run must rethrow the panic after recording it")

	jobSpans := spansNamed(jobName)
	require.Len(t, jobSpans, 1, "the job span must be ended even when fn panics")
	require.True(t, jobSpans[0].EndTime().After(jobSpans[0].StartTime()))
	require.Equal(t, codes.Error, jobSpans[0].Status().Code, "a panic must be recorded as a span error")
	require.NotEmpty(t, jobSpans[0].Events(), "a panic must be recorded as a span event")

	byName := collectMetrics(t)
	inflightSum, ok := byName["job.inflight"].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	inflightDP, found := dataPointForJob(t, inflightSum, jobName)
	require.True(t, found, "expected a job.inflight data point for %q", jobName)
	require.Equal(t, int64(0), inflightDP.Value, "inflight must return to zero even when fn panics")

	errorSum, ok := byName["job.errors"].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	errDP, found := dataPointForJob(t, errorSum, jobName)
	require.True(t, found, "expected a job.errors data point for %q", jobName)
	require.Equal(t, int64(1), errDP.Value, "a panic must increment job.errors")
}

func TestRunEndsJobEvenIfFnIsNil(t *testing.T) {
	const jobName = "test.job.nil-fn"

	var recovered any
	func() {
		defer func() { recovered = recover() }()

		_ = job.Run(context.Background(), job.Desc{Name: jobName, ComponentName: "test-service"}, nil)
	}()
	require.NotNil(t, recovered, "calling a nil fn must panic, and Run must rethrow it")

	jobSpans := spansNamed(jobName)
	require.Len(t, jobSpans, 1, "the job span must be ended even when fn is nil")
	require.Equal(t, codes.Error, jobSpans[0].Status().Code, "a nil-fn panic must be recorded as a span error")

	byName := collectMetrics(t)
	inflightSum, ok := byName["job.inflight"].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	inflightDP, found := dataPointForJob(t, inflightSum, jobName)
	require.True(t, found, "expected a job.inflight data point for %q", jobName)
	require.Equal(t, int64(0), inflightDP.Value, "inflight must return to zero even when fn is nil")
}
