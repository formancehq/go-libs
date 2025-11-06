package audit

import (
	"context"

	"github.com/formancehq/go-libs/v3/auth"
	"go.uber.org/zap"
)

// ExtractIdentity extracts the identity for audit logging from the request context.
//
// SECURITY: This function ONLY reads claims that have been validated and stored
// by the authentication middleware (auth.Middleware with AuthenticatorWithClaims).
// It will NOT attempt to parse or verify JWT tokens directly.
//
// IMPORTANT: The audit middleware MUST run after the auth middleware in your
// middleware chain, otherwise this will return an empty string.
//
// Example middleware order:
//
//	handler = authMiddleware(handler)    // First: validate JWT and store claims in context
//	handler = auditMiddleware(handler)   // Second: read claims from context
//
// Returns empty string if:
// - No claims found in context (auth middleware not run or disabled)
// - Claims exist but Subject is empty
func ExtractIdentity(ctx context.Context, logger *zap.Logger) string {
	// Get identity from context (validated by auth middleware)
	claims := auth.GetClaimsFromContext(ctx)
	if claims != nil && claims.Subject != "" {
		return claims.Subject
	}

	// No claims in context - auth middleware probably not configured
	logger.Debug("no claims found in context for audit identity extraction")
	return ""
}
