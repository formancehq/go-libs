package audit

import (
	"context"
	"fmt"
	"strings"

	"github.com/formancehq/go-libs/v3/auth"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

// ExtractIdentity extracts the identity for audit logging.
// It tries multiple sources in order:
// 1. Claims from the request context (if auth middleware stored them)
// 2. Parsing the JWT from the Authorization header (fallback for backwards compatibility)
//
// This approach is secure because:
// - When claims are in context, they've been validated by the auth middleware
// - When parsing JWT, it's after the auth middleware has already validated it
func ExtractIdentity(ctx context.Context, authorizationHeader string, logger *zap.Logger) string {
	// Try to get identity from context first (preferred method)
	claims := auth.GetClaimsFromContext(ctx)
	if claims != nil && claims.Subject != "" {
		return claims.Subject
	}

	// Fallback: parse JWT from header (for backwards compatibility)
	if authorizationHeader != "" {
		return ExtractJWTIdentity(authorizationHeader, logger)
	}

	return ""
}

// ExtractJWTIdentity extracts the "sub" claim from a JWT token in the Authorization header.
//
// DEPRECATED: Use ExtractIdentity instead, which prefers claims from the request context.
//
// SECURITY NOTE: This function uses ParseUnverified intentionally because:
// 1. The JWT has already been validated by the authentication middleware upstream
// 2. We only need the subject for audit logging purposes
// 3. Re-validating the signature here would be redundant and require storing keys
//
// IMPORTANT: This function should ONLY be called after the authentication middleware
// has validated the token. Never use this function for authorization decisions.
func ExtractJWTIdentity(authorizationHeader string, logger *zap.Logger) string {
	if authorizationHeader == "" {
		return ""
	}

	if !strings.HasPrefix(strings.ToLower(authorizationHeader), "bearer ") {
		return ""
	}

	// Remove "Bearer " prefix (case-insensitive)
	tokenString := strings.Replace(strings.Replace(authorizationHeader, "Bearer ", "", 1), "bearer ", "", 1)

	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		logger.Error("failed to parse JWT token for audit", zap.Error(err))
		return ""
	}

	if token == nil {
		return ""
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		logger.Error("failed to extract claims from JWT token")
		return ""
	}

	return fmt.Sprint(claims["sub"])
}
