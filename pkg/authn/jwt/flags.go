package jwt

import (
	flag "github.com/spf13/pflag"
)

const (
	AuthEnabledFlag              = "auth-enabled"
	AuthIssuerFlag               = "auth-issuer"
	AuthIssuersFlag              = "auth-issuers"
	AuthReadKeySetMaxRetriesFlag = "auth-read-key-set-max-retries"
	AuthCheckScopesFlag          = "auth-check-scopes"
	AuthServiceFlag              = "auth-service"
)

func AddFlags(flags *flag.FlagSet) {
	flags.Bool(AuthEnabledFlag, false, "Enable auth")
	flags.String(AuthIssuerFlag, "", "Issuer (single issuer, for backward compatibility)")
	flags.StringSlice(AuthIssuersFlag, nil, "Trusted issuers (comma-separated, e.g. --auth-issuers=https://issuer1,https://issuer2)")
	flags.Int(AuthReadKeySetMaxRetriesFlag, 10, "ReadKeySetMaxRetries")
	flags.Bool(AuthCheckScopesFlag, false, "CheckScopes")
	flags.String(AuthServiceFlag, "", "Service")
}

func ConfigFromFlags(flags *flag.FlagSet) Config {
	authEnabled, _ := flags.GetBool(AuthEnabledFlag)
	authIssuer, _ := flags.GetString(AuthIssuerFlag)
	authIssuers, _ := flags.GetStringSlice(AuthIssuersFlag)
	authReadKeySetMaxRetries, _ := flags.GetInt(AuthReadKeySetMaxRetriesFlag)
	authCheckScopes, _ := flags.GetBool(AuthCheckScopesFlag)
	authService, _ := flags.GetString(AuthServiceFlag)

	// Merge --auth-issuer into --auth-issuers for backward compatibility
	if authIssuer != "" {
		found := false
		for _, iss := range authIssuers {
			if iss == authIssuer {
				found = true
				break
			}
		}
		if !found {
			authIssuers = append(authIssuers, authIssuer)
		}
	}

	return Config{
		Enabled:              authEnabled,
		Issuers:              authIssuers,
		ReadKeySetMaxRetries: authReadKeySetMaxRetries,
		CheckScopes:          authCheckScopes,
		Service:              authService,
		AdditionalChecks:     make([]AdditionalCheck, 0),
	}
}
