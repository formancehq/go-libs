package observe

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

// TestResourceAttributesDropsCollidingKeys pins the guarantee that resource
// identity duplicated onto data points can never overwrite a label the
// instrumentation sets itself. Not parallel: it mutates process-wide state.
func TestResourceAttributesDropsCollidingKeys(t *testing.T) {
	t.Cleanup(func() { SetResourceAttributes() })

	SetResourceAttributes(
		// Every spelling that lands on an attribute pkg/observe/job sets
		// itself, dotted and underscored.
		attribute.String("component.name", "the-whole-process"),
		attribute.String("component_name", "the-whole-process"),
		attribute.String("job.name", "someone-elses-job"),
		attribute.String("job_name", "someone-elses-job"),
		// Neither of these collides with anything job sets.
		attribute.String("service.name", "the-whole-process"),
		attribute.String("service.version", "1.0.0"),
		attribute.String("stack", "connectivity"),
	)

	got := ResourceAttributes()

	for _, dropped := range []attribute.Key{"component.name", "component_name", "job.name", "job_name"} {
		for _, kv := range got {
			require.NotEqual(t, dropped, kv.Key,
				"%q reaches an attribute pkg/observe/job sets from Desc -- attribute.NewSet keeps the last duplicate, and baseAttrs appends resource attributes after Desc's, so leaving it in silently replaces the per-job value", dropped)
		}
	}

	require.Contains(t, got, attribute.String("stack", "connectivity"),
		"non-colliding resource attributes must still reach data points")
	require.Contains(t, got, attribute.String("service.version", "1.0.0"),
		"service.version does not collide with any attribute job sets, so it must survive")
	require.Contains(t, got, attribute.String("service.name", "the-whole-process"),
		"service.name identifies the process, a complementary identity to component_name, so it must survive to reach the data point alongside it")
}

func TestAppendNonColliding(t *testing.T) {
	t.Parallel()

	src := []attribute.KeyValue{
		attribute.String("component.name", "dropped"),
		attribute.String("stack", "kept"),
	}

	require.Equal(t,
		[]attribute.KeyValue{attribute.String("stack", "kept")},
		appendNonColliding(nil, src))

	require.Equal(t, src, []attribute.KeyValue{
		attribute.String("component.name", "dropped"),
		attribute.String("stack", "kept"),
	}, "the source slice must not be mutated")
}

func TestParseResourceAttrs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  string
		want []attribute.KeyValue
	}{
		{
			name: "empty",
			raw:  "",
			want: nil,
		},
		{
			name: "single pair",
			raw:  "deployment.environment=dev",
			want: []attribute.KeyValue{attribute.String("deployment.environment", "dev")},
		},
		{
			name: "multiple pairs",
			raw:  "deployment.environment=dev,stack=connectivity",
			want: []attribute.KeyValue{
				attribute.String("deployment.environment", "dev"),
				attribute.String("stack", "connectivity"),
			},
		},
		{
			name: "surrounding whitespace is trimmed",
			raw:  "  deployment.environment = dev , stack = connectivity ",
			want: []attribute.KeyValue{
				attribute.String("deployment.environment", "dev"),
				attribute.String("stack", "connectivity"),
			},
		},
		{
			name: "entry without a separator is skipped",
			raw:  "deployment.environment=dev,bare,stack=connectivity",
			want: []attribute.KeyValue{
				attribute.String("deployment.environment", "dev"),
				attribute.String("stack", "connectivity"),
			},
		},
		{
			// An empty key would otherwise be attached to every span and
			// metric data point this package's callers emit.
			name: "entry with an empty key is skipped",
			raw:  "=orphan,stack=connectivity",
			want: []attribute.KeyValue{attribute.String("stack", "connectivity")},
		},
		{
			name: "entry whose key is only whitespace is skipped",
			raw:  "   =orphan,stack=connectivity",
			want: []attribute.KeyValue{attribute.String("stack", "connectivity")},
		},
		{
			name: "only malformed entries yields nothing",
			raw:  "=orphan, =another,bare",
			want: nil,
		},
		{
			name: "empty value is kept",
			raw:  "stack=",
			want: []attribute.KeyValue{attribute.String("stack", "")},
		},
		{
			name: "value containing a separator is kept whole",
			raw:  "url=https://example.com/a=b",
			want: []attribute.KeyValue{attribute.String("url", "https://example.com/a=b")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, parseResourceAttrs(tc.raw))
		})
	}
}
