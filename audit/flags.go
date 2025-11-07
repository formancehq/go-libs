package audit

import (
	"github.com/spf13/pflag"
)

const (
	// Flags constants for audit configuration
	AuditEnabledFlag          = "audit-enabled"
	AuditMaxBodySizeFlag      = "audit-max-body-size"
	AuditExcludedPathsFlag    = "audit-excluded-paths"
	AuditSensitiveHeadersFlag = "audit-sensitive-headers"
)

// AddFlags adds audit-related flags to the given FlagSet
// This provides a standardized way to configure audit across all Formance services
func AddFlags(flags *pflag.FlagSet) {
	flags.Bool(AuditEnabledFlag, false, "Enable HTTP audit logging")
	flags.Int64(AuditMaxBodySizeFlag, 1024*1024, "Maximum request/response body size to capture in bytes (default 1MB)")
	flags.StringSlice(AuditExcludedPathsFlag, []string{"/_healthcheck", "/_/healthcheck", "/_/info"}, "HTTP paths to exclude from audit logging")
	flags.StringSlice(AuditSensitiveHeadersFlag, []string{"Authorization", "Cookie", "Set-Cookie", "X-API-Key", "X-Auth-Token", "Proxy-Authorization"}, "HTTP headers to sanitize in audit logs")
}

// DefaultExcludedPaths returns the default list of paths to exclude from auditing
func DefaultExcludedPaths() []string {
	return []string{"/_healthcheck", "/_/healthcheck", "/_/info"}
}

// DefaultSensitiveHeaders returns the default list of sensitive headers to sanitize
func DefaultSensitiveHeaders() []string {
	return []string{
		"Authorization",
		"Cookie",
		"Set-Cookie",
		"X-API-Key",
		"X-Auth-Token",
		"Proxy-Authorization",
	}
}
