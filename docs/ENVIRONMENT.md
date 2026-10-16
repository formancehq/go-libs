# Environment binding

## Legacy binding diagnostics

Malformed environment values returned by `service.BindEnvToFlagSetWithError`,
`BindEnvToCommandWithError`, or `service.Execute` are reported through
`*service.EnvironmentBindingError`. Its `EnvironmentVariable` and `FlagName`
fields identify the setting. By default, the error message omits the rejected
value. This applies to every typed flag because configuration values can be
sensitive even when a flag name does not identify it as a credential.

Use `errors.As` to classify the binding error. Its `Unwrap` preserves the original
parser cause for `errors.As` and `errors.Is`; that cause can include the value,
so do not log an unwrapped parser error. Existing callers that inspect parser
causes retain that ability.

With `--debug` enabled, the error message includes the full parser cause and can
contain sensitive configuration values. `service.Execute` uses the selected
command's effective debug flag after CLI parsing, including an inherited
persistent flag. Direct binding helpers use the debug flag already set on the
flag set or command. Bare `DEBUG` environment variables do not enable this mode.

Parsing, collection handling, whitespace, explicit CLI precedence, bare `DEBUG`
handling, and selected-command rejection are unchanged.

## Exact environment binding

`pkg/service` offers opt-in exact environment binding:

- `BindEnvToFlagSetExact(flags)` binds a parsed flag set.
- `BindEnvToSelectedCommandExact(cmd)` binds the selected command's local and
  inherited persistent flags. Local flags shadow inherited flags. Other commands
  and ancestor-local flags are excluded.

Call these functions after CLI parsing and before starting components. For Cobra,
call the selected-command function from an `Args` validator, before any existing
argument validation. This also binds before persistent pre-run hooks and required
flag checks. Check and return its error. Cobra initialization callbacks run earlier
and cannot use these environment values. Call `cmd.Execute()` yourself; do not
combine exact binding with `service.Execute`, which retains legacy early binding.

For example, bind before validating a command that accepts no positional arguments:

```go
cmd.Args = func(selected *cobra.Command, args []string) error {
    if err := service.BindEnvToSelectedCommandExact(selected); err != nil {
        return err
    }
    return cobra.NoArgs(selected, args)
}
```

Flag names map to uppercase environment names with hyphens replaced by underscores.
CLI-changed flags take precedence, even if the environment value is malformed.
Unset variables retain defaults; present empty strings override string defaults.
Strings retain all whitespace. Other scalar values use their pflag parsers without
trimming. Both `stringSlice` and `stringArray` require JSON arrays of strings:

```sh
export ENDPOINTS='["one,two","three four",""]'
```

`[]` clears the default collection; `[""]` preserves one empty item. `null`,
non-string items, malformed JSON and CSV/space-delimited lists are rejected.
Successful bindings set `Changed` and participate in `Visit` and `NFlag`, like
`FlagSet.Set`. A subsequent binding skips those flags. Binding is not transactional;
earlier successful flags remain bound if a later flag fails. Configure and execute
command instances serially; pflag command configuration is not concurrency safe.

### Lossless collection retrieval

The binding guarantee applies to the stored values. To preserve every collection
item, callers must read the typed pointer registered with pflag, or assert
`pflag.SliceValue` on the original flag value and call `GetSlice()` directly:

```go
items := cmd.Flag("endpoints").Value.(pflag.SliceValue).GetSlice()
```

With the pinned pflag version, `GetStringSlice` and `GetStringArray` reconstruct
values through a CSV round trip. They return an empty collection for `[""]` and
normalize CRLF within an item to LF. `Value.String()` is also a CSV display
representation, not a lossless serialization. Exact binding does not change these
third-party getters. Use the direct retrieval paths above for configuration and
secret values that require exact item preservation.

### Safe errors

Failures return `*EnvironmentBindingError` with the environment and flag names and
wrap `ErrInvalidEnvironmentValue`. Parser errors and their input are discarded,
including in debug mode and through error unwrapping. `DEBUG` is always ignored;
use the explicit CLI flag. Custom pflag values must not log their own input.

The legacy binders and `service.Execute` retain their existing parsing, empty-value,
debug disclosure and command-initialization behavior. Exact binding is an additive
API. It is available only in source containing this change; no containing published
release is asserted by this document.
