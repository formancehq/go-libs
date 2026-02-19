package profiling

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v4/httpserver"
)

// NewModule creates an HTTP server serving pprof "/debug" endpoints on bindPort
func NewModule(bindPort string, enable bool) fx.Option {
	if !enable {
		return fx.Options()
	}
	return fx.Options(
		fx.Invoke(fx.Annotate(func(m *chi.Mux, lc fx.Lifecycle) {
			lc.Append(httpserver.NewHook(m, httpserver.WithAddress(bindPort)))
		}, fx.ParamTags(`name:"profilingRouter"`, ``))),
		fx.Provide(fx.Annotate(NewRouter, fx.ResultTags(`name:"profilingRouter"`))),
	)
}

func NewRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Mount("/debug", middleware.Profiler())
	return r
}
