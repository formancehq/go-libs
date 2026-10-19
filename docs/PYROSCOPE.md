# Native continuous profiling

`pkg/observe/pyroscope` supports the native Grafana Go SDK, separately from
`observe/profiling`'s pprof HTTP listener and `observe/pyroscopetraces`' span labels.
It starts no listener. Compile with `-tags pyroscope` to include the SDK. Without
the tag, `AddFlags` registers nothing, `ConfigFromFlags` returns a disabled
configuration and `observefx.PyroscopeModule` is a no-op even if Enabled is true.

The enabled Config embeds `github.com/grafana/pyroscope-go.Config` directly.
`DefaultConfig` is disabled, targets `http://localhost:4040`, uploads every 15s,
and selects CPU, alloc objects/space, in-use objects/space, goroutines, mutex
count/duration and block count/duration. Positive mutex/block sampling defaults
are both 5. Non-positive rates leave the runtime setting alone; shutdown does
not restore process-wide sampling. Automatic GC remains enabled by default.

`AddFlags(*pflag.FlagSet)` registers `pyroscope-enabled`, `pyroscope-server-address`,
`pyroscope-application-name`, `pyroscope-auth-token`, `pyroscope-tenant-id`,
`pyroscope-basic-auth-user`, `pyroscope-basic-auth-password`, `pyroscope-upload-rate`,
`pyroscope-tags`, `pyroscope-profile-types`, `pyroscope-mutex-profile-fraction`,
`pyroscope-block-profile-rate` and `pyroscope-disable-gc-runs`. Credentials and
application name default to empty. Profile names in flag help are sorted.
It reuses the existing `otel-traces-provider-pyroscope-*` flags.

`ConfigFromFlags` accepts a flag set after CLI/environment parsing. Tag keys and
values are trimmed; empty keys, malformed pairs and unknown profile names are skipped.
An empty profile collection keeps DefaultConfig's types; a nonempty collection
containing no recognized types becomes empty and is subject to the SDK's own
fallback. Collection reads use `pflag.SliceValue.GetSlice()` directly, preserving
CRLF and commas before the documented trimming/selection. Profile names are
case-insensitive. No environment binding is performed by this package.

For plain library use, `NewHook(cfg, logger)` allocates without side effects.
Call `cfg.SetupRuntimeProfiling()` when process-wide sampling should take effect,
then use `hook.OnStart(ctx)` and `hook.OnStop(ctx)`. The hook snapshots mutable
configuration, starts once, and tolerates stop before start or repeated stop.
Negative upload durations fail startup before SDK work; zero retains the SDK
default. This validation also applies when the embedded SDK configuration sets
`DisableAutomaticResets`: the pinned SDK would replace the interval in that mode,
but the hook deliberately rejects a negative input consistently before SDK work.
The ticker panic applies when the SDK uses the negative interval, not when its
automatic-reset override replaces it. Start errors fail startup. Stop errors are
logged at Info and shutdown succeeds.
The native SDK Stop has no context API: joining uploads can exceed the caller's
deadline. Stop drains SDK work; it does not promise a fresh final snapshot for
every profile type. The SDK's ad-hoc address override remains supported.

`observefx.PyroscopeModule(cfg)` contains only Fx wiring. If enabled, it applies
positive sampling rates during graph construction, registers the hook and
composes the existing trace decorator. TagSpans and TagAllSpans do not tag traces
when Enabled is false. No other tracing-provider policy changes.

Config formatting/JSON and SDK log callbacks omit or mask configured credentials,
credential-bearing URLs/headers and sensitive tag values. Native start errors
are sanitized without retaining unsafe parser causes; stop diagnostics are also
sanitized. Secrets remain unchanged on SDK requests. Logging the raw embedded
SDK Config bypasses these protections; do not log it. The logger callback API
has no request context, so SDK callbacks use the process logger.

This is a source capability candidate. A containing published go-libs release
and application-specific source/operational qualification are required before
released adoption. It does not enable profiling in existing applications.

Native flag registration owns the existing correlation flags too: do not also
call `pyroscopetraces.AddFlags` on the same flag set. Similarly, select one trace
provider decorator; do not combine `PyroscopeModule` with another enabled
Pyroscope correlation decorator in the same Fx scope. The module composes that
correlation itself.

Configured Authorization/Proxy-Authorization credential components and opaque
address path segments selected by the URL redactor remain masked when an SDK
callback echoes only a token or rewrites the address to an upload endpoint.
These diagnostic transformations never alter SDK request credentials.


## OpenTelemetry Profiles

As of October 19, 2026, the [OpenTelemetry Profiles specification] is Alpha.
It defines a profile data model derived from pprof and supports OTLP transport.
The [official SDK status matrix] lists no Profiles support for Go.
The existing OpenTelemetry trace, metric and log exporters therefore do not
replace profile collection and export in these packages.

`observe/profiling` exposes pprof HTTP endpoints for local diagnostics or
periodic retrieval by an external collector. `observe/pyroscope` collects and
uploads through the native Grafana SDK; it does not implement OTLP Profiles
export. `observe/pyroscopetraces` links traces and profiles, without collecting
or uploading profiles. Grafana's [Go span profiles guide] configures the native
profiler separately from the OpenTelemetry trace correlation bridge.

[OpenTelemetry Profiles specification]: https://opentelemetry.io/docs/specs/otel/profiles/
[official SDK status matrix]: https://opentelemetry.io/status/
[Go span profiles guide]: https://grafana.com/docs/pyroscope/latest/configure-client/trace-span-profiles/go-span-profiles/

## Collection path and composition

The existing `profiling.NewRouter` and
`observefx.ProfilingModule(bindPort, enable)` expose pprof HTTP profiles; they
remain unchanged. An external collector can retrieve those profiles periodically
and forward them for historical analysis. The listener itself neither schedules
retrieval nor exports profiles. That path does not need this native uploader.

Choose the collection path for an application before enabling another profiler:

| Path | Application integration | Qualification and cost |
| --- | --- | --- |
| Existing pprof plus external periodic pull | Keep `profiling.AddFlags` and `ProfilingModule`; configure retrieval/export outside the application. | Qualify endpoint discovery, access controls, collection intervals, profile coverage and collector resources. This library does not deploy or configure that collector. |
| Proposed embedded SDK push | Opt into the `pyroscope` build, native flags and either `NewHook` or `PyroscopeModule`. | Qualify an observable outcome that needs embedded uploads, SDK footprint, process-wide profiling interactions, upload traffic and shutdown duration. |
| Application-owned native integration | Keep the native lifecycle in the application, optionally reusing narrower existing facilities. | Application maintainers retain SDK lifecycle/security maintenance; no shared-wrapper necessity is implied by code consolidation alone. |

The push proposal does not establish a need across independent services. A
preference for push, a shared maintenance owner or an extension of the existing
profiling API needs explicit justification; no such placement decision or
collector rollout is made by this package. No measured overhead or parity of
profile coverage, trace correlation or cost between these paths is claimed.

If the native module is chosen, register `pyroscope.AddFlags` once and provide
one base `trace.TracerProvider` in the Fx scope. `PyroscopeModule` already owns
the correlation decorator; do not also install an enabled
`PyroscopeTracesModule` or call `pyroscopetraces.AddFlags` on that flag set.
Duplicate flag registration panics, and duplicate Fx decoration fails graph
construction. Existing trace-only and pprof integrations remain valid when used
without the native module. The pprof listener is a separate facility, but that
does not establish that simultaneous SDK CPU collection and CPU pprof requests
are safe: qualify their process-wide ownership and the incident diagnostic path.

Sampling changes are process-wide and are not restored on stop; choose one
application owner for them. The native hook must be stopped by its lifecycle
owner, but its SDK stop cannot be bounded by context cancellation and does not
promise a final snapshot for every profile type. Profile tags/cardinality,
network/upload volume, memory and CPU overhead, and retained history costs need
consumer-specific measurement. Diagnostics masking does not make the raw
embedded SDK configuration safe to log. Whitespace normalization is used only
when extracting configured Authorization/Proxy-Authorization components for
masking; request header values are left untouched.
