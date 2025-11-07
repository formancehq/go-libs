package audit

import (
	"net/http"

	"github.com/formancehq/go-libs/v3/logging"
	"github.com/go-chi/chi/v5"
)

// DecorateRouter adds audit middleware to the router if the client is available
// This is a convenience function for use in router decorators
//
// Example usage in cmd/serve.go:
//
//	fx.Decorate(func(params struct {
//	    fx.In
//	    Handler     chi.Router
//	    AuditClient *audit.PublisherClient `optional:"true"`
//	    Logger      logging.Logger
//	}) chi.Router {
//	    return audit.DecorateRouter(params.Handler, params.AuditClient, params.Logger)
//	})
func DecorateRouter(router chi.Router, client *PublisherClient, logger logging.Logger) chi.Router {
	if client == nil {
		return router // Audit disabled
	}

	logger.Infof("Adding audit middleware to router")
	router.Use(HTTPMiddlewareWithPublisher(client))
	return router
}

// DecorateHandler adds audit middleware to an http.Handler if the client is available
// This is useful for non-chi routers
func DecorateHandler(handler http.Handler, client *PublisherClient, logger logging.Logger) http.Handler {
	if client == nil {
		return handler // Audit disabled
	}

	logger.Infof("Adding audit middleware to handler")
	return HTTPMiddlewareWithPublisher(client)(handler)
}
