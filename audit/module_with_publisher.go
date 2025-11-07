package audit

import (
	"context"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/formancehq/go-libs/v3/logging"
	"go.uber.org/fx"
)

// ModuleWithPublisherConfig holds configuration for audit module
type ModuleWithPublisherConfig struct {
	AppName          string   // Application name (e.g., "ledger", "payments", "wallets")
	Enabled          bool     // Enable audit logging
	Topic            string   // Audit topic name (e.g., "organization-stack.audit")
	MaxBodySize      int64    // Maximum request/response body size to capture
	ExcludedPaths    []string // HTTP paths to exclude from auditing
	SensitiveHeaders []string // HTTP headers to sanitize
}

// ModuleWithPublisher creates an Fx module for audit that reuses the existing publisher
//
// This module is designed for Formance services that already have a publisher configured
// (via PUBLISHER_NATS_* or PUBLISHER_KAFKA_* environment variables). It automatically:
//   - Loads configuration from CLI flags (--audit-enabled, --audit-max-body-size, etc.)
//   - Derives the audit topic from PUBLISHER_TOPIC_MAPPING
//   - Reuses the existing message.Publisher from the DI container
//   - Provides a *PublisherClient that can be injected into router decorators
//
// Usage in a service:
//
//	options := []fx.Option{
//	    publish.FXModuleFromFlags(cmd, debug),  // Creates publisher
//	    audit.ModuleWithPublisher(audit.ModuleWithPublisherConfig{
//	        AppName: "ledger",
//	    }),
//	}
//
// Then in your router decorator:
//
//	fx.Decorate(func(params struct {
//	    fx.In
//	    Router       chi.Router
//	    AuditClient  *audit.PublisherClient `optional:"true"`
//	}) chi.Router {
//	    if params.AuditClient != nil {
//	        router.Use(audit.HTTPMiddlewareWithPublisher(params.AuditClient))
//	    }
//	    return router
//	})
func ModuleWithPublisher(cfg ModuleWithPublisherConfig) fx.Option {
	return fx.Module("audit",
		fx.Provide(func(
			publisher message.Publisher,
			logger logging.Logger,
		) (*PublisherClient, error) {
			// If audit is disabled, return nil (will be optional in DI)
			if !cfg.Enabled {
				return nil, nil
			}

			// Create client with existing publisher
			client := NewClientWithPublisher(
				publisher,
				cfg.Topic,
				cfg.AppName,
				cfg.MaxBodySize,
				cfg.ExcludedPaths,
				cfg.SensitiveHeaders,
				logger,
			)

			// Log audit configuration at startup
			logger.Infof("Audit logging enabled (topic=%s, max-body-size=%d, excluded-paths=%d)",
				cfg.Topic, cfg.MaxBodySize, len(cfg.ExcludedPaths))

			return client, nil
		}),

		// Lifecycle hook for cleanup (no-op since we don't own the publisher)
		fx.Invoke(func(lc fx.Lifecycle, client *PublisherClient) {
			if client == nil {
				return // Audit disabled
			}

			lc.Append(fx.Hook{
				OnStop: func(ctx context.Context) error {
					return client.Close()
				},
			})
		}),
	)
}
