package auth

import (
	"github.com/spf13/cobra"
	flag "github.com/spf13/pflag"
	"go.uber.org/fx"
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

func FXModuleFromFlags(cmd *cobra.Command) fx.Option {
	authEnabled, _ := cmd.Flags().GetBool(AuthEnabledFlag)
	authIssuer, _ := cmd.Flags().GetString(AuthIssuerFlag)
	authIssuers, _ := cmd.Flags().GetStringSlice(AuthIssuersFlag)
	authReadKeySetMaxRetries, _ := cmd.Flags().GetInt(AuthReadKeySetMaxRetriesFlag)
	authCheckScopes, _ := cmd.Flags().GetBool(AuthCheckScopesFlag)
	authService, _ := cmd.Flags().GetString(AuthServiceFlag)

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

	return Module(ModuleConfig{
		Enabled:              authEnabled,
		Issuers:              authIssuers,
		ReadKeySetMaxRetries: authReadKeySetMaxRetries,
		CheckScopes:          authCheckScopes,
		Service:              authService,
	})
}
