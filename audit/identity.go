package audit

import (
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

// ExtractJWTIdentity extracts the "sub" claim from a JWT token in the Authorization header.
// This uses ParseUnverified because we only need the subject for audit logging,
// not to validate the token (validation happens elsewhere in the auth chain).
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
