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

	"github.com/formancehq/go-libs/v5/pkg/observe"
	"github.com/formancehq/go-libs/v5/pkg/observe/job"
)

// The OTel global tracer/meter vars that job.go captures at package-init time
// only re-delegate correctly to the *first* SetTracerProvider/SetMeterProvider
// call made afterwards (matching real usage: a service installs its SDK once
// at startup, before any job.Run call). So the whole suite installs one
// recording provider of each kind here, and individual tests isolate
// themselves by using a unique job name and filtering the shared recorder/
// reader down to just that name, rather than each swapping providers.
//
// OTEL_RESOURCE_ATTRIBUTES is set here too, before m.Run(), for the same
// reason: job.go reads it through a sync.OnceValue that caches whatever the
// first caller observes, matching how a real process only ever configures it
// once at startup.
const (
	wantDeploymentEnvironment = "test"
	wantStack                 = "unittest"
)

var (
	spanRecorder = tracetest.NewSpanRecorder()
	metricReader = sdkmetric.NewManualReader()
)

func TestMain(m *testing.M) {
	if err := os.Setenv("OTEL_RESOURCE_ATTRIBUTES", fmt.Sprintf("deployment.environment=%s,stack=%s", wantDeploymentEnvironment, wantStack)); err != nil {
		panic(err)
	}

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

func requireResourceAttrs(t *testing.T, attrs attribute.Set) {
	t.Helper()

	deploymentEnv, ok := attrs.Value(attribute.Key("deployment.environment"))
	require.True(t, ok, "expected a deployment.environment attribute from OTEL_RESOURCE_ATTRIBUTES")
	require.Equal(t, wantDeploymentEnvironment, deploymentEnv.AsString())

	stack, ok := attrs.Value(attribute.Key("stack"))
	require.True(t, ok, "expected a stack attribute from OTEL_RESOURCE_ATTRIBUTES")
	require.Equal(t, wantStack, stack.AsString())
}

func TestRunNestsSpanUnderCallerSpan(t *testing.T) {
	tracer := otel.Tracer("test")
	ctx, parentSpan := tracer.Start(context.Background(), "TestRunNestsSpanUnderCallerSpan.parent")

	err := job.Run(ctx, job.Desc{Name: "test.job.nesting", ServiceName: "test-service"}, func(ctx context.Context) error {
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

	serviceNameAttr, ok := spanAttr(jobSpan, "service_name")
	require.True(t, ok, "job span must carry a service_name attribute")
	require.Equal(t, "test-service", serviceNameAttr.AsString())

	deploymentEnv, ok := spanAttr(jobSpan, "deployment.environment")
	require.True(t, ok, "job span must carry the deployment.environment attribute from OTEL_RESOURCE_ATTRIBUTES")
	require.Equal(t, wantDeploymentEnvironment, deploymentEnv.AsString())
	stack, ok := spanAttr(jobSpan, "stack")
	require.True(t, ok, "job span must carry the stack attribute from OTEL_RESOURCE_ATTRIBUTES")
	require.Equal(t, wantStack, stack.AsString())
}

func TestRunRecordsErrorOnSpan(t *testing.T) {
	wantErr := errors.New("boom")
	err := job.Run(context.Background(), job.Desc{Name: "test.job.err", ServiceName: "test-service"}, func(ctx context.Context) error {
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
	const serviceName = "test-service"

	require.NoError(t, job.Run(context.Background(), job.Desc{Name: jobName, ServiceName: serviceName}, func(ctx context.Context) error {
		return nil
	}))

	failing := fmt.Errorf("wrapped: %w", errors.New("root cause"))
	err := job.Run(context.Background(), job.Desc{Name: jobName, ServiceName: serviceName}, func(ctx context.Context) error {
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
	serviceNameAttr, ok := errDP.Attributes.Value(attribute.Key("service_name"))
	require.True(t, ok, "job.errors data point must carry a service_name attribute")
	require.Equal(t, serviceName, serviceNameAttr.AsString())
	requireResourceAttrs(t, errDP.Attributes)

	inflightSum, ok := byName["job.inflight"].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	inflightDP, found := dataPointForJob(t, inflightSum, jobName)
	require.True(t, found, "expected a job.inflight data point for %q", jobName)
	require.Equal(t, int64(0), inflightDP.Value, "inflight must return to zero once both runs end")
	serviceNameAttr, ok = inflightDP.Attributes.Value(attribute.Key("service_name"))
	require.True(t, ok, "job.inflight data point must carry a service_name attribute")
	require.Equal(t, serviceName, serviceNameAttr.AsString())
	requireResourceAttrs(t, inflightDP.Attributes)
}

func TestSetResourceAttributesMergesWithEnvResourceAttrs(t *testing.T) {
	const jobName = "test.job.configured-resource-attrs"
	const serviceName = "test-service"

	observe.SetResourceAttributes(attribute.String("region", "us-east-1"))
	t.Cleanup(func() { observe.SetResourceAttributes() })

	require.NoError(t, job.Run(context.Background(), job.Desc{Name: jobName, ServiceName: serviceName}, func(ctx context.Context) error {
		return nil
	}))

	jobSpans := spansNamed(jobName)
	require.Len(t, jobSpans, 1)

	regionAttr, ok := spanAttr(jobSpans[0], "region")
	require.True(t, ok, "job span must carry the configured region attribute")
	require.Equal(t, "us-east-1", regionAttr.AsString())

	deploymentEnv, ok := spanAttr(jobSpans[0], "deployment.environment")
	require.True(t, ok, "job span must still carry the OTEL_RESOURCE_ATTRIBUTES-derived attribute alongside the configured one")
	require.Equal(t, wantDeploymentEnvironment, deploymentEnv.AsString())

	byName := collectMetrics(t)
	inflightSum, ok := byName["job.inflight"].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	inflightDP, found := dataPointForJob(t, inflightSum, jobName)
	require.True(t, found, "expected a job.inflight data point for %q", jobName)
	regionAttr, ok = inflightDP.Attributes.Value(attribute.Key("region"))
	require.True(t, ok, "job.inflight data point must carry the configured region attribute")
	require.Equal(t, "us-east-1", regionAttr.AsString())
	requireResourceAttrs(t, inflightDP.Attributes)
}

// A resource attribute named service_name reaches the same key baseAttrs sets
// from Desc.ServiceName. Because resource attributes are appended after Desc's
// and attribute.NewSet keeps the last duplicate, an unfiltered one silently
// replaces the component name in the SDK -- before any exporter is involved --
// and every job gets attributed to the process instead of the plugin.
func TestConfiguredResourceAttrsCannotOverrideJobServiceName(t *testing.T) {
	const jobName = "test.job.service-name-collision"
	const serviceName = "test-service"

	observe.SetResourceAttributes(
		attribute.String("service_name", "the-whole-process"),
		attribute.String("job_name", "someone-elses-job"),
	)
	t.Cleanup(func() { observe.SetResourceAttributes() })

	require.NoError(t, job.Run(context.Background(), job.Desc{Name: jobName, ServiceName: serviceName}, func(ctx context.Context) error {
		return nil
	}))

	jobSpans := spansNamed(jobName)
	require.Len(t, jobSpans, 1)

	serviceNameAttr, ok := spanAttr(jobSpans[0], "service_name")
	require.True(t, ok, "job span must carry a service_name attribute")
	require.Equal(t, serviceName, serviceNameAttr.AsString(),
		"service_name must come from Desc.ServiceName, not from a configured resource attribute")

	jobNameAttr, ok := spanAttr(jobSpans[0], "job_name")
	require.True(t, ok, "job span must carry a job_name attribute")
	require.Equal(t, jobName, jobNameAttr.AsString(),
		"job_name must come from Desc.Name, not from a configured resource attribute")

	byName := collectMetrics(t)
	inflightSum, ok := byName["job.inflight"].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	inflightDP, found := dataPointForJob(t, inflightSum, jobName)
	require.True(t, found, "expected a job.inflight data point for %q", jobName)
	metricServiceName, ok := inflightDP.Attributes.Value(attribute.Key("service_name"))
	require.True(t, ok, "job.inflight data point must carry a service_name attribute")
	require.Equal(t, serviceName, metricServiceName.AsString(),
		"metrics must be attributed to the component that ran the job, not the process")
}

func TestEndIsSafeToCallOnceAndIgnoresNilJob(t *testing.T) {
	var nilJob *job.Job
	require.NotPanics(t, func() { nilJob.End(nil) })
}

func TestEndIsIdempotentUnderConcurrency(t *testing.T) {
	const jobName = "test.job.concurrent-end"

	_, j := job.Start(context.Background(), job.Desc{Name: jobName, ServiceName: "test-service"})

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

func TestRunEndsJobEvenIfFnPanics(t *testing.T) {
	const jobName = "test.job.panic"
	const serviceName = "test-service"

	var recovered any
	func() {
		defer func() { recovered = recover() }()

		_ = job.Run(context.Background(), job.Desc{Name: jobName, ServiceName: serviceName}, func(ctx context.Context) error {
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

		_ = job.Run(context.Background(), job.Desc{Name: jobName, ServiceName: "test-service"}, nil)
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
