# Environment binding diagnostics

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
