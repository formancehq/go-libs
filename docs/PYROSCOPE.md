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
values are trimmed; malformed pairs and unknown profile names are skipped.
An empty profile collection keeps DefaultConfig's types; a nonempty collection
containing no recognized types becomes empty and is subject to the SDK's own
fallback. Collection reads use `pflag.SliceValue.GetSlice()` directly, preserving
CRLF and commas before the documented trimming/selection. Profile names are
case-insensitive. No environment binding is performed by this package.

For plain library use, `NewHook(cfg, logger)` allocates without side effects.
Call `cfg.SetupRuntimeProfiling()` when process-wide sampling should take effect,
then use `hook.OnStart(ctx)` and `hook.OnStop(ctx)`. The hook snapshots mutable
configuration, starts once, and tolerates stop before start or repeated stop.
Start errors fail startup. Stop errors are logged at Info and shutdown succeeds.
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
