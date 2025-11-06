# Security Design: Audit Package

## Overview

The audit package is designed with security as a top priority. This document explains the security architecture and why certain design decisions were made.

## JWT Validation Architecture

### The Problem

When auditing HTTP requests, we need to extract the user identity from JWT tokens. However, parsing JWTs without validating their signature is a security vulnerability.

### Our Solution: Context-Based Claims

Instead of parsing JWTs in the audit middleware, we rely on **validated claims stored in the request context** by the authentication middleware.

```
Request Flow:
1. HTTP Request arrives
2. Auth Middleware: Validates JWT signature → Stores claims in context
3. Audit Middleware: Reads claims from context → Logs audit event
4. Application Handler: Processes request
```

### Key Security Properties

1. **Single Source of Truth**: JWT is validated exactly once by the auth middleware
2. **No Signature Re-verification**: Audit middleware trusts the validated claims in context
3. **No Fallback Parsing**: If no claims in context, identity is empty (no unsafe parsing)
4. **Defense in Depth**: Multiple layers validate before audit sees the data

## Implementation Details

### Authentication Middleware (auth package)

The `auth` package provides two interfaces:

- **`Authenticator`**: Legacy interface (backwards compatible)
- **`AuthenticatorWithClaims`**: New interface that stores validated claims in context

```go
type AuthenticatorWithClaims interface {
    Authenticator
    AuthenticateWithClaims(w http.ResponseWriter, r *http.Request) (bool, *oidc.AccessTokenClaims, error)
}
```

The `JWTAuth` implementation:
1. Extracts JWT from Authorization header
2. Decrypts token if encrypted
3. Parses token and extracts claims
4. **Validates issuer**
5. **Verifies signature** using JWKS
6. **Checks expiration**
7. Validates scopes (if configured)
8. Stores validated claims in context

### Audit Middleware (audit package)

The audit middleware **only reads from context**:

```go
func ExtractIdentity(ctx context.Context, logger *zap.Logger) string {
    claims := auth.GetClaimsFromContext(ctx)
    if claims != nil && claims.Subject != "" {
        return claims.Subject
    }
    return "" // No unsafe fallback
}
```

**What we DON'T do**:
- ❌ Parse JWT tokens directly
- ❌ Use `jwt.ParseUnverified()`
- ❌ Fall back to header parsing if context is empty
- ❌ Trust JWTs without signature verification

## Middleware Ordering

**CRITICAL**: The audit middleware MUST run after the auth middleware:

```go
// ✅ CORRECT ORDER
handler = authMiddleware(handler)    // 1. Validate JWT, store claims
handler = auditMiddleware(handler)   // 2. Read claims from context

// ❌ WRONG ORDER
handler = auditMiddleware(handler)   // Claims not yet in context!
handler = authMiddleware(handler)    // Too late
```

## Security Guarantees

1. **No Unverified JWT Parsing**: The audit package never parses JWTs without signature verification
2. **Separation of Concerns**: Authentication validates, audit observes
3. **Fail Secure**: If auth fails, request is rejected (audit never runs)
4. **No Bypass**: Audit cannot be used to bypass authentication
5. **Backwards Compatible**: Old code continues to work (no breaking changes)

## Configuration Options

### DisableIdentityExtraction

```go
Config{
    DisableIdentityExtraction: true, // Skip identity extraction entirely
}
```

Use this if:
- You don't need user identity in audit logs
- You have privacy concerns
- Auth middleware is not available in your setup

## Threat Model

### What We Protect Against

✅ **JWT Replay Attacks**: Auth middleware validates expiration
✅ **Signature Forgery**: Auth middleware verifies signature with JWKS
✅ **Invalid Issuer**: Auth middleware checks issuer claim
✅ **Insufficient Scopes**: Auth middleware validates required scopes
✅ **Unverified Claims**: Audit only reads claims validated by auth

### What We DON'T Protect Against

❌ **Compromised Keys**: If JWKS private key is compromised, valid-looking tokens can be created
❌ **Token Theft**: If a valid token is stolen, it can be used until expiration
❌ **Auth Middleware Bypass**: If auth middleware is not configured, audit will have no identity

## Code Review Notes

If you're reviewing this code for security:

1. **Check middleware order**: Audit MUST run after auth
2. **No ParseUnverified**: The code should never use `jwt.ParseUnverified()`
3. **Context-only**: Identity extraction should only read from context
4. **No fallback parsing**: There should be no code path that parses JWTs in audit
5. **Fail secure**: Empty identity is better than unverified identity

## Migration Path

For teams upgrading from older versions:

1. **v1 (Unsafe)**: Audit parsed JWTs with `ParseUnverified()`
2. **v2 (Current)**: Audit reads validated claims from context
3. **Migration**: No code changes needed! Just update go-libs version

The v2 implementation is backwards compatible and automatically uses context when available.

## References

- [RFC 7519: JSON Web Token (JWT)](https://tools.ietf.org/html/rfc7519)
- [OWASP JWT Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/JSON_Web_Token_for_Java_Cheat_Sheet.html)
- Go JWT Library: github.com/golang-jwt/jwt/v5
