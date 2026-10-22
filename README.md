# go-libs

Shared command configuration: [environment binding diagnostics](docs/ENVIRONMENT.md).

## JWT authentication scopes

JWT authentication always checks service scopes when enabled. Safe HTTP methods
(`GET`, `HEAD`, `OPTIONS`, `TRACE`) require `<service>:read` or `<service>:write`.
Other methods require `<service>:write`. Additional checks, including endpoint
scopes from OpenAPI, still apply and do not replace service scopes.

When upgrading, remove `--auth-check-scopes`, `Config.CheckScopes`, and the
`checkScopes` boolean argument to `NewJWTAuth`. Configure the service name and
issue tokens with its required scopes. Disabling authentication with
`--auth-enabled=false` still disables authentication and scope checks.
