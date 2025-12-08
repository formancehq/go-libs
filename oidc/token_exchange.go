package oidc

import (
	"slices"
)

// ValidateTokenExchangeRequest validates a TokenExchangeRequest according to RFC 8693
func ValidateTokenExchangeRequest(req *TokenExchangeRequest) error {
	// Validate grant type
	if req.GrantType != GrantTypeTokenExchange {
		return ErrUnsupportedGrantType().WithDescription("grant_type must be %s", GrantTypeTokenExchange)
	}

	// Validate required parameters
	if req.SubjectToken == "" {
		return ErrInvalidRequest().WithDescription("subject_token is required")
	}

	if req.SubjectTokenType == "" {
		return ErrInvalidRequest().WithDescription("subject_token_type is required")
	}

	// Validate subject_token_type
	validSubjectTokenTypes := []TokenType{
		AccessTokenType,
		// RefreshTokenType, // Can be enabled if needed
		// IDTokenType,      // Can be enabled if needed
		// JWTTokenType,     // Can be enabled if needed
	}
	if !slices.Contains(validSubjectTokenTypes, req.SubjectTokenType) {
		return ErrInvalidRequest().WithDescription("invalid subject_token_type: %s", req.SubjectTokenType)
	}

	// Set default requested_token_type if not provided
	requestedTokenType := req.RequestedTokenType
	if requestedTokenType == "" {
		requestedTokenType = AccessTokenType
	}

	// Validate requested_token_type
	validRequestedTokenTypes := []TokenType{
		AccessTokenType,
		// RefreshTokenType, // Can be enabled if needed
	}
	if !slices.Contains(validRequestedTokenTypes, requestedTokenType) {
		return ErrInvalidRequest().WithDescription("invalid requested_token_type: %s", requestedTokenType)
	}

	// Validate actor_token and actor_token_type if provided
	if req.ActorToken != "" && req.ActorTokenType == "" {
		return ErrInvalidRequest().WithDescription("actor_token_type is required when actor_token is provided")
	}

	return nil
}

// ValidateTokenExchangeScopes validates that requested scopes are within the allowed scopes
func ValidateTokenExchangeScopes(requestedScopes []string, allowedScopes []string) error {
	for _, requestedScope := range requestedScopes {
		if !slices.Contains(allowedScopes, requestedScope) {
			return ErrInvalidRequest().WithDescription("requested scope '%s' is not allowed", requestedScope)
		}
	}
	return nil
}

// GetRequestedScopes extracts and returns the requested scopes from a TokenExchangeRequest
// If no scopes are requested, returns the default scopes provided
func GetRequestedScopes(req *TokenExchangeRequest, defaultScopes []string) []string {
	if len(req.Scopes) > 0 {
		return req.Scopes
	}
	return defaultScopes
}
