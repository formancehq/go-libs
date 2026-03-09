package jwt

import (
	flag "github.com/spf13/pflag"
)

const (
	AuthEnabledFlag              = "auth-enabled"
	AuthIssuerFlag               = "auth-issuer"
	AuthReadKeySetMaxRetriesFlag = "auth-read-key-set-max-retries"
	AuthCheckScopesFlag          = "auth-check-scopes"
	AuthServiceFlag              = "auth-service"
)

func AddFlags(flags *flag.FlagSet) {
	flags.Bool(AuthEnabledFlag, false, "Enable auth")
	flags.String(AuthIssuerFlag, "", "Issuer")
	flags.Int(AuthReadKeySetMaxRetriesFlag, 10, "ReadKeySetMaxRetries")
	flags.Bool(AuthCheckScopesFlag, false, "CheckScopes")
	flags.String(AuthServiceFlag, "", "Service")
}

func ConfigFromFlags(flags *flag.FlagSet) ModuleConfig {
	authEnabled, _ := flags.GetBool(AuthEnabledFlag)
	authIssuer, _ := flags.GetString(AuthIssuerFlag)
	authReadKeySetMaxRetries, _ := flags.GetInt(AuthReadKeySetMaxRetriesFlag)
	authCheckScopes, _ := flags.GetBool(AuthCheckScopesFlag)
	authService, _ := flags.GetString(AuthServiceFlag)

	return ModuleConfig{
		Enabled:              authEnabled,
		Issuer:               authIssuer,
		ReadKeySetMaxRetries: authReadKeySetMaxRetries,
		CheckScopes:          authCheckScopes,
		Service:              authService,
		AdditionalChecks:     make([]AdditionalCheck, 0),
	}
}
