# Contributing

## Before you start

Read [ARCHITECTURE.md](./ARCHITECTURE.md) to understand the project structure and rules.

## Dependency rules checklist

Before submitting a PR, verify:

- [ ] Your package does **not** import `go.uber.org/fx` (unless it lives under `fx/`)
- [ ] Your package only imports from its own layer or lower layers
- [ ] Leaf packages (`types/`, `query/`, `errors/`) have no internal `go-libs` imports
- [ ] New FX modules live under `fx/<domain>fx/`
- [ ] No package name ends with `utils`

## Package structure conventions

Each package should expose:

```go
// Constructor — pure, no side effects beyond allocation
func New(cfg Config) (*Thing, error)

// Config struct with sensible defaults
type Config struct { ... }

// If CLI flags are needed:
func AddFlags(flags *pflag.FlagSet) { ... }
```

## FX module conventions

FX modules under `fx/` follow this pattern:

```go
package somefx

func Module() fx.Option {
    return fx.Module("name",
        fx.Provide(pure.New),
        fx.Invoke(func(lc fx.Lifecycle, t *pure.Thing) {
            lc.Append(fx.Hook{
                OnStart: func(ctx context.Context) error { return t.Start(ctx) },
                OnStop:  func(ctx context.Context) error { return t.Stop(ctx) },
            })
        }),
    )
}
```

Key rules:
- Module name matches the package concept (e.g., `"traces"`, `"httpserver"`)
- Use `fx.Provide` for constructors, `fx.Invoke` for lifecycle hooks
- Never put business logic in FX modules

## Testing

- Unit tests go alongside the code (`foo_test.go` next to `foo.go`)
- Integration tests that need containers use helpers from `testing/platform/`
- Test service scaffolding is in `testing/testservice/`
