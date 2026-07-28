package observe

import (
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
)

// envResourceAttrs lazily parses OTEL_RESOURCE_ATTRIBUTES -- the same
// standard OTel env var (a comma-separated "key=value" list, e.g.
// "deployment.environment=dev,stack=connectivity") the process's OTel
// Resource is built from -- once per process.
var envResourceAttrs = sync.OnceValue(func() []attribute.KeyValue {
	return parseResourceAttrs(os.Getenv("OTEL_RESOURCE_ATTRIBUTES"))
})

// parseResourceAttrs splits an OTEL_RESOURCE_ATTRIBUTES-style value into
// attributes, skipping malformed entries rather than failing the process --
// a bad env var must not take a service down, and the attributes it carries
// are supplementary telemetry labels, not required config.
func parseResourceAttrs(raw string) []attribute.KeyValue {
	if raw == "" {
		return nil
	}

	var attrs []attribute.KeyValue
	for _, kv := range strings.Split(raw, ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}

		// An entry like "=value" cuts cleanly, so ok is true even though
		// there is no key -- guard separately, or the resulting empty-keyed
		// attribute rides along on every span and metric data point built
		// from ResourceAttributes.
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}

		attrs = append(attrs, attribute.String(k, strings.TrimSpace(v)))
	}

	return attrs
}

// collidingResourceKeys are resource attribute keys ResourceAttributes must
// never hand out, because each one reaches an attribute the instrumentation
// already sets from its own, more specific source. pkg/observe/job sets
// job_name from Desc.Name and service_name from Desc.ServiceName, so both
// spellings of each are filtered: the underscored form collides directly, and
// the dotted form collides once the OTel-to-Prometheus translation normalizes
// dots to underscores.
//
// Two distinct failure modes, both silent. An underscored key collides inside
// the SDK: baseAttrs appends these after Desc's own attributes and
// attribute.NewSet keeps the last duplicate, so the resource value simply
// replaces the per-job one before any exporter runs. A dotted key survives as
// far as the exporter and collapses onto the same Prometheus label there.
// Either way jobs get attributed to the process instead of the component that
// ran them, which is the collision Desc.Name already documents for the
// reserved "job" label.
//
// service.name in particular is unavoidable rather than merely possible: the
// SDK resource always carries one, since BuildResource sets it from
// Config.ServiceName and resource.Default() supplies a fallback regardless.
//
// Filtered here rather than at the observefx call site so the same guard
// covers OTEL_RESOURCE_ATTRIBUTES="service.name=..." and friends.
var collidingResourceKeys = map[attribute.Key]struct{}{
	"service.name": {},
	"service_name": {},
	"job.name":     {},
	"job_name":     {},
}

// appendNonColliding appends every attribute in src that is safe to duplicate
// onto a data point, leaving src untouched.
func appendNonColliding(dst, src []attribute.KeyValue) []attribute.KeyValue {
	for _, kv := range src {
		if _, colliding := collidingResourceKeys[kv.Key]; colliding {
			continue
		}
		dst = append(dst, kv)
	}

	return dst
}

// configuredResourceAttrs holds whatever attributes SetResourceAttributes was
// last called with -- nil until a caller opts in.
var configuredResourceAttrs atomic.Pointer[[]attribute.KeyValue]

// SetResourceAttributes registers the process's configured OTel resource
// attributes -- typically extracted via res.Attributes() from the
// *resource.Resource built by ResourceModule/BuildResource -- so
// ResourceAttributes() includes them. It exists because a process may
// configure its resource programmatically (Config.ResourceAttributes)
// instead of, or in addition to, the OTEL_RESOURCE_ATTRIBUTES env var, and
// both sources must reach any instrumentation that duplicates resource
// attributes onto individual data points (see ResourceAttributes).
//
// Call this once at startup, before any instrumentation that reads
// ResourceAttributes runs -- e.g. from an fx.Invoke alongside
// otel.SetTracerProvider/otel.SetMeterProvider. observefx.ResourceModule does
// this automatically.
func SetResourceAttributes(attrs ...attribute.KeyValue) {
	configuredResourceAttrs.Store(&attrs)
}

// ResourceAttributes returns the attributes an instrument should attach to
// every span/metric data point to keep resource-level identity (deployment
// environment, stack, pod identity, ...) queryable after export, even when
// the destination doesn't promote genuine OTel Resource attributes onto
// per-series labels on its own.
//
// This duplicates data that is, in principle, already on the OTel Resource.
// It exists because the collector's Prometheus remote-write path does not
// convert resource attributes into per-series labels unless
// resource_to_telemetry_conversion is explicitly enabled, which is
// collector-side config this package cannot rely on -- so attributes that
// must survive as VictoriaMetrics labels (e.g. deployment environment, stack)
// are attached directly on the data point instead.
//
// Combines OTEL_RESOURCE_ATTRIBUTES (env, always available) with whatever
// was registered via SetResourceAttributes (programmatic config, if any);
// the latter is appended after, so it wins on overlapping keys the same way
// resource.Merge favors the more specific resource.
//
// Keys that would collide with a label the instrumentation sets from a more
// specific source are dropped from both sources -- see collidingResourceKeys.
//
// Always returns a freshly allocated slice (len == cap), never a view over
// envResourceAttrs' memoized backing array -- appending directly onto that
// cached slice here would risk overwriting its spare capacity and corrupting
// every other caller's copy for the rest of the process's life, since
// sync.OnceValue hands out the exact same slice value to everyone.
func ResourceAttributes() []attribute.KeyValue {
	env := envResourceAttrs()

	var configuredLen int
	configured := configuredResourceAttrs.Load()
	if configured != nil {
		configuredLen = len(*configured)
	}

	attrs := make([]attribute.KeyValue, 0, len(env)+configuredLen)
	attrs = appendNonColliding(attrs, env)
	if configured != nil {
		attrs = appendNonColliding(attrs, *configured)
	}

	// Filtering leaves spare capacity behind, so clip it to keep the
	// len == cap guarantee below true.
	attrs = attrs[:len(attrs):len(attrs)]

	return attrs
}
