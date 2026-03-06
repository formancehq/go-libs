# Architecture

## Overview

`go-libs` is a collection of shared Go libraries used across Formance services.
The project is organized by **functional domain** (bounded contexts), with a strict
separation between pure logic and dependency injection wiring.

## Directory Structure

```
go-libs/
├── docs/                            # Project documentation
│
└── pkg/                             # All Go packages
    ├── types/                       # Leaf packages (zero internal dependency)
    │   ├── pointer/                 #   Pointer creation/dereference helpers
    │   ├── time/                    #   Time wrapper with JSON serialization
    │   ├── currency/                #   Currency types and formatting
    │   ├── metadata/                #   Generic metadata map
    │   └── collections/             #   Generic slice, map, linked list utilities
    │
    ├── errors/                      # Error helpers (exit codes, wrapping)
    ├── query/                       # Query expression builder
    │
    ├── observe/                     # Observability
    │   ├── log/                     #   Logger interface + adapters (zap, logrus, hclog)
    │   ├── traces/                  #   Tracer provider (OTLP gRPC/HTTP, stdout)
    │   ├── metrics/                 #   Meter provider (OTLP, in-memory)
    │   ├── resource.go              #   Shared OTLP resource builder
    │   └── profiling/               #   pprof debug server
    │
    ├── transport/                   # Network I/O
    │   ├── serverport/              #   Server address discovery and context binding
    │   ├── httpserver/              #   HTTP server (chi, middlewares, OTEL)
    │   ├── grpcserver/              #   gRPC server
    │   ├── httpclient/              #   HTTP client debug/tracing
    │   └── api/                     #   Response formatting, pagination, idempotency
    │
    ├── authn/                       # Authentication & authorization
    │   ├── jwt/                     #   JWT validation, keyset, middleware
    │   ├── oidc/                    #   OpenID Connect provider/client
    │   └── licence/                 #   Licence JWT validation
    │
    ├── storage/                     # Persistence
    │   ├── postgres/                #   PostgreSQL error mapping
    │   ├── bun/                     #   Bun ORM helpers
    │   │   ├── connect/             #     Connection setup
    │   │   ├── paginate/            #     Cursor/offset pagination
    │   │   ├── migrate/             #     Bun migrations
    │   │   ├── debug/               #     Query debug hook
    │   │   └── explain/             #     EXPLAIN hook
    │   ├── migrations/              #   Generic migration framework
    │   └── s3/                      #   S3 bucket helpers
    │
    ├── messaging/                   # Async messaging
    │   ├── publish/                 #   Multi-backend publisher (kafka, nats, sns, http)
    │   │   ├── circuit/             #     Circuit breaker
    │   │   └── topicmap/            #     Topic mapping decorator
    │   └── queue/                   #   Listener/consumer
    │
    ├── workflow/                    # Orchestration
    │   └── temporal/                #   Temporal client, worker, encryption
    │
    ├── cloud/                       # Cloud provider integrations
    │   └── aws/                     #   IAM credential loading
    │
    ├── service/                     # Service bootstrap
    │   ├── app.go                   #   Config, run loop, graceful shutdown
    │   └── health/                  #   Health check controller
    │
    ├── fx/                          # FX wiring (imports pure packages, wraps in fx.Module)
    │   ├── observefx/               #   Modules for observe/*
    │   ├── transportfx/             #   Modules for transport/*
    │   ├── authnfx/                 #   Modules for authn/*
    │   ├── storagefx/               #   Modules for storage/*
    │   ├── messagingfx/             #   Modules for messaging/*
    │   ├── workflowfx/              #   Modules for workflow/*
    │   ├── cloudfx/                 #   Modules for cloud/*
    │   └── servicefx/               #   Top-level service assembly
    │
    └── testing/                     # Test infrastructure
        ├── testservice/             #   Service scaffold for integration tests
        ├── docker/                  #   Container pool management
        ├── platform/                #   Database/broker containers (pg, nats, clickhouse...)
        └── api/                     #   HTTP assertion helpers
```

## Rules

### 1. Dependency layers

Packages are organized in strict layers. Dependencies flow **upward only** (a lower
layer never imports a higher layer).

```
Layer 0 (Leaf)    : pkg/types/  pkg/query/  pkg/errors/
Layer 1 (Core)    : pkg/observe/  pkg/storage/  pkg/authn/
Layer 2 (Infra)   : pkg/transport/  pkg/messaging/  pkg/workflow/  pkg/cloud/
Layer 3 (Bootstrap): pkg/service/
Layer 4 (Wiring)  : pkg/fx/
Layer 5 (Test)    : pkg/testing/
```

**A package may only import from its own layer or a lower layer.**

### 2. FX isolation

FX is **plumbing only**. The `fx/` directory is the sole consumer of `go.uber.org/fx`.

- Pure packages (`observe/`, `transport/`, etc.) **must not** import `go.uber.org/fx`.
- All `fx.Module`, `fx.Provide`, `fx.Invoke` calls live under `fx/`.
- Every feature must be usable with plain constructor calls, without FX.

Example:

```go
// pkg/observe/traces/provider.go — PURE
package traces

func NewTracerProvider(cfg Config, res *resource.Resource) (*sdktrace.TracerProvider, error) {
    // ...
}
```

```go
// pkg/fx/observefx/traces.go — WIRING
package observefx

func TracesModule() fx.Option {
    return fx.Module("traces",
        fx.Provide(traces.NewTracerProvider),
        fx.Invoke(func(lc fx.Lifecycle, tp *sdktrace.TracerProvider) {
            lc.Append(fx.Hook{
                OnStop: func(ctx context.Context) error {
                    return tp.Shutdown(ctx)
                },
            })
        }),
    )
}
```

### 3. Leaf packages have zero internal dependencies

Everything under `types/`, `query/`, and `errors/` must not import any other
`go-libs` package. They may only depend on the standard library or minimal
external dependencies.

### 4. No `*utils` naming

Package names should be descriptive nouns, not suffixed with `utils`.
Use `collections`, not `collectionutils`. Use `errors`, not `errorsutils`.

### 5. CLI flags

Packages that need CLI configuration expose an `AddFlags(*pflag.FlagSet)` function.
The `fx/` layer or `service/` calls these during setup. The pure package itself
never imports cobra or interacts with CLI frameworks directly beyond pflag.

`*cobra.Command` is only accepted in `service/`, `fx/`, and CLI builder packages
(e.g. `storage/bun/migrate/`). Pure packages accept `*pflag.FlagSet` and
`context.Context` instead.

### 6. Testing packages

`testing/` is a top-level directory for test infrastructure. Test files (`*_test.go`)
within each package are fine and encouraged. The `testing/` directory is for shared
test helpers and integration test scaffolding.

## Adding a new package

1. Determine which **layer** your package belongs to.
2. Place it under the correct domain directory.
3. Write pure constructors and functions — no FX.
4. If FX wiring is needed, add a module under `pkg/fx/<domain>fx/`.
5. Update this document if adding a new domain directory.

## FX isolation status

All packages comply with the FX isolation rule. The only exception is
`service/app.go` which is the bootstrap layer and uses FX by design.

## Module path

The module path is `github.com/formancehq/go-libs/v5`.
This is a breaking change from v4 to enforce the new structure.
