package observefx_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/formancehq/go-libs/v5/pkg/fx/observefx"
	"github.com/formancehq/go-libs/v5/pkg/observe"
	"github.com/formancehq/go-libs/v5/pkg/observe/job"
)

// job.go's package-init tracer only re-delegates correctly to the *first*
// SetTracerProvider call made afterwards, so this whole file installs one
// recording provider here, matching job_test.go's approach.
var spanRecorder = tracetest.NewSpanRecorder()

func init() {
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder)))
}

func TestResourceModuleRegistersResourceAttributesWithJob(t *testing.T) {
	t.Cleanup(func() { observe.SetResourceAttributes() })

	app := fxtest.New(t,
		observefx.ResourceModule(observe.Config{
			ServiceName:        "resource-test",
			ResourceAttributes: []string{"stack.id=stack-123"},
		}),
		fx.NopLogger,
	)
	app.RequireStart()
	defer app.RequireStop()

	const jobName = "resource-test.job"
	_, j := job.Start(t.Context(), job.Desc{Name: jobName, ServiceName: "resource-test"})
	j.End(nil)

	var jobSpan sdktrace.ReadOnlySpan
	for _, s := range spanRecorder.Ended() {
		if s.Name() == jobName {
			jobSpan = s
		}
	}
	require.NotNil(t, jobSpan, "expected the job span to have been recorded")

	var stackIDAttr attribute.Value
	var found bool
	for _, a := range jobSpan.Attributes() {
		if a.Key == "stack.id" {
			stackIDAttr, found = a.Value, true
		}
	}
	require.True(t, found, "job span must carry the resource attribute configured via observe.Config.ResourceAttributes")
	require.Equal(t, "stack-123", stackIDAttr.AsString())
}

// The SDK resource always carries service.name -- BuildResource sets it from
// Config.ServiceName, and resource.Default() supplies one regardless. Copying
// it onto job data points would collide with the service_name attribute job
// sets from Desc.ServiceName: both normalize to the same Prometheus label, so
// the process-wide name could overwrite the per-component one and attribute
// every job to the process instead of the plugin that ran it.
func TestResourceModuleDoesNotOverrideJobServiceName(t *testing.T) {
	t.Cleanup(func() { observe.SetResourceAttributes() })

	app := fxtest.New(t,
		observefx.ResourceModule(observe.Config{
			ServiceName:        "the-whole-process",
			ResourceAttributes: []string{"stack.id=stack-456"},
		}),
		fx.NopLogger,
	)
	app.RequireStart()
	defer app.RequireStop()

	const jobName = "service-name-collision.job"
	_, j := job.Start(t.Context(), job.Desc{Name: jobName, ServiceName: "myplugin"})
	j.End(nil)

	var jobSpan sdktrace.ReadOnlySpan
	for _, s := range spanRecorder.Ended() {
		if s.Name() == jobName {
			jobSpan = s
		}
	}
	require.NotNil(t, jobSpan, "expected the job span to have been recorded")

	attrs := map[attribute.Key]string{}
	for _, a := range jobSpan.Attributes() {
		attrs[a.Key] = a.Value.AsString()
	}

	require.NotContains(t, attrs, attribute.Key("service.name"),
		"the resource's service.name must not be copied onto job data points -- it collapses onto the same label as service_name at export")
	require.Equal(t, "myplugin", attrs["service_name"],
		"service_name must still identify the component that ran the job")
	require.Equal(t, "stack-456", attrs["stack.id"],
		"non-colliding configured resource attributes must still reach the job")
}
