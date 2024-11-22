package profiling

import (
	"github.com/formancehq/go-libs/httpserver"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"go.uber.org/fx"
)

// NewModule creates an HTTP server serving pprof "/debug" endpoints on bindPort
func NewModule(bindPort string, enable bool) fx.Option {
	if !enable {
		return fx.Options()
	}
	return fx.Options(
		fx.Invoke(func(m *chi.Mux, lc fx.Lifecycle) {
			lc.Append(httpserver.NewHook(m, httpserver.WithAddress(bindPort)))
		}),
		fx.Provide(NewRouter),
	)
}

func NewRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Mount("/debug", middleware.Profiler())
	return r
}
