# Environment binding diagnostics

Malformed environment values returned by `service.BindEnvToFlagSetWithError`,
`BindEnvToCommandWithError`, or `service.Execute` are reported through
`*service.EnvironmentBindingError`. Its `EnvironmentVariable` and `FlagName`
fields identify the setting without including the rejected value in the error
message. This applies to every typed flag because configuration values can be
sensitive even when a flag name does not identify it as a credential.

Use `errors.As` to classify the binding error. Its `Unwrap` preserves the original
parser cause for `errors.As` and `errors.Is`; that cause can include the value,
so do not log an unwrapped parser error. Existing callers that inspect parser
causes retain that ability. Error messages now omit the rejected value.

Parsing, collection handling, whitespace, explicit CLI precedence, bare `DEBUG`
handling, and selected-command rejection are unchanged.
