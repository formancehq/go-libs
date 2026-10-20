# Collect secret flag values

`pkg/service/secretflags.Collect` collects selected Cobra flag values for the
application's redaction setup. Call it after parsing and environment binding,
before startup or logging. A selector returns a `Source` for each effective flag;
its zero value ignores the flag. `Value` collects scalars and individual slice
members. `File` reads the configured path without trimming the contents.

The application owns annotations, flag naming, aliases, default file locations,
secret transformations and registration with its redactor. This package has no
global logger or redactor and does not inspect environment variables itself.

For a file source, `DefaultPath` applies only when the flag is empty. A readable
default is activated on that flag and marks it changed. `OptionalDefault` permits
a genuinely absent default; a dangling symlink in the final component or any
ancestor, or any other read failure, stops collection. Healthy symlinks retain
their normal filesystem behavior. A missing explicitly configured file always fails. Empty file
contents activate the path but contribute no redaction value.

Results contain unique non-empty values in first occurrence order. Local,
persistent and inherited flag sets are visited once per effective flag name.
Cobra may merge inherited flags into the local set before traversal; values
follow the effective sets rather than a separate application-defined sort.
No partial results are returned on failure. Earlier default activation is not
rolled back; abort startup on error. This operation is not safe against concurrent
flag mutation and does not provide transactional filesystem reads.

Diagnostics omit file paths and file contents. A custom setter's error is not
retained because it can echo rejected input. Selector errors are returned as-is;
selectors must never include secret values in their errors. Returned values are
secrets: register them privately and never print, serialize or attach them to
telemetry. File size and trust policy remain with the application selecting files.
